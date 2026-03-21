package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/valkey-io/valkey-go"
)

func TestCache_KeyGeneration_Deterministic(t *testing.T) {
	// Build a cache (no Valkey needed for this test)
	c := &Cache{client: nil}

	qv := make([]float32, 1024)
	for i := range qv {
		qv[i] = float32(i) * 0.001
	}

	filters := SearchFilters{
		ProductID: "payments-dashboard",
		StageType: "api",
		Limit:     5,
	}

	key1, err1 := c.cacheKey(qv, filters)
	key2, err2 := c.cacheKey(qv, filters)

	if err1 != nil || err2 != nil {
		t.Fatalf("cacheKey() errors: %v, %v", err1, err2)
	}
	if key1 != key2 {
		t.Errorf("cacheKey is not deterministic: %q != %q", key1, key2)
	}
}

func TestCache_KeyGeneration_Format(t *testing.T) {
	c := &Cache{client: nil}

	qv := make([]float32, 1024)
	filters := SearchFilters{
		ProductID: "my-product",
		StageType: "migrate",
		Limit:     10,
	}

	key, err := c.cacheKey(qv, filters)
	if err != nil {
		t.Fatalf("cacheKey() error = %v", err)
	}

	// Must start with the retrieval:{product}:{stage}: prefix
	expectedPrefix := "retrieval:my-product:migrate:"
	if len(key) < len(expectedPrefix) || key[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("key %q does not have expected prefix %q", key, expectedPrefix)
	}
}

func TestCache_KeyGeneration_DifferentVectors(t *testing.T) {
	c := &Cache{client: nil}

	qv1 := make([]float32, 1024)
	qv2 := make([]float32, 1024)
	qv2[0] = 0.999 // different first element

	filters := SearchFilters{ProductID: "p", StageType: "api", Limit: 5}

	key1, _ := c.cacheKey(qv1, filters)
	key2, _ := c.cacheKey(qv2, filters)

	if key1 == key2 {
		t.Error("expected different keys for different query vectors")
	}
}

func TestCache_KeyGeneration_DifferentFilters(t *testing.T) {
	c := &Cache{client: nil}

	qv := make([]float32, 1024)
	trueBool := true

	filters1 := SearchFilters{ProductID: "p", StageType: "api", Limit: 5}
	filters2 := SearchFilters{ProductID: "p", StageType: "api", Limit: 5, HasPermissions: &trueBool}

	key1, _ := c.cacheKey(qv, filters1)
	key2, _ := c.cacheKey(qv, filters2)

	if key1 == key2 {
		t.Error("expected different keys for different filter parameters")
	}
}

func TestCache_MarshalUnmarshal(t *testing.T) {
	// Verify that SearchResult round-trips through JSON correctly.
	results := []SearchResult{
		{
			ID:           "abc-123",
			ContentText:  "router.get('/items', ...)",
			Metadata:     json.RawMessage(`{"file_path": "src/controllers/item.ts"}`),
			Similarity:   0.92,
			QualityScore: 0.85,
			TokenCount:   150,
		},
	}

	data, err := json.Marshal(results)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded []SearchResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(decoded) != 1 {
		t.Fatalf("decoded %d results, want 1", len(decoded))
	}
	if decoded[0].ID != results[0].ID {
		t.Errorf("ID = %q, want %q", decoded[0].ID, results[0].ID)
	}
	if decoded[0].Similarity != results[0].Similarity {
		t.Errorf("Similarity = %v, want %v", decoded[0].Similarity, results[0].Similarity)
	}
}

// TestCache_Integration tests the full Get/Set/Invalidate flow against a real Valkey instance.
// Run with: go test -tags=integration ./internal/retrieval/...
func TestCache_Integration(t *testing.T) {
	t.Skip("integration test: run with -tags=integration and live Valkey")

	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{"localhost:6379"},
	})
	if err != nil {
		t.Fatalf("valkey.NewClient() error = %v", err)
	}
	defer client.Close()

	cache := NewCache(client)
	ctx := context.Background()

	qv := make([]float32, 1024)
	for i := range qv {
		qv[i] = float32(i) * 0.001
	}
	filters := SearchFilters{
		ProductID: fmt.Sprintf("test-cache-%d", 42),
		StageType: "api",
		Limit:     5,
	}

	// Cache miss
	got, err := cache.Get(ctx, qv, filters)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != nil {
		t.Errorf("expected cache miss (nil), got %v", got)
	}

	// Cache set
	results := []SearchResult{
		{ID: "test-id", ContentText: "test content", Similarity: 0.9, QualityScore: 0.8, TokenCount: 50},
	}
	if err := cache.Set(ctx, qv, filters, results); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	// Cache hit
	got, err = cache.Get(ctx, qv, filters)
	if err != nil {
		t.Fatalf("Get() after Set() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != "test-id" {
		t.Errorf("Get() = %v, want [{ID: test-id}]", got)
	}

	// Invalidate
	if err := cache.InvalidateProduct(ctx, filters.ProductID); err != nil {
		t.Fatalf("InvalidateProduct() error = %v", err)
	}

	// Cache miss after invalidation
	got, err = cache.Get(ctx, qv, filters)
	if err != nil {
		t.Fatalf("Get() after invalidation error = %v", err)
	}
	if got != nil {
		t.Errorf("expected cache miss after invalidation, got %v", got)
	}
}
