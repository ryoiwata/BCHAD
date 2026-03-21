package intelligence

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	sitter "github.com/smacker/go-tree-sitter"
	typescript "github.com/smacker/go-tree-sitter/typescript/typescript"
)

const (
	maxPatternsPerStage = 5

	weightRecency      = 0.3
	weightCompleteness = 0.4
	weightReview       = 0.3
)

// Extractor analyses a repository and produces CodePattern instances for each stage type.
// It uses Tree-sitter to inspect TypeScript ASTs and go-git for recency/review signals.
type Extractor struct {
	parser *sitter.Parser
}

// NewExtractor creates an Extractor with a Tree-sitter TypeScript parser.
func NewExtractor() (*Extractor, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(typescript.GetLanguage())
	return &Extractor{parser: parser}, nil
}

// Extract scans repoPath and returns ranked CodePattern instances ready for embedding.
func (e *Extractor) Extract(ctx context.Context, repoPath, productID string, profile *StructuralProfile) ([]CodePattern, error) {
	slog.Info("extractor: starting pattern extraction",
		"product_id", productID,
		"repo_path", repoPath,
	)

	// Open the git repository for recency and review signals
	repo, repoErr := git.PlainOpen(repoPath)
	if repoErr != nil {
		slog.Warn("extractor: cannot open git repo — recency/review signals will be zeroed",
			"error", repoErr)
		repo = nil
	}

	// Build git file-to-date and file-to-commit-count maps
	fileDate := make(map[string]time.Time)
	fileCommitCount := make(map[string]int)
	if repo != nil {
		if err := buildGitMaps(repo, repoPath, fileDate, fileCommitCount); err != nil {
			slog.Warn("extractor: failed to build git maps", "error", err)
		}
	}

	stageTypes := []StageType{StageTypeMigrate, StageTypeAPI, StageTypeTests, StageTypeConfig}
	var allPatterns []CodePattern

	for _, stage := range stageTypes {
		patterns, err := e.extractForStage(ctx, repoPath, productID, stage, profile, fileDate, fileCommitCount)
		if err != nil {
			slog.Warn("extractor: failed to extract patterns for stage",
				"stage", stage, "error", err)
			continue
		}

		// Rank and keep top N
		ranked := rankPatterns(patterns)
		if len(ranked) > maxPatternsPerStage {
			ranked = ranked[:maxPatternsPerStage]
		}
		allPatterns = append(allPatterns, ranked...)

		slog.Info("extractor: extracted patterns for stage",
			"stage", stage,
			"count", len(ranked),
		)
	}

	return allPatterns, nil
}

// extractForStage collects and scores patterns for a single pipeline stage.
func (e *Extractor) extractForStage(
	ctx context.Context,
	repoPath, productID string,
	stage StageType,
	profile *StructuralProfile,
	fileDate map[string]time.Time,
	fileCommitCount map[string]int,
) ([]CodePattern, error) {
	switch stage {
	case StageTypeMigrate:
		return e.extractMigrationPatterns(ctx, repoPath, productID, fileDate, fileCommitCount)
	case StageTypeAPI:
		return e.extractAPIPatterns(ctx, repoPath, productID, profile, fileDate, fileCommitCount)
	case StageTypeTests:
		return e.extractTestPatterns(ctx, repoPath, productID, profile, fileDate, fileCommitCount)
	case StageTypeConfig:
		return e.extractConfigPatterns(ctx, repoPath, productID, profile, fileDate, fileCommitCount)
	default:
		return nil, fmt.Errorf("unknown stage type: %s", stage)
	}
}

