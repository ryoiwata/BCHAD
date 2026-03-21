package retrieval

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/valkey-io/valkey-go"
)

const (
	// cacheTTL is how long a retrieval result is cached in Valkey.
	// This is effectively "until next re-index" — re-indexing calls InvalidateProduct.
	cacheTTL = 7 * 24 * time.Hour
)

// Cache wraps Valkey to cache retrieval results keyed by product/stage/query hash.
type Cache struct {
	client valkey.Client
}

// NewCache creates a Cache backed by the given Valkey client.
func NewCache(client valkey.Client) *Cache {
	return &Cache{client: client}
}

// Get returns cached search results for the given filters and query vector.
// Returns nil, nil if not cached.
func (c *Cache) Get(ctx context.Context, queryVector []float32, filters SearchFilters) ([]SearchResult, error) {
	key, err := c.cacheKey(queryVector, filters)
	if err != nil {
		return nil, err
	}

	cmd := c.client.B().Get().Key(key).Build()
	resp := c.client.Do(ctx, cmd)
	if resp.Error() != nil {
		if valkey.IsValkeyNil(resp.Error()) {
			return nil, nil // cache miss
		}
		return nil, fmt.Errorf("valkey get %s: %w", key, resp.Error())
	}

	raw, err := resp.AsBytes()
	if err != nil {
		return nil, fmt.Errorf("valkey get bytes: %w", err)
	}

	var results []SearchResult
	if err := json.Unmarshal(raw, &results); err != nil {
		slog.Warn("cache: failed to unmarshal cached results — treating as miss",
			"key", key, "error", err)
		return nil, nil
	}

	slog.Info("cache: hit",
		"product_id", filters.ProductID,
		"stage_type", filters.StageType,
		"results", len(results),
	)

	return results, nil
}

// Set stores search results in Valkey with the standard TTL.
func (c *Cache) Set(ctx context.Context, queryVector []float32, filters SearchFilters, results []SearchResult) error {
	key, err := c.cacheKey(queryVector, filters)
	if err != nil {
		return err
	}

	data, err := json.Marshal(results)
	if err != nil {
		return fmt.Errorf("marshal cache value: %w", err)
	}

	ttlSecs := int64(math.Round(cacheTTL.Seconds()))
	cmd := c.client.B().Set().Key(key).Value(string(data)).Ex(time.Duration(ttlSecs) * time.Second).Build()
	if err := c.client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("valkey set %s: %w", key, err)
	}

	slog.Info("cache: stored",
		"product_id", filters.ProductID,
		"stage_type", filters.StageType,
		"results", len(results),
		"key", key,
	)

	return nil
}

// InvalidateProduct deletes all cached retrieval results for a product.
// Called after re-indexing to ensure stale results are not served.
func (c *Cache) InvalidateProduct(ctx context.Context, productID string) error {
	// Scan for keys matching retrieval:{productID}:*
	pattern := fmt.Sprintf("retrieval:%s:*", productID)

	var cursor uint64
	var keysToDelete []string

	for {
		cmd := c.client.B().Scan().Cursor(cursor).Match(pattern).Count(100).Build()
		resp := c.client.Do(ctx, cmd)
		if resp.Error() != nil {
			return fmt.Errorf("valkey scan: %w", resp.Error())
		}

		scanResult, err := resp.AsScanEntry()
		if err != nil {
			return fmt.Errorf("valkey scan entry: %w", err)
		}

		keysToDelete = append(keysToDelete, scanResult.Elements...)
		cursor = scanResult.Cursor
		if cursor == 0 {
			break
		}
	}

	if len(keysToDelete) == 0 {
		return nil
	}

	// Delete all found keys
	delCmd := c.client.B().Del().Key(keysToDelete...).Build()
	if err := c.client.Do(ctx, delCmd).Error(); err != nil {
		return fmt.Errorf("valkey del: %w", err)
	}

	slog.Info("cache: invalidated product",
		"product_id", productID,
		"keys_deleted", len(keysToDelete),
	)

	return nil
}

// cacheKey computes the Valkey key for a search request.
// Format: retrieval:{productID}:{stageType}:{sha256(queryVector+filters)}
func (c *Cache) cacheKey(queryVector []float32, filters SearchFilters) (string, error) {
	h := sha256.New()

	// Hash query vector
	for _, v := range queryVector {
		b := math.Float32bits(v)
		h.Write([]byte{byte(b), byte(b >> 8), byte(b >> 16), byte(b >> 24)})
	}

	// Hash filter parameters
	filterData := struct {
		HasPermissions *bool `json:"hp,omitempty"`
		HasAudit       *bool `json:"ha,omitempty"`
		Limit          int   `json:"l"`
	}{
		HasPermissions: filters.HasPermissions,
		HasAudit:       filters.HasAudit,
		Limit:          filters.Limit,
	}
	filterBytes, err := json.Marshal(filterData)
	if err != nil {
		return "", fmt.Errorf("marshal filter for cache key: %w", err)
	}
	h.Write(filterBytes)

	queryHash := fmt.Sprintf("%x", h.Sum(nil))[:16] // first 16 hex chars is enough
	return fmt.Sprintf("retrieval:%s:%s:%s", filters.ProductID, filters.StageType, queryHash), nil
}
