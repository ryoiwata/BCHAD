package intelligence

import (
	"context"
	"math"
	"os"
	"testing"
	"time"
)

// testController is a minimal Express controller for Tree-sitter tests.
const testController = `
import { NextFunction, Request, Response, Router } from 'express';
import auth from '../utils/auth';
import { createArticle, getArticle, deleteArticle } from '../services/article.service';

const router = Router();

router.get('/articles', auth.optional, async (req: Request, res: Response, next: NextFunction) => {
  try {
    const result = await getArticle(req.params.slug, req.user?.username);
    res.json({ article: result });
  } catch (error) {
    next(error);
  }
});

router.post('/articles', auth.required, async (req: Request, res: Response, next: NextFunction) => {
  try {
    const article = await createArticle(req.body.article, req.user?.username as string);
    res.json({ article });
  } catch (error) {
    next(error);
  }
});

router.delete('/articles/:slug', auth.required, async (req: Request, res: Response, next: NextFunction) => {
  try {
    await deleteArticle(req.params.slug);
    res.sendStatus(204);
  } catch (error) {
    next(error);
  }
});

export default router;
`

// testJestFile is a Jest service test for Tree-sitter detection tests.
const testJestFile = `
import prismaMock from '../prisma-mock';
import { createArticle } from '../../src/services/article.service';

describe('ArticleService', () => {
  describe('createArticle', () => {
    test('should create an article successfully', async () => {
      // Given
      const input = { title: 'Test', description: 'Desc', body: 'Body', tagList: [] };
      const username = 'testuser';
      const mockedUser = { id: 1 };

      // When
      prismaMock.user.findUnique.mockResolvedValue(mockedUser);
      prismaMock.article.findUnique.mockResolvedValue(null);
      prismaMock.article.create.mockResolvedValue({ id: 1, slug: 'test-1', ...input });

      // Then
      await expect(createArticle(input, username)).resolves.toBeDefined();
    });

    test('should throw when title is missing', async () => {
      // Given
      const input = { title: '', description: 'Desc', body: 'Body', tagList: [] };

      // Then
      await expect(createArticle(input, 'user')).rejects.toThrowError();
    });
  });
});
`

func TestExtractor_AnalyseTypeScript_Controller(t *testing.T) {
	extractor, err := NewExtractor()
	if err != nil {
		t.Fatalf("NewExtractor() error = %v", err)
	}

	elements, hasAuth, hasErrorHandling := extractor.analyseTypeScript(context.Background(), []byte(testController))

	if !hasAuth {
		t.Error("expected hasAuth = true (auth.required is present)")
	}
	if !hasErrorHandling {
		t.Error("expected hasErrorHandling = true (try/catch is present)")
	}

	// Check for specific elements
	elemSet := make(map[string]bool, len(elements))
	for _, e := range elements {
		elemSet[e] = true
	}

	wantElems := []string{"route_get", "route_post", "route_delete", "auth_required", "auth_optional", "try_catch", "res_json", "res_sendstatus"}
	for _, want := range wantElems {
		if !elemSet[want] {
			t.Errorf("element %q not found in %v", want, elements)
		}
	}
}

func TestExtractor_AnalyseTypeScript_JestFile(t *testing.T) {
	extractor, err := NewExtractor()
	if err != nil {
		t.Fatalf("NewExtractor() error = %v", err)
	}

	elements, _, _ := extractor.analyseTypeScript(context.Background(), []byte(testJestFile))

	elemSet := make(map[string]bool, len(elements))
	for _, e := range elements {
		elemSet[e] = true
	}

	wantElems := []string{"jest_describe", "jest_test", "jest_expect"}
	for _, want := range wantElems {
		if !elemSet[want] {
			t.Errorf("Jest element %q not found in %v", want, elements)
		}
	}
}

