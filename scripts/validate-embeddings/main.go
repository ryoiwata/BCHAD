// Command validate-embeddings verifies that the pgvector retrieval returns
// sensible results for each pipeline stage type.
//
// For each stage type (migrate, api, tests, config), it constructs a natural
// language query, generates an embedding via Voyage AI, runs filtered vector
// search against bchad_code_patterns, and prints the top-3 results with
// similarity scores and content previews.
//
// An engineer manually evaluates whether the results match the patterns
// documented in docs/codebase-exploration/node-express-prisma-app.md.
//
// Usage:
//
//	go run ./scripts/validate-embeddings/main.go --product <product-id>
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
)

const (
	voyageAPIURL = "https://api.voyageai.com/v1/embeddings"
	voyageModel  = "voyage-code-3"
	topK         = 3
)

// stageQuery maps each stage type to a natural-language query that should
// retrieve relevant patterns from the indexed codebase.
var stageQueries = map[string]string{
	"migrate": "Prisma migration SQL file that creates a table with relationships, foreign keys, unique indexes, and timestamps",
	"api":     "Express CRUD route handler with JWT authentication middleware, async try-catch error handling, and service layer delegation",
	"tests":   "Jest service test with jest-mock-extended Prisma mock, Given When Then comments, and mockResolvedValue assertions",
	"config":  "TypeScript project configuration including tsconfig strict mode, ESLint airbnb-base rules, and jest ts-jest preset",
}

func main() {
	productID := flag.String("product", "", "product ID to validate (required)")
	flag.Parse()

	if *productID == "" {
		fmt.Fprintln(os.Stderr, "error: --product flag is required")
		flag.Usage()
		os.Exit(1)
	}

	// Required env vars
	databaseURL := os.Getenv("BCHAD_DATABASE_URL")
	voyageAPIKey := os.Getenv("VOYAGE_API_KEY")

	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "error: BCHAD_DATABASE_URL environment variable is required")
		os.Exit(1)
	}
	if voyageAPIKey == "" {
		fmt.Fprintln(os.Stderr, "error: VOYAGE_API_KEY environment variable is required")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Connect to Postgres
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		slog.Error("database ping failed", "error", err)
		os.Exit(1)
	}

	httpClient := &http.Client{Timeout: 60 * time.Second}

	fmt.Printf("=== BCHAD Embedding Validation ===\n")
	fmt.Printf("Product: %s\n", *productID)
	fmt.Printf("Timestamp: %s\n\n", time.Now().Format(time.RFC3339))

	stages := []string{"migrate", "api", "tests", "config"}

	for _, stage := range stages {
		query := stageQueries[stage]
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("Stage: %s\n", stage)
		fmt.Printf("Query: %q\n\n", query)

		// Generate embedding for the query
		embedding, err := generateEmbedding(ctx, httpClient, voyageAPIKey, query)
		if err != nil {
			fmt.Printf("  ERROR: failed to generate embedding: %v\n\n", err)
			continue
		}

		// Search pgvector
		results, err := searchVectors(ctx, pool, embedding, *productID, stage, topK)
		if err != nil {
			fmt.Printf("  ERROR: vector search failed: %v\n\n", err)
			continue
		}

		if len(results) == 0 {
			fmt.Printf("  WARNING: no results found — is the product indexed?\n\n")
			continue
		}

		for i, r := range results {
			fmt.Printf("  Result %d (similarity=%.4f, quality=%.3f)\n", i+1, r.similarity, r.quality)
			fmt.Printf("  File: %s\n", r.filePath)
			preview := previewContent(r.content, 200)
			fmt.Printf("  Preview:\n%s\n\n", indent(preview, "    "))
		}
	}

	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Println("Validation complete. Review the results above against:")
	fmt.Println("  docs/codebase-exploration/node-express-prisma-app.md")
}

type searchResultRow struct {
	id         string
	content    string
	similarity float64
	quality    float64
	filePath   string
}

func searchVectors(ctx context.Context, pool *pgxpool.Pool, embedding []float32, productID, stageType string, limit int) ([]searchResultRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT
			id::text,
			content_text,
			(1 - (embedding <=> $1))::float8 AS similarity,
			pr_quality_score::float8,
			metadata_json->>'relative_path' AS file_path
		FROM bchad_code_patterns
		WHERE product_id = $2 AND stage_type = $3
		ORDER BY embedding <=> $1
		LIMIT $4
	`,
		pgvector.NewVector(embedding),
		productID,
		stageType,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var results []searchResultRow
	for rows.Next() {
		var r searchResultRow
		if err := rows.Scan(&r.id, &r.content, &r.similarity, &r.quality, &r.filePath); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

type voyageEmbedRequest struct {
	Input     []string `json:"input"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type"`
}

type voyageEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func generateEmbedding(ctx context.Context, client *http.Client, apiKey, text string) ([]float32, error) {
	reqBody := voyageEmbedRequest{
		Input:     []string{text},
		Model:     voyageModel,
		InputType: "query",
	}
	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, voyageAPIURL, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("voyage api status %d", resp.StatusCode)
	}

	var voyageResp voyageEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&voyageResp); err != nil {
		return nil, err
	}

	if len(voyageResp.Data) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}

	return voyageResp.Data[0].Embedding, nil
}

// previewContent returns the first maxChars characters of content with ellipsis.
func previewContent(content string, maxChars int) string {
	content = strings.TrimSpace(content)
	if len(content) <= maxChars {
		return content
	}
	return content[:maxChars] + "..."
}

// indent adds a prefix to each line of s.
func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