// extractMigrationPatterns finds Prisma migration SQL files.
func (e *Extractor) extractMigrationPatterns(
	_ context.Context,
	repoPath, productID string,
	fileDate map[string]time.Time,
	fileCommitCount map[string]int,
) ([]CodePattern, error) {
	migrationDirs := []string{
		filepath.Join(repoPath, "prisma", "migrations"),
		filepath.Join(repoPath, "migrations"),
		filepath.Join(repoPath, "db", "migrations"),
	}

	var sqlFiles []string
	for _, dir := range migrationDirs {
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() && strings.ToLower(filepath.Ext(path)) == ".sql" {
				sqlFiles = append(sqlFiles, path)
			}
			return nil
		})
	}

	var patterns []CodePattern
	now := time.Now()

	for _, sqlPath := range sqlFiles {
		content, err := os.ReadFile(sqlPath)
		if err != nil {
			continue
		}

		// Only keep migrations that CREATE tables (skip pure index or alter scripts)
		if !strings.Contains(string(content), "CREATE TABLE") {
			continue
		}

		rel, _ := filepath.Rel(repoPath, sqlPath)
		gitDate := fileDate[rel]
		if gitDate.IsZero() {
			gitDate = now.Add(-30 * 24 * time.Hour) // default to 30 days ago
		}
		commitCount := fileCommitCount[rel]

		meta := PatternMetadata{
			FilePath:     sqlPath,
			RelativePath: rel,
			GitDate:      gitDate,
		}
		meta.RecencyScore = recencyScore(gitDate, now)
		meta.CompletenessScore = scoreMigrationCompleteness(string(content))
		meta.ReviewQuality = reviewQualityScore(commitCount)
		meta.Elements = extractMigrationElements(string(content))

		quality := meta.RecencyScore*weightRecency +
			meta.CompletenessScore*weightCompleteness +
			meta.ReviewQuality*weightReview

		patterns = append(patterns, CodePattern{
			ProductID:       productID,
			StageType:       StageTypeMigrate,
			Language:        "sql",
			HasPermissions:  false,
			HasAudit:        false,
			HasIntegrations: []string{},
			QualityScore:    quality,
			ContentText:     string(content),
			Metadata:        meta,
			LastUpdated:     time.Now().UTC(),
		})
	}

	return patterns, nil
}

// extractAPIPatterns finds Express controller + service file pairs.
func (e *Extractor) extractAPIPatterns(
	ctx context.Context,
	repoPath, productID string,
	profile *StructuralProfile,
	fileDate map[string]time.Time,
	fileCommitCount map[string]int,
) ([]CodePattern, error) {
	controllerDir := filepath.Join(repoPath, profile.DirectoryLayout["controllers"])
	if profile.DirectoryLayout["controllers"] == "" {
		controllerDir = filepath.Join(repoPath, "src", "controllers")
	}
	serviceDir := filepath.Join(repoPath, profile.DirectoryLayout["services"])
	if profile.DirectoryLayout["services"] == "" {
		serviceDir = filepath.Join(repoPath, "src", "services")
	}

	// List controller files
	controllerFiles, _ := filepath.Glob(filepath.Join(controllerDir, "*.controller.ts"))
	if len(controllerFiles) == 0 {
		controllerFiles, _ = filepath.Glob(filepath.Join(controllerDir, "*.ts"))
	}

	var patterns []CodePattern
	now := time.Now()

	for _, ctrlPath := range controllerFiles {
		// Skip non-CRUD controllers
		base := filepath.Base(ctrlPath)
		if strings.Contains(base, "index") || strings.Contains(base, "app") {
			continue
		}

		// Derive the entity name (e.g., "article" from "article.controller.ts")
		entityName := strings.TrimSuffix(base, ".controller.ts")
		entityName = strings.TrimSuffix(entityName, ".ts")

		// Find matching service file
		servicePath := filepath.Join(serviceDir, entityName+".service.ts")
		if _, err := os.Stat(servicePath); err != nil {
			servicePath = ""
		}

		// Build combined content: controller + service
		ctrlContent, err := os.ReadFile(ctrlPath)
		if err != nil {
			continue
		}

		combined := string(ctrlContent)
		if servicePath != "" {
			svcContent, err := os.ReadFile(servicePath)
			if err == nil {
				combined += "\n\n// --- SERVICE ---\n" + string(svcContent)
			}
		}

		// Tree-sitter analysis
		elements, hasAuth, hasErrorHandling := e.analyseTypeScript(ctx, ctrlContent)

		rel, _ := filepath.Rel(repoPath, ctrlPath)
		gitDate := fileDate[rel]
		if gitDate.IsZero() {
			gitDate = now.Add(-30 * 24 * time.Hour)
		}
		commitCount := fileCommitCount[rel]

		meta := PatternMetadata{
			FilePath:     ctrlPath,
			RelativePath: rel,
			GitDate:      gitDate,
			Elements:     elements,
		}
		meta.RecencyScore = recencyScore(gitDate, now)
		meta.CompletenessScore = scoreAPICompleteness(elements, hasAuth, hasErrorHandling)
		meta.ReviewQuality = reviewQualityScore(commitCount)

		quality := meta.RecencyScore*weightRecency +
			meta.CompletenessScore*weightCompleteness +
			meta.ReviewQuality*weightReview

		patterns = append(patterns, CodePattern{
			ProductID:       productID,
			StageType:       StageTypeAPI,
			Language:        "typescript",
			EntityType:      toEntityName(entityName),
			HasPermissions:  hasAuth,
			HasAudit:        false, // no audit logging in this codebase
			HasIntegrations: []string{},
			QualityScore:    quality,
			ContentText:     combined,
			Metadata:        meta,
			LastUpdated:     time.Now().UTC(),
		})
	}

	return patterns, nil
}

