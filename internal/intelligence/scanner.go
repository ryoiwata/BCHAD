package intelligence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Uploader is the interface the scanner uses to upload to S3.
// Satisfied by *s3.Client.
type S3Uploader interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// Scanner extracts structural profiles from a local repository.
type Scanner struct {
	s3Client   S3Uploader
	bucketName string
}

// NewScanner creates a Scanner backed by the given S3 client and bucket.
func NewScanner(s3Client S3Uploader, bucketName string) *Scanner {
	return &Scanner{s3Client: s3Client, bucketName: bucketName}
}

// Scan walks the repo at repoPath, builds a StructuralProfile, uploads it to S3,
// and returns the profile for downstream use.
func (sc *Scanner) Scan(ctx context.Context, repoPath, productID string) (*StructuralProfile, error) {
	slog.Info("scanner: starting structural scan",
		"product_id", productID,
		"repo_path", repoPath,
	)

	profile := &StructuralProfile{
		ProductID:       productID,
		RepoPath:        repoPath,
		Language:        "typescript",
		DirectoryLayout: make(map[string]string),
		ScannedAt:       time.Now().UTC(),
	}

	// Detect framework stack from package.json
	if err := sc.detectStack(repoPath, profile); err != nil {
		return nil, fmt.Errorf("scanner: detect stack: %w", err)
	}

	// Discover directory layout
	if err := sc.discoverLayout(repoPath, profile); err != nil {
		return nil, fmt.Errorf("scanner: discover layout: %w", err)
	}

	// Extract config files (tsconfig, eslint, prettier, jest)
	if err := sc.extractConfigFiles(ctx, repoPath, productID, profile); err != nil {
		return nil, fmt.Errorf("scanner: extract config files: %w", err)
	}

	// Read prisma schema if present
	if err := sc.readPrismaSchema(repoPath, profile); err != nil {
		return nil, fmt.Errorf("scanner: read prisma schema: %w", err)
	}

	// Walk file tree and classify
	if err := sc.classifyFiles(repoPath, profile); err != nil {
		return nil, fmt.Errorf("scanner: classify files: %w", err)
	}

	// Upload structural profile JSON to S3
	s3Key, err := sc.uploadProfile(ctx, productID, profile)
	if err != nil {
		return nil, fmt.Errorf("scanner: upload profile: %w", err)
	}

	slog.Info("scanner: scan complete",
		"product_id", productID,
		"s3_key", s3Key,
		"framework", profile.Framework,
		"orm", profile.ORM,
		"files_source", profile.FileCount.Source,
		"files_test", profile.FileCount.Test,
		"files_migration", profile.FileCount.Migration,
	)

	return profile, nil
}

// detectStack reads package.json to identify framework, ORM, and test framework.
func (sc *Scanner) detectStack(repoPath string, profile *StructuralProfile) error {
	pkgPath := filepath.Join(repoPath, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // not a Node.js project
		}
		return fmt.Errorf("read package.json: %w", err)
	}

	profile.PackageJSON = string(data)

	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return fmt.Errorf("parse package.json: %w", err)
	}

	all := make(map[string]string)
	for k, v := range pkg.Dependencies {
		all[k] = v
	}
	for k, v := range pkg.DevDependencies {
		all[k] = v
	}

	// Framework detection
	switch {
	case all["express"] != "":
		profile.Framework = "express"
	case all["fastify"] != "":
		profile.Framework = "fastify"
	case all["koa"] != "":
		profile.Framework = "koa"
	default:
		profile.Framework = "unknown"
	}

	// ORM detection
	switch {
	case all["@prisma/client"] != "" || all["prisma"] != "":
		profile.ORM = "prisma"
	case all["typeorm"] != "":
		profile.ORM = "typeorm"
	case all["sequelize"] != "":
		profile.ORM = "sequelize"
	default:
		profile.ORM = "none"
	}

	// Test framework detection
	switch {
	case all["jest"] != "":
		profile.TestFramework = "jest"
	case all["mocha"] != "":
		profile.TestFramework = "mocha"
	case all["vitest"] != "":
		profile.TestFramework = "vitest"
	default:
		profile.TestFramework = "unknown"
	}

	return nil
}

// discoverLayout identifies key directories in the repo.
func (sc *Scanner) discoverLayout(repoPath string, profile *StructuralProfile) error {
	candidates := map[string][]string{
		"controllers": {"src/controllers", "controllers", "src/routes"},
		"services":    {"src/services", "services", "lib/services"},
		"models":      {"src/models", "models", "src/types", "types"},
		"utils":       {"src/utils", "utils", "lib/utils", "src/helpers"},
		"routes":      {"src/routes", "routes"},
		"tests":       {"tests", "test", "__tests__", "src/__tests__"},
		"migrations":  {"prisma/migrations", "migrations", "db/migrations"},
		"prisma":      {"prisma"},
	}

	for key, paths := range candidates {
		for _, rel := range paths {
			full := filepath.Join(repoPath, rel)
			if info, err := os.Stat(full); err == nil && info.IsDir() {
				profile.DirectoryLayout[key] = rel
				break
			}
		}
	}

	return nil
}

