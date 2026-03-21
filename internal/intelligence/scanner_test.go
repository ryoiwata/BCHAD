package intelligence

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// testRepoPath is the real test target repository on disk.
const testRepoPath = "/home/riwata/Documents/projects/ai_engineering/gauntlet-curriculum/projects/node-express-prisma-v1-official-app"

// mockS3Uploader captures PutObject calls for assertion in tests.
type mockS3Uploader struct {
	uploads map[string][]byte
}

func newMockS3() *mockS3Uploader {
	return &mockS3Uploader{uploads: make(map[string][]byte)}
}

func (m *mockS3Uploader) PutObject(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if params.Body != nil {
		data, _ := io.ReadAll(params.Body)
		m.uploads[*params.Key] = data
	}
	return &s3.PutObjectOutput{}, nil
}

func TestScanner_DetectsFrameworkStack(t *testing.T) {
	if _, err := os.Stat(testRepoPath); err != nil {
		t.Skipf("test repo not available at %s: %v", testRepoPath, err)
	}

	mockS3 := newMockS3()
	scanner := NewScanner(mockS3, "test-bucket")

	profile, err := scanner.Scan(context.Background(), testRepoPath, "test-product")
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	// Framework detection from package.json
	if profile.Framework != "express" {
		t.Errorf("Framework = %q, want %q", profile.Framework, "express")
	}
	if profile.ORM != "prisma" {
		t.Errorf("ORM = %q, want %q", profile.ORM, "prisma")
	}
	if profile.TestFramework != "jest" {
		t.Errorf("TestFramework = %q, want %q", profile.TestFramework, "jest")
	}
}

func TestScanner_DiscoverConfigFiles(t *testing.T) {
	if _, err := os.Stat(testRepoPath); err != nil {
		t.Skipf("test repo not available at %s: %v", testRepoPath, err)
	}

	mockS3 := newMockS3()
	scanner := NewScanner(mockS3, "test-bucket")

	profile, err := scanner.Scan(context.Background(), testRepoPath, "test-product")
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	// Check that known config files are discovered
	found := make(map[string]bool)
	for _, cf := range profile.ConfigFiles {
		found[cf.Name] = true
	}

	wantFiles := []string{"tsconfig.json", ".eslintrc.json", "jest.config.js"}
	for _, want := range wantFiles {
		if !found[want] {
			t.Errorf("config file %q not found; discovered: %v", want, configFileNames(profile.ConfigFiles))
		}
	}

	// Each config file should have content
	for _, cf := range profile.ConfigFiles {
		if cf.Content == "" {
			t.Errorf("config file %q has empty content", cf.Name)
		}
	}
}

func TestScanner_FileClassification(t *testing.T) {
	if _, err := os.Stat(testRepoPath); err != nil {
		t.Skipf("test repo not available at %s: %v", testRepoPath, err)
	}

	mockS3 := newMockS3()
	scanner := NewScanner(mockS3, "test-bucket")

	profile, err := scanner.Scan(context.Background(), testRepoPath, "test-product")
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	// The test repo has source, test, and migration files
	if profile.FileCount.Source == 0 {
		t.Error("expected source files > 0")
	}
	if profile.FileCount.Test == 0 {
		t.Error("expected test files > 0")
	}
	if profile.FileCount.Migration == 0 {
		t.Error("expected migration files > 0")
	}
}

func TestScanner_PrismaSchemaExtracted(t *testing.T) {
	if _, err := os.Stat(testRepoPath); err != nil {
		t.Skipf("test repo not available at %s: %v", testRepoPath, err)
	}

	mockS3 := newMockS3()
	scanner := NewScanner(mockS3, "test-bucket")

	profile, err := scanner.Scan(context.Background(), testRepoPath, "test-product")
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if profile.PrismaSchema == "" {
		t.Error("expected PrismaSchema to be non-empty")
	}
	// Should contain model definitions
	if !containsSubstring(profile.PrismaSchema, "model Article") {
		t.Error("expected PrismaSchema to contain 'model Article'")
	}
	if !containsSubstring(profile.PrismaSchema, "model User") {
		t.Error("expected PrismaSchema to contain 'model User'")
	}
}

func TestScanner_DirectoryLayout(t *testing.T) {
	if _, err := os.Stat(testRepoPath); err != nil {
		t.Skipf("test repo not available at %s: %v", testRepoPath, err)
	}

	mockS3 := newMockS3()
	scanner := NewScanner(mockS3, "test-bucket")

	profile, err := scanner.Scan(context.Background(), testRepoPath, "test-product")
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	wantLayout := map[string]string{
		"controllers": "src/controllers",
		"services":    "src/services",
		"tests":       "tests",
		"migrations":  "prisma/migrations",
	}

	for key, want := range wantLayout {
		got := profile.DirectoryLayout[key]
		if got != want {
			t.Errorf("DirectoryLayout[%q] = %q, want %q", key, got, want)
		}
	}
}

func TestScanner_UploadsToS3(t *testing.T) {
	if _, err := os.Stat(testRepoPath); err != nil {
		t.Skipf("test repo not available at %s: %v", testRepoPath, err)
	}

	mockS3 := newMockS3()
	scanner := NewScanner(mockS3, "test-bucket")

	_, err := scanner.Scan(context.Background(), testRepoPath, "my-product")
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	// Structural profile must be uploaded
	profileKey := "bchad-codebase-profiles/my-product/structural_profile.json"
	profileData, ok := mockS3.uploads[profileKey]
	if !ok {
		t.Errorf("expected S3 upload for key %q", profileKey)
	}

	// Profile must be valid JSON
	var profile StructuralProfile
	if err := json.Unmarshal(profileData, &profile); err != nil {
		t.Errorf("uploaded profile is not valid JSON: %v", err)
	}
	if profile.ProductID != "my-product" {
		t.Errorf("profile ProductID = %q, want %q", profile.ProductID, "my-product")
	}

	// Style config files must be uploaded
	styleKeyPrefix := "bchad-codebase-profiles/my-product/style_configs/"
	foundStyle := false
	for key := range mockS3.uploads {
		if len(key) > len(styleKeyPrefix) && key[:len(styleKeyPrefix)] == styleKeyPrefix {
			foundStyle = true
			break
		}
	}
	if !foundStyle {
		t.Error("expected at least one style config uploaded to S3")
	}
}

func TestClassifyFile(t *testing.T) {
	tests := []struct {
		name     string
		relPath  string
		wantClass string
	}{
		{"ts source", "src/controllers/article.controller.ts", "source"},
		{"ts test", "tests/services/article.service.test.ts", "test"},
		{"sql migration", "prisma/migrations/20210924222830_initial/migration.sql", "migration"},
		{"tsconfig", "tsconfig.json", "config"},
		{"eslint", ".eslintrc.json", "config"},
		{"jest config", "jest.config.js", "config"},
		{"package json", "package.json", "config"},
		{"spec file", "src/utils/auth.spec.ts", "test"},
		{"node_modules ignored", "node_modules/express/index.js", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyFile(tt.relPath)
			if got != tt.wantClass {
				t.Errorf("classifyFile(%q) = %q, want %q", tt.relPath, got, tt.wantClass)
			}
		})
	}
}

// --- helpers ---

func configFileNames(cfs []ConfigFile) []string {
	names := make([]string, len(cfs))
	for i, cf := range cfs {
		names[i] = cf.Name
	}
	return names
}

func containsSubstring(s, sub string) bool {
	return bytes.Contains([]byte(s), []byte(sub))
}