// extractTestPatterns finds Jest test files.
func (e *Extractor) extractTestPatterns(
	ctx context.Context,
	repoPath, productID string,
	profile *StructuralProfile,
	fileDate map[string]time.Time,
	fileCommitCount map[string]int,
) ([]CodePattern, error) {
	testDir := profile.DirectoryLayout["tests"]
	if testDir == "" {
		testDir = "tests"
	}
	fullTestDir := filepath.Join(repoPath, testDir)

	var testFiles []string
	_ = filepath.Walk(fullTestDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			name := filepath.Base(path)
			if (ext == ".ts" || ext == ".js") && (strings.Contains(name, ".test.") || strings.Contains(name, ".spec.")) {
				testFiles = append(testFiles, path)
			}
		}
		return nil
	})

	var patterns []CodePattern
	now := time.Now()

	for _, testPath := range testFiles {
		content, err := os.ReadFile(testPath)
		if err != nil {
			continue
		}

		// Skip setup/mock files
		base := filepath.Base(testPath)
		if strings.Contains(base, "mock") || strings.Contains(base, "setup") || strings.Contains(base, "fixture") {
			continue
		}

		elements, _, _ := e.analyseTypeScript(ctx, content)

		rel, _ := filepath.Rel(repoPath, testPath)
		gitDate := fileDate[rel]
		if gitDate.IsZero() {
			gitDate = now.Add(-30 * 24 * time.Hour)
		}
		commitCount := fileCommitCount[rel]

		meta := PatternMetadata{
			FilePath:     testPath,
			RelativePath: rel,
			GitDate:      gitDate,
			Elements:     elements,
		}
		meta.RecencyScore = recencyScore(gitDate, now)
		meta.CompletenessScore = scoreTestCompleteness(string(content), elements)
		meta.ReviewQuality = reviewQualityScore(commitCount)

		quality := meta.RecencyScore*weightRecency +
			meta.CompletenessScore*weightCompleteness +
			meta.ReviewQuality*weightReview

		entityName := extractEntityFromTestPath(base)

		patterns = append(patterns, CodePattern{
			ProductID:       productID,
			StageType:       StageTypeTests,
			Language:        "typescript",
			EntityType:      entityName,
			HasPermissions:  false,
			HasAudit:        false,
			HasIntegrations: []string{},
			QualityScore:    quality,
			ContentText:     string(content),
			Metadata:        meta,
			LastUpdated:     time.Now().UTC(),
		})
	}

	return patterns, nil
}