// extractConfigFiles reads known config files and uploads them to S3.
func (sc *Scanner) extractConfigFiles(ctx context.Context, repoPath, productID string, profile *StructuralProfile) error {
	configCandidates := []struct {
		name    string
		relPath string
	}{
		{"tsconfig.json", "tsconfig.json"},
		{".eslintrc.json", ".eslintrc.json"},
		{".eslintrc.js", ".eslintrc.js"},
		{".eslintrc", ".eslintrc"},
		{".prettierrc", ".prettierrc"},
		{".prettierrc.json", ".prettierrc.json"},
		{"prettier.config.js", "prettier.config.js"},
		{"jest.config.js", "jest.config.js"},
		{"jest.config.ts", "jest.config.ts"},
	}

	for _, c := range configCandidates {
		fullPath := filepath.Join(repoPath, c.relPath)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue // not present
		}

		cf := ConfigFile{
			Name:    c.name,
			RelPath: c.relPath,
			Content: string(data),
		}
		profile.ConfigFiles = append(profile.ConfigFiles, cf)

		// Also upload to S3 style_configs/
		s3Key := fmt.Sprintf("bchad-codebase-profiles/%s/style_configs/%s", productID, c.name)
		_, err = sc.s3Client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:      aws.String(sc.bucketName),
			Key:         aws.String(s3Key),
			Body:        bytes.NewReader(data),
			ContentType: aws.String("application/octet-stream"),
		})
		if err != nil {
			slog.Warn("scanner: failed to upload config file to S3",
				"file", c.name, "error", err)
			// non-fatal — continue scanning
		}
	}

	return nil
}

// readPrismaSchema reads prisma/schema.prisma if present.
func (sc *Scanner) readPrismaSchema(repoPath string, profile *StructuralProfile) error {
	schemaPath := filepath.Join(repoPath, "prisma", "schema.prisma")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read prisma schema: %w", err)
	}
	profile.PrismaSchema = string(data)
	return nil
}

// classifyFiles walks the repo and counts files by type.
func (sc *Scanner) classifyFiles(repoPath string, profile *StructuralProfile) error {
	return filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable paths
		}
		if info.IsDir() {
			// Skip hidden directories and common noise dirs
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") || base == "node_modules" || base == "dist" || base == ".next" {
				return filepath.SkipDir
			}
			return nil
		}

		rel, _ := filepath.Rel(repoPath, path)
		class := classifyFile(rel)
		switch class {
		case "source":
			profile.FileCount.Source++
		case "test":
			profile.FileCount.Test++
		case "migration":
			profile.FileCount.Migration++
		case "config":
			profile.FileCount.Config++
		default:
			profile.FileCount.Other++
		}

		return nil
	})
}

// classifyFile returns the classification of a file by its relative path and extension.
func classifyFile(relPath string) string {
	lower := strings.ToLower(relPath)
	ext := strings.ToLower(filepath.Ext(relPath))

	// Skip noise directories
	noiseDirs := []string{"node_modules/", "dist/", ".next/", ".git/", "vendor/", "build/"}
	for _, dir := range noiseDirs {
		if strings.Contains(lower, dir) {
			return "other"
		}
	}

	// Migrations
	if strings.Contains(lower, "migration") || strings.Contains(lower, "prisma/migrations") {
		if ext == ".sql" {
			return "migration"
		}
	}

	// Tests
	if strings.Contains(lower, ".test.") || strings.Contains(lower, ".spec.") ||
		strings.Contains(lower, "/__tests__/") || strings.HasPrefix(lower, "tests/") ||
		strings.HasPrefix(lower, "test/") {
		return "test"
	}

	// Config files
	configPatterns := []string{
		"tsconfig", ".eslintrc", "jest.config", "prettier", ".babelrc",
		"webpack.config", "rollup.config", "vite.config", ".env",
		"package.json", "package-lock.json", "yarn.lock", ".gitignore",
		"dockerfile", "docker-compose", "makefile", "justfile",
	}
	baseLower := strings.ToLower(filepath.Base(relPath))
	for _, pat := range configPatterns {
		if strings.Contains(baseLower, pat) {
			return "config"
		}
	}
	if ext == ".json" && !strings.Contains(lower, "/src/") {
		return "config"
	}

	// Source files
	switch ext {
	case ".ts", ".tsx", ".js", ".jsx", ".mts", ".cts":
		return "source"
	}

	return "other"
}

// uploadProfile serialises the profile to JSON and uploads to S3.
func (sc *Scanner) uploadProfile(ctx context.Context, productID string, profile *StructuralProfile) (string, error) {
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal profile: %w", err)
	}

	s3Key := fmt.Sprintf("bchad-codebase-profiles/%s/structural_profile.json", productID)
	_, err = sc.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(sc.bucketName),
		Key:         aws.String(s3Key),
		Body:        io.NopCloser(bytes.NewReader(data)),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return "", fmt.Errorf("s3 put object %s: %w", s3Key, err)
	}

	return s3Key, nil
}
