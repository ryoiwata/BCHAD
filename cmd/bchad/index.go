package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/athena-digital/bchad/internal/intelligence"
)

func init() {
	rootCmd.AddCommand(indexCmd)
	indexCmd.Flags().StringP("repo", "r", "", "local path to the repository to index (required)")
	indexCmd.Flags().StringP("product", "p", "", "product ID to use for this codebase (required)")
	indexCmd.Flags().Bool("extract-patterns", false, "extract and score patterns only — skip scanner and embeddings")
	_ = indexCmd.MarkFlagRequired("repo")
	_ = indexCmd.MarkFlagRequired("product")
}

var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Index a local repository into the codebase intelligence store",
	Long: `Scans a local repository and indexes it into BCHAD:

1. Scanner: extracts structural profile (framework, ORM, config files) → uploads to S3
2. Extractor: finds CRUD-shaped patterns for each stage type, scores them
3. Indexer: generates Voyage Code 3 embeddings and upserts into pgvector

Requires: BCHAD_DATABASE_URL, BCHAD_S3_ENDPOINT, VOYAGE_API_KEY environment variables.

Example:
  bchad index --repo ~/projects/payments-dashboard --product payments-dashboard`,
	RunE: runIndex,
}

func runIndex(cmd *cobra.Command, _ []string) error {
	repoPath, _ := cmd.Flags().GetString("repo")
	productID, _ := cmd.Flags().GetString("product")
	extractOnly, _ := cmd.Flags().GetBool("extract-patterns")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Validate repo path
	if _, err := os.Stat(repoPath); err != nil {
		return fmt.Errorf("repo path not found: %w", err)
	}

	// Required environment variables
	databaseURL := os.Getenv("BCHAD_DATABASE_URL")
	s3Endpoint := os.Getenv("BCHAD_S3_ENDPOINT")
	voyageAPIKey := os.Getenv("VOYAGE_API_KEY")
	s3Bucket := os.Getenv("BCHAD_S3_BUCKET_PROFILES")
	if s3Bucket == "" {
		s3Bucket = "bchad-codebase-profiles"
	}

	if databaseURL == "" {
		return fmt.Errorf("BCHAD_DATABASE_URL environment variable is required")
	}
	if s3Endpoint == "" {
		return fmt.Errorf("BCHAD_S3_ENDPOINT environment variable is required")
	}
	if voyageAPIKey == "" && !extractOnly {
		return fmt.Errorf("VOYAGE_API_KEY environment variable is required for embedding generation")
	}

	slog.Info("bchad index: starting",
		"repo", repoPath,
		"product_id", productID,
		"extract_only", extractOnly,
	)

	// --- Step 1: Scanner ---
	var profile *intelligence.StructuralProfile

	if !extractOnly {
		s3Client, err := newS3Client(ctx, s3Endpoint)
		if err != nil {
			return fmt.Errorf("s3 client: %w", err)
		}

		scanner := intelligence.NewScanner(s3Client, s3Bucket)
		scannedProfile, err := scanner.Scan(ctx, repoPath, productID)
		if err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		profile = scannedProfile

		fmt.Printf("\nScanner complete:\n")
		fmt.Printf("  Framework:      %s\n", profile.Framework)
		fmt.Printf("  ORM:            %s\n", profile.ORM)
		fmt.Printf("  Test framework: %s\n", profile.TestFramework)
		fmt.Printf("  Config files:   %d\n", len(profile.ConfigFiles))
		fmt.Printf("  Files scanned:  source=%d  test=%d  migration=%d  config=%d\n",
			profile.FileCount.Source,
			profile.FileCount.Test,
			profile.FileCount.Migration,
			profile.FileCount.Config,
		)
	}

	// --- Step 2: Extractor ---
	extractor, err := intelligence.NewExtractor()
	if err != nil {
		return fmt.Errorf("create extractor: %w", err)
	}

	if profile == nil {
		// Minimal profile for extract-only mode
		profile = &intelligence.StructuralProfile{
			ProductID:       productID,
			RepoPath:        repoPath,
			Language:        "typescript",
			DirectoryLayout: map[string]string{},
		}
	}

	patterns, err := extractor.Extract(ctx, repoPath, productID, profile)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	// Print extraction summary
	patternsPerStage := make(map[intelligence.StageType]int)
	for _, p := range patterns {
		patternsPerStage[p.StageType]++
	}
	fmt.Printf("\nExtractor complete:\n")
	for stage, count := range patternsPerStage {
		fmt.Printf("  %s: %d patterns\n", stage, count)
	}
	fmt.Printf("  Total: %d patterns\n", len(patterns))

	if extractOnly {
		fmt.Println("\n(--extract-patterns: skipping embedding generation)")
		return nil
	}

	// --- Step 3: Indexer ---
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("database connection: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("database ping: %w", err)
	}

	indexer := intelligence.NewIndexer(pool, voyageAPIKey)
	stored, err := indexer.IndexPatterns(ctx, patterns)
	if err != nil {
		return fmt.Errorf("index: %w", err)
	}

	fmt.Printf("\nIndexer complete:\n")
	fmt.Printf("  Embeddings stored: %d\n", stored)
	fmt.Printf("\nIndex complete for product '%s'.\n", productID)

	return nil
}

// newS3Client creates an S3 client configured for local MinIO or AWS.
func newS3Client(ctx context.Context, endpoint string) (*s3.Client, error) {
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if accessKey == "" {
		accessKey = "bchad" // MinIO local default
	}
	if secretKey == "" {
		secretKey = "bchad123" // MinIO local default
	}

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			accessKey, secretKey, "",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = &endpoint
		o.UsePathStyle = true // required for MinIO
	})

	return client, nil
}