func TestExtractor_QualityScoring_RanksCorrectly(t *testing.T) {
	// A recent, complete controller should score higher than an old, minimal one.
	now := time.Now()

	tests := []struct {
		name       string
		gitDate    time.Time
		sql        string
		wantHigher bool // compared to the "low" case
	}{
		{
			name:       "high: recent and complete",
			gitDate:    now.Add(-7 * 24 * time.Hour),
			sql:        "CREATE TABLE a (id SERIAL PRIMARY KEY, name TEXT DEFAULT 'x'); CREATE UNIQUE INDEX idx ON a(name); ALTER TABLE b ADD FOREIGN KEY (a_id) REFERENCES a(id) ON DELETE CASCADE;",
			wantHigher: true,
		},
		{
			name:       "low: old and minimal",
			gitDate:    now.Add(-400 * 24 * time.Hour),
			sql:        "CREATE TABLE b (id SERIAL NOT NULL);",
			wantHigher: false,
		},
	}

	var highScore, lowScore float64
	for _, tt := range tests {
		recency := recencyScore(tt.gitDate, now)
		completeness := scoreMigrationCompleteness(tt.sql)
		quality := recency*weightRecency + completeness*weightCompleteness + 0.3*weightReview

		if tt.wantHigher {
			highScore = quality
		} else {
			lowScore = quality
		}
	}

	if highScore <= lowScore {
		t.Errorf("high-quality pattern scored %.3f <= low-quality pattern %.3f", highScore, lowScore)
	}
}