// extractConfigPatterns builds a single synthetic pattern from config file contents.
func (e *Extractor) extractConfigPatterns(
	_ context.Context,
	repoPath, productID string,
	profile *StructuralProfile,
	_ map[string]time.Time,
	_ map[string]int,
) ([]CodePattern, error) {
	var parts []string
	for _, cf := range profile.ConfigFiles {
		if cf.Content != "" {
			parts = append(parts, fmt.Sprintf("// --- %s ---\n%s", cf.Name, cf.Content))
		}
	}

	if len(parts) == 0 {
		return nil, nil
	}

	// Check for jest.config.js specifically (most important for tests stage config)
	jestConfig := ""
	tsconfigContent := ""
	eslintContent := ""
	for _, cf := range profile.ConfigFiles {
		switch {
		case strings.Contains(cf.Name, "jest"):
			jestConfig = cf.Content
		case strings.Contains(cf.Name, "tsconfig"):
			tsconfigContent = cf.Content
		case strings.Contains(cf.Name, "eslint"):
			eslintContent = cf.Content
		}
	}

	combined := strings.Join(parts, "\n\n")
	elements := []string{}
	if jestConfig != "" {
		elements = append(elements, "jest_config")
	}
	if tsconfigContent != "" {
		elements = append(elements, "tsconfig")
	}
	if eslintContent != "" {
		elements = append(elements, "eslint_config")
	}

	completeness := float64(len(elements)) / 3.0
	quality := 0.5*weightRecency + completeness*weightCompleteness + 0.7*weightReview

	meta := PatternMetadata{
		FilePath:          filepath.Join(repoPath, "config-bundle"),
		RelativePath:      "config-bundle",
		GitDate:           time.Now().Add(-7 * 24 * time.Hour),
		RecencyScore:      0.5,
		CompletenessScore: completeness,
		ReviewQuality:     0.7,
		Elements:          elements,
	}

	return []CodePattern{
		{
			ProductID:       productID,
			StageType:       StageTypeConfig,
			Language:        "json",
			HasPermissions:  false,
			HasAudit:        false,
			HasIntegrations: []string{},
			QualityScore:    quality,
			ContentText:     combined,
			Metadata:        meta,
			LastUpdated:     time.Now().UTC(),
		},
	}, nil
}

// analyseTypeScript parses a TypeScript file with Tree-sitter and extracts
// structural elements, auth middleware usage, and error handling patterns.
func (e *Extractor) analyseTypeScript(ctx context.Context, content []byte) (elements []string, hasAuth bool, hasErrorHandling bool) {
	tree, err := e.parser.ParseCtx(ctx, nil, content)
	if err != nil || tree == nil {
		// Fall back to text scanning
		return textScanTypeScript(content)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		return textScanTypeScript(content)
	}

	// Walk the AST to extract Express patterns
	elements, hasAuth, hasErrorHandling = walkTSNode(root, content)
	return elements, hasAuth, hasErrorHandling
}

// walkTSNode recursively walks a Tree-sitter node and collects Express structural elements.
func walkTSNode(node *sitter.Node, content []byte) (elements []string, hasAuth bool, hasErrorHandling bool) {
	elemSet := make(map[string]bool)

	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}

		nodeText := n.Content(content)

		// Detect router method calls: router.get(...), router.post(...), etc.
		if n.Type() == "call_expression" {
			funcPart := ""
			if n.ChildCount() > 0 {
				funcPart = n.Child(0).Content(content)
			}
			switch {
			case strings.HasPrefix(funcPart, "router.get"):
				elemSet["route_get"] = true
			case strings.HasPrefix(funcPart, "router.post"):
				elemSet["route_post"] = true
			case strings.HasPrefix(funcPart, "router.put"):
				elemSet["route_put"] = true
			case strings.HasPrefix(funcPart, "router.delete"):
				elemSet["route_delete"] = true
			case strings.HasPrefix(funcPart, "router.patch"):
				elemSet["route_patch"] = true
			}
		}

		// Detect auth middleware (auth.required / auth.optional)
		if strings.Contains(nodeText, "auth.required") {
			hasAuth = true
			elemSet["auth_required"] = true
		}
		if strings.Contains(nodeText, "auth.optional") {
			elemSet["auth_optional"] = true
		}

		// Detect try/catch → next(error) pattern
		if n.Type() == "try_statement" {
			hasErrorHandling = true
			elemSet["try_catch"] = true
		}
		if strings.Contains(nodeText, "next(error)") {
			elemSet["next_error"] = true
		}

		// Detect res.json() / res.sendStatus()
		if strings.Contains(nodeText, "res.json(") {
			elemSet["res_json"] = true
		}
		if strings.Contains(nodeText, "res.sendStatus(") {
			elemSet["res_sendstatus"] = true
		}

		// Detect async arrow functions (handlers)
		if n.Type() == "arrow_function" {
			parent := n.Parent()
			if parent != nil && parent.Type() == "arguments" {
				elemSet["async_handler"] = true
			}
		}

		// Detect describe/test/it (Jest patterns)
		if n.Type() == "call_expression" && n.ChildCount() > 0 {
			funcName := n.Child(0).Content(content)
			switch funcName {
			case "describe":
				elemSet["jest_describe"] = true
			case "test", "it":
				elemSet["jest_test"] = true
			case "expect":
				elemSet["jest_expect"] = true
			case "beforeEach", "afterEach", "beforeAll", "afterAll":
				elemSet["jest_lifecycle"] = true
			}
		}

		// Recurse
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}

	walk(node)

	for e := range elemSet {
		elements = append(elements, e)
	}
	sort.Strings(elements)

	return elements, hasAuth, hasErrorHandling
}

