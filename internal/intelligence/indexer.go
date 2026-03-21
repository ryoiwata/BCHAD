package intelligence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
)

const (
	voyageAPIURL   = "https://api.voyageai.com/v1/embeddings"
	voyageModel    = "voyage-code-3"
	voyageBatchMax = 128 // Voyage supports up to 128 texts per request
)

// Indexer generates embeddings via Voyage AI and upserts them into pgvector.
type Indexer struct {
	pool       *pgxpool.Pool
	apiKey     string
	httpClient *http.Client
}

// NewIndexer creates an Indexer with the given database pool and Voyage API key.
func NewIndexer(pool *pgxpool.Pool, voyageAPIKey string) *Indexer {
	return &Indexer{
		pool:   pool,
		apiKey: voyageAPIKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// voyageRequest is the request body for the Voyage AI embeddings API.
type voyageRequest struct {
	Input     []string `json:"input"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type"`
}

// voyageResponse is the response from the Voyage AI embeddings API.
type voyageResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// IndexPatterns generates embeddings for all patterns and upserts them into bchad_code_patterns.
func (idx *Indexer) IndexPatterns(ctx context.Context, patterns []CodePattern) (int, error) {
	if len(patterns) == 0 {
		return 0, nil
	}

	slog.Info("indexer: starting embedding generation",
		"patterns", len(patterns),
	)

	// Process in batches to respect Voyage API limits
	stored := 0
	for i := 0; i < len(patterns); i += voyageBatchMax {
		end := i + voyageBatchMax
		if end > len(patterns) {
			end = len(patterns)
		}
		batch := patterns[i:end]

		if err := idx.processBatch(ctx, batch); err != nil {
			return stored, fmt.Errorf("indexer: batch %d: %w", i/voyageBatchMax, err)
		}
		stored += len(batch)

		slog.Info("indexer: batch complete",
			"batch_start", i,
			"batch_end", end,
			"total_stored", stored,
		)
	}

	return stored, nil
}

// processBatch embeds a batch of patterns and upserts them into Postgres.
func (idx *Indexer) processBatch(ctx context.Context, patterns []CodePattern) error {
	// Extract text content for embedding
	texts := make([]string, len(patterns))
	for i, p := range patterns {
		texts[i] = p.ContentText
	}

	// Generate embeddings via Voyage AI
	embeddings, err := idx.generateEmbeddings(ctx, texts)
	if err != nil {
		return fmt.Errorf("generate embeddings: %w", err)
	}
	if len(embeddings) != len(patterns) {
		return fmt.Errorf("embedding count mismatch: got %d, want %d", len(embeddings), len(patterns))
	}

	// Upsert into Postgres using pgx batch
	batch := &pgx.Batch{}
	for i, p := range patterns {
		metaJSON, err := json.Marshal(p.Metadata)
		if err != nil {
			return fmt.Errorf("marshal metadata for pattern %d: %w", i, err)
		}

		integrations := p.HasIntegrations
		if integrations == nil {
			integrations = []string{}
		}

		batch.Queue(
			`INSERT INTO bchad_code_patterns
				(product_id, stage_type, language, entity_type, has_permissions, has_audit,
				 has_integrations, pr_quality_score, content_text, metadata_json, embedding, last_updated)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			 ON CONFLICT DO NOTHING`,
			p.ProductID,
			string(p.StageType),
			p.Language,
			p.EntityType,
			p.HasPermissions,
			p.HasAudit,
			integrations,
			p.QualityScore,
			p.ContentText,
			json.RawMessage(metaJSON),
			pgvector.NewVector(embeddings[i]),
			time.Now().UTC(),
		)
	}

	results := idx.pool.SendBatch(ctx, batch)

	for i := range patterns {
		if _, err := results.Exec(); err != nil {
			results.Close() //nolint:errcheck
			return fmt.Errorf("upsert pattern %d: %w", i, err)
		}
	}

	return results.Close()
}

// generateEmbeddings calls the Voyage AI API to embed a batch of texts.
func (idx *Indexer) generateEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := voyageRequest{
		Input:     texts,
		Model:     voyageModel,
		InputType: "document",
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, voyageAPIURL, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+idx.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := idx.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voyage api request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Detail string `json:"detail"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return nil, fmt.Errorf("voyage api error: status=%d detail=%s", resp.StatusCode, errBody.Detail)
	}

	var voyageResp voyageResponse
	if err := json.NewDecoder(resp.Body).Decode(&voyageResp); err != nil {
		return nil, fmt.Errorf("decode voyage response: %w", err)
	}

	slog.Info("indexer: voyage api call complete",
		"model", voyageResp.Model,
		"total_tokens", voyageResp.Usage.TotalTokens,
		"embeddings", len(voyageResp.Data),
	)

	// Sort by index to match input order
	embeddings := make([][]float32, len(texts))
	for _, item := range voyageResp.Data {
		if item.Index < 0 || item.Index >= len(embeddings) {
			return nil, fmt.Errorf("unexpected embedding index %d", item.Index)
		}
		if len(item.Embedding) != 1024 {
			return nil, fmt.Errorf("unexpected embedding dimension: got %d, want 1024", len(item.Embedding))
		}
		embeddings[item.Index] = item.Embedding
	}

	return embeddings, nil
}

// DeleteProductPatterns removes all stored patterns for a product, used before re-indexing.
func (idx *Indexer) DeleteProductPatterns(ctx context.Context, productID string) error {
	_, err := idx.pool.Exec(ctx,
		`DELETE FROM bchad_code_patterns WHERE product_id = $1`,
		productID,
	)
	if err != nil {
		return fmt.Errorf("delete patterns for product %s: %w", productID, err)
	}
	return nil
}