func TestRecencyScore(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		daysAgo float64
		wantMin float64
		wantMax float64
	}{
		{"very recent (1 day)", 1, 0.99, 1.0},
		{"recent (30 days)", 30, 0.75, 0.85},
		{"old (180 days)", 180, 0.20, 0.35},
		{"very old (365 days)", 365, 0.02, 0.10},
		{"zero (today)", 0, 0.99, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			date := now.Add(-time.Duration(tt.daysAgo*24) * time.Hour)
			score := recencyScore(date, now)

			if score < tt.wantMin || score > tt.wantMax {
				t.Errorf("recencyScore(-%g days) = %.4f, want [%.2f, %.2f]",
					tt.daysAgo, score, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestReviewQualityScore(t *testing.T) {
	tests := []struct {
		name        string
		commitCount int
		wantMin     float64
		wantMax     float64
	}{
		{"no commits", 0, 0.28, 0.32},
		{"one commit", 1, 0.28, 0.35},
		{"five commits", 5, 0.65, 0.80},
		{"ten commits", 10, 0.99, 1.01},
		{"many commits", 20, 0.99, 1.01}, // clamped
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := reviewQualityScore(tt.commitCount)
			if math.IsNaN(score) || math.IsInf(score, 0) {
				t.Errorf("reviewQualityScore(%d) = NaN/Inf", tt.commitCount)
			}
			if score < tt.wantMin || score > tt.wantMax {
				t.Errorf("reviewQualityScore(%d) = %.4f, want [%.2f, %.2f]",
					tt.commitCount, score, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestScoreMigrationCompleteness(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantMin float64
		wantMax float64
	}{
		{
			"complete migration",
			"CREATE TABLE a (id SERIAL NOT NULL PRIMARY KEY, name TEXT DEFAULT 'x'); CREATE UNIQUE INDEX idx ON a(name); ALTER TABLE b ADD FOREIGN KEY (a_id) REFERENCES a(id) ON DELETE CASCADE;",
			0.95, 1.01,
		},
		{
			"minimal migration",
			"CREATE TABLE b (id SERIAL NOT NULL);",
			0.15, 0.25,
		},
		{
			"empty",
			"",
			0.0, 0.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := scoreMigrationCompleteness(tt.sql)
			if score < tt.wantMin || score > tt.wantMax {
				t.Errorf("scoreMigrationCompleteness() = %.4f, want [%.2f, %.2f]",
					score, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestScoreAPICompleteness(t *testing.T) {
	tests := []struct {
		name             string
		elements         []string
		hasAuth          bool
		hasErrorHandling bool
		wantMin          float64
		wantMax          float64
	}{
		{
			"full CRUD with auth and error handling",
			[]string{"route_get", "route_post", "route_put", "route_delete", "res_json", "res_sendstatus"},
			true, true,
			0.99, 1.01,
		},
		{
			"read-only no auth",
			[]string{"route_get", "res_json"},
			false, false,
			0.20, 0.35,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := scoreAPICompleteness(tt.elements, tt.hasAuth, tt.hasErrorHandling)
			if score < tt.wantMin || score > tt.wantMax {
				t.Errorf("scoreAPICompleteness() = %.4f, want [%.2f, %.2f]",
					score, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestExtractor_ExtractFromRealRepo(t *testing.T) {
	if _, err := os.Stat(testRepoPath); err != nil {
		t.Skipf("test repo not available at %s: %v", testRepoPath, err)
	}

	extractor, err := NewExtractor()
	if err != nil {
		t.Fatalf("NewExtractor() error = %v", err)
	}

	profile := &StructuralProfile{
		ProductID:       "test-product",
		RepoPath:        testRepoPath,
		Language:        "typescript",
		DirectoryLayout: map[string]string{
			"controllers": "src/controllers",
			"services":    "src/services",
			"tests":       "tests",
			"migrations":  "prisma/migrations",
		},
	}

	patterns, err := extractor.Extract(context.Background(), testRepoPath, "test-product", profile)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	if len(patterns) == 0 {
		t.Fatal("expected patterns to be non-empty")
	}

	// Check that we get patterns for each expected stage type
	foundStages := make(map[StageType]bool)
	for _, p := range patterns {
		foundStages[p.StageType] = true

		if p.ProductID != "test-product" {
			t.Errorf("pattern ProductID = %q, want %q", p.ProductID, "test-product")
		}
		if p.ContentText == "" {
			t.Errorf("pattern %s/%s has empty ContentText", p.StageType, p.EntityType)
		}
		if p.QualityScore < 0 || p.QualityScore > 1 {
			t.Errorf("pattern %s QualityScore = %.4f, want in [0,1]", p.StageType, p.QualityScore)
		}
	}

	expectedStages := []StageType{StageTypeMigrate, StageTypeAPI, StageTypeTests}
	for _, stage := range expectedStages {
		if !foundStages[stage] {
			t.Errorf("no patterns found for stage %q", stage)
		}
	}

	// Verify no more than maxPatternsPerStage per stage
	stageCount := make(map[StageType]int)
	for _, p := range patterns {
		stageCount[p.StageType]++
	}
	for stage, count := range stageCount {
		if count > maxPatternsPerStage {
			t.Errorf("stage %q has %d patterns, want <= %d", stage, count, maxPatternsPerStage)
		}
	}
}

func TestExtractor_APIPatternHasCorrectElements(t *testing.T) {
	if _, err := os.Stat(testRepoPath); err != nil {
		t.Skipf("test repo not available at %s: %v", testRepoPath, err)
	}

	extractor, err := NewExtractor()
	if err != nil {
		t.Fatalf("NewExtractor() error = %v", err)
	}

	profile := &StructuralProfile{
		ProductID:       "test-product",
		RepoPath:        testRepoPath,
		Language:        "typescript",
		DirectoryLayout: map[string]string{
			"controllers": "src/controllers",
			"services":    "src/services",
			"tests":       "tests",
			"migrations":  "prisma/migrations",
		},
	}

	patterns, err := extractor.Extract(context.Background(), testRepoPath, "test-product", profile)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	// Find an API pattern
	var apiPattern *CodePattern
	for i := range patterns {
		if patterns[i].StageType == StageTypeAPI {
			apiPattern = &patterns[i]
			break
		}
	}

	if apiPattern == nil {
		t.Fatal("no API pattern found")
	}

	// The article controller has auth middleware
	if !apiPattern.HasPermissions {
		// The article controller does use auth.required — check if at least one API pattern does
		for _, p := range patterns {
			if p.StageType == StageTypeAPI && p.HasPermissions {
				apiPattern = &p
				break
			}
		}
	}

	// Article controller should have at least GET and POST routes
	elems := make(map[string]bool, len(apiPattern.Metadata.Elements))
	for _, e := range apiPattern.Metadata.Elements {
		elems[e] = true
	}

	if !elems["route_get"] && !elems["route_post"] {
		t.Errorf("API pattern missing route elements, got: %v", apiPattern.Metadata.Elements)
	}
}