// textScanTypeScript falls back to string-based scanning when Tree-sitter fails.
func textScanTypeScript(content []byte) (elements []string, hasAuth bool, hasErrorHandling bool) {
	text := string(content)
	elemSet := make(map[string]bool)

	checks := map[string]string{
		"route_get":     "router.get(",
		"route_post":    "router.post(",
		"route_put":     "router.put(",
		"route_delete":  "router.delete(",
		"auth_required": "auth.required",
		"auth_optional": "auth.optional",
		"try_catch":     "} catch",
		"next_error":    "next(error)",
		"res_json":      "res.json(",
		"res_sendstatus":"res.sendStatus(",
		"jest_describe": "describe(",
		"jest_test":     "test(",
		"jest_expect":   "expect(",
	}

	for elem, substr := range checks {
		if strings.Contains(text, substr) {
			elemSet[elem] = true
		}
	}

	hasAuth = elemSet["auth_required"]
	hasErrorHandling = elemSet["try_catch"]

	for e := range elemSet {
		elements = append(elements, e)
	}
	sort.Strings(elements)

	return elements, hasAuth, hasErrorHandling
}

// --- Scoring helpers ---

// recencyScore returns a 0–1 score based on how recently a file was committed.
// Files committed in the last 30 days score close to 1.0; files > 365 days old score ~0.
func recencyScore(gitDate, now time.Time) float64 {
	if gitDate.IsZero() {
		return 0.3 // default when git date unavailable
	}
	age := now.Sub(gitDate).Hours() / 24 // days
	// Exponential decay: score = e^(-age/120) clamped to [0,1]
	score := math.Exp(-age / 120)
	return math.Max(0, math.Min(1, score))
}

// reviewQualityScore returns a 0–1 score based on git commit count for the file.
// More commits = more reviewed.
func reviewQualityScore(commitCount int) float64 {
	if commitCount <= 0 {
		return 0.3
	}
	// Log scale: log(count+1)/log(11) gives 0→0, 10→1
	score := math.Log(float64(commitCount)+1) / math.Log(11)
	return math.Max(0, math.Min(1, score))
}

// scoreMigrationCompleteness rates a migration SQL file's structural completeness.
func scoreMigrationCompleteness(sql string) float64 {
	elements := 0
	total := 5

	if strings.Contains(sql, "CREATE TABLE") {
		elements++
	}
	if strings.Contains(sql, "PRIMARY KEY") {
		elements++
	}
	if strings.Contains(sql, "DEFAULT") {
		elements++
	}
	if strings.Contains(sql, "CREATE INDEX") || strings.Contains(sql, "CREATE UNIQUE INDEX") {
		elements++
	}
	if strings.Contains(sql, "REFERENCES") || strings.Contains(sql, "FOREIGN KEY") || strings.Contains(sql, "ON DELETE") {
		elements++
	}

	return float64(elements) / float64(total)
}

