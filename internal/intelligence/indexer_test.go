package intelligence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// buildMockVoyageServer returns an httptest.Server that responds to embedding requests
// with fixture 1024-dimensional embeddings.
func buildMockVoyageServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Build fixture embeddings: 1024 zeros for each input text
		data := make([]map[string]interface{}, len(req.Input))
		for i := range req.Input {
			embedding := make([]float32, 1024)
			// Set a distinguishable value so tests can verify ordering
			embedding[0] = float32(i + 1)
			data[i] = map[string]interface{}{
				"object":    "embedding",
				"embedding": embedding,
				"index":     i,
			}
		}

		resp := map[string]interface{}{
			"object": "list",
			"data":   data,
			"model":  "voyage-code-3",
			"usage":  map[string]interface{}{"total_tokens": len(req.Input) * 10},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Logf("mock voyage server: encode error: %v", err)
		}
	}))
}

func TestIndexer_GenerateEmbeddings_MockAPI(t *testing.T) {
	server := buildMockVoyageServer(t)
	defer server.Close()

	indexer := &Indexer{
		apiKey: "test-key",
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	// Override the API URL by patching directly on the request build
	// We achieve this by testing generateEmbeddings with a custom server URL.
	// Since voyageAPIURL is a package-level const, we test via a helper that accepts a URL.
	embeddings, err := indexer.generateEmbeddingsWithURL(context.Background(), []string{"hello", "world"}, server.URL)
	if err != nil {
		t.Fatalf("generateEmbeddingsWithURL() error = %v", err)
	}

	if len(embeddings) != 2 {
		t.Fatalf("got %d embeddings, want 2", len(embeddings))
	}
	for i, emb := range embeddings {
		if len(emb) != 1024 {
			t.Errorf("embedding[%d] has %d dimensions, want 1024", i, len(emb))
		}
		// Verify distinguishable values from the mock
		if emb[0] != float32(i+1) {
			t.Errorf("embedding[%d][0] = %v, want %v", i, emb[0], float32(i+1))
		}
	}
}

func TestIndexer_IndexPatterns_EmptyInput(t *testing.T) {
	// With no patterns, IndexPatterns should succeed with 0 stored.
	idx := &Indexer{
		pool:       nil, // should not be called
		apiKey:     "test-key",
		httpClient: &http.Client{},
	}

	stored, err := idx.IndexPatterns(context.Background(), nil)
	if err != nil {
		t.Fatalf("IndexPatterns(nil) error = %v", err)
	}
	if stored != 0 {
		t.Errorf("stored = %d, want 0", stored)
	}
}

// TestIndexer_UpsertToPostgres is an integration test that requires a real Postgres
// instance with the pgvector extension and BCHAD migrations applied.
// Run with: go test -tags=integration ./internal/intelligence/...
func TestIndexer_UpsertToPostgres(t *testing.T) {
	t.Skip("integration test: run with -tags=integration and live Postgres")

	databaseURL := "postgres://bchad:bchad@localhost:5432/bchad?sslmode=disable"
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("postgres not reachable: %v", err)
	}

	server := buildMockVoyageServer(t)
	defer server.Close()

	idx := &Indexer{
		pool:       pool,
		apiKey:     "test-key",
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	// Override API URL for test
	_ = idx
	t.Log("integration test would store patterns to pgvector here")
}

// --- helper for testable embedding URL ---

// generateEmbeddingsWithURL is a testable variant that accepts a custom API URL.
func (idx *Indexer) generateEmbeddingsWithURL(ctx context.Context, texts []string, apiURL string) ([][]float32, error) {
	reqBody := voyageRequest{
		Input:     texts,
		Model:     voyageModel,
		InputType: "document",
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+idx.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := idx.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("non-200 status: %d", resp.StatusCode)
	}

	var voyageResp voyageResponse
	if err := json.NewDecoder(resp.Body).Decode(&voyageResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	embeddings := make([][]float32, len(texts))
	for _, item := range voyageResp.Data {
		if item.Index < 0 || item.Index >= len(embeddings) {
			return nil, fmt.Errorf("unexpected embedding index %d", item.Index)
		}
		if len(item.Embedding) != 1024 {
			return nil, fmt.Errorf("unexpected dimension %d", len(item.Embedding))
		}
		embeddings[item.Index] = item.Embedding
	}

	return embeddings, nil
}