// scoreAPICompleteness rates a controller file's structural completeness.
func scoreAPICompleteness(elements []string, hasAuth, hasErrorHandling bool) float64 {
	score := 0.0
	total := 7.0

	elemSet := make(map[string]bool, len(elements))
	for _, e := range elements {
		elemSet[e] = true
	}

	// CRUD coverage: GET + POST + PUT/PATCH + DELETE = 4 points
	if elemSet["route_get"] {
		score++
	}
	if elemSet["route_post"] {
		score++
	}
	if elemSet["route_put"] || elemSet["route_patch"] {
		score++
	}
	if elemSet["route_delete"] {
		score++
	}

	// Auth: 1 point
	if hasAuth {
		score++
	}

	// Error handling: 1 point
	if hasErrorHandling {
		score++
	}

	// Response sending: 1 point
	if elemSet["res_json"] || elemSet["res_sendstatus"] {
		score++
	}

	return score / total
}

// scoreTestCompleteness rates a test file's structural completeness.
func scoreTestCompleteness(content string, elements []string) float64 {
	score := 0.0
	total := 5.0

	elemSet := make(map[string]bool, len(elements))
	for _, e := range elements {
		elemSet[e] = true
	}

	if elemSet["jest_describe"] {
		score++
	}
	if elemSet["jest_test"] {
		score++
	}
	if elemSet["jest_expect"] {
		score++
	}
	if strings.Contains(content, "// Given") || strings.Contains(content, "// When") || strings.Contains(content, "// Then") {
		score++
	}
	if strings.Contains(content, ".mockResolvedValue") || strings.Contains(content, ".mockReturnValue") {
		score++
	}

	return score / total
}

// rankPatterns sorts patterns by QualityScore descending.
func rankPatterns(patterns []CodePattern) []CodePattern {
	sort.Slice(patterns, func(i, j int) bool {
		return patterns[i].QualityScore > patterns[j].QualityScore
	})
	return patterns
}

// extractEntityFromTestPath derives a PascalCase entity name from a test file name.
// e.g., "article.service.test.ts" → "Article"
func extractEntityFromTestPath(base string) string {
	// Remove known suffixes
	name := base
	for _, suf := range []string{".service.test.ts", ".test.ts", ".spec.ts", ".service.spec.ts"} {
		name = strings.TrimSuffix(name, suf)
	}
	return toEntityName(name)
}

// toEntityName converts a file base name to PascalCase entity name.
func toEntityName(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// extractMigrationElements returns a list of structural elements found in SQL.
func extractMigrationElements(sql string) []string {
	var elems []string
	checks := map[string]string{
		"create_table":  "CREATE TABLE",
		"primary_key":   "PRIMARY KEY",
		"default_value": "DEFAULT",
		"unique_index":  "CREATE UNIQUE INDEX",
		"foreign_key":   "REFERENCES",
		"on_delete":     "ON DELETE",
		"add_column":    "ALTER TABLE",
	}
	for elem, substr := range checks {
		if strings.Contains(sql, substr) {
			elems = append(elems, elem)
		}
	}
	sort.Strings(elems)
	return elems
}

// buildGitMaps walks git history and builds file→date and file→commitCount maps.
func buildGitMaps(repo *git.Repository, repoPath string, fileDate map[string]time.Time, fileCommitCount map[string]int) error {
	ref, err := repo.Head()
	if err != nil {
		return fmt.Errorf("git head: %w", err)
	}

	commits, err := repo.Log(&git.LogOptions{From: ref.Hash()})
	if err != nil {
		return fmt.Errorf("git log: %w", err)
	}

	return commits.ForEach(func(c *object.Commit) error {
		stats, err := c.Stats()
		if err != nil {
			return nil // skip commits we cannot stat
		}
		for _, stat := range stats {
			rel := filepath.ToSlash(stat.Name)
			fileCommitCount[rel]++
			// Keep the most recent date for each file
			if existing, ok := fileDate[rel]; !ok || c.Author.When.After(existing) {
				fileDate[rel] = c.Author.When
			}
		}
		return nil
	})
}
