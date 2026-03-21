package retrieval

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
	"github.com/pkoukk/tiktoken-go"
)

// Searcher executes filtered vector searches against bchad_code_patterns.
type Searcher struct {
	pool    *pgxpool.Pool
	encoder *tiktoken.Tiktoken
}

// NewSearcher creates a Searcher. The encoder is used for token counting.
func NewSearcher(pool *pgxpool.Pool) (*Searcher, error) {
	// cl100k_base works for both Claude and code content
	enc, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		return nil, fmt.Errorf("tiktoken encoding: %w", err)
	}
	return &Searcher{pool: pool, encoder: enc}, nil
}

// Search runs a filtered pgvector cosine similarity search.
// The query vector must be 1024-dimensional (Voyage Code 3).
func (s *Searcher) Search(ctx context.Context, queryVector []float32, filters SearchFilters) ([]SearchResult, error) {
	if len(queryVector) != 1024 {
		return nil, fmt.Errorf("search: query vector must be 1024-dimensional, got %d", len(queryVector))
	}
	if filters.ProductID == "" {
		return nil, fmt.Errorf("search: product_id filter is required")
	}
	if filters.StageType == "" {
		return nil, fmt.Errorf("search: stage_type filter is required")
	}
	if filters.Limit <= 0 {
		filters.Limit = 10
	}

	query, args := buildSearchQuery(queryVector, filters)

	slog.Info("retrieval: executing vector search",
		"product_id", filters.ProductID,
		"stage_type", filters.StageType,
		"limit", filters.Limit,
	)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	results, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SearchResult, error) {
		var sr SearchResult
		if err := row.Scan(&sr.ID, &sr.ContentText, &sr.Metadata, &sr.QualityScore, &sr.Similarity); err != nil {
			return SearchResult{}, fmt.Errorf("scan search result: %w", err)
		}
		sr.TokenCount = s.countTokens(sr.ContentText)
		return sr, nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect search rows: %w", err)
	}

	slog.Info("retrieval: search complete",
		"product_id", filters.ProductID,
		"stage_type", filters.StageType,
		"results", len(results),
	)

	return results, nil
}

// buildSearchQuery constructs the parameterised pgvector query and its arguments.
// The query uses cosine distance (<=> operator) ordered ascending (smaller = more similar).
func buildSearchQuery(queryVector []float32, filters SearchFilters) (string, []any) {
	// Base: filtered vector search returning cosine similarity as 1 - distance
	const baseQuery = `
		SELECT
			id::text,
			content_text,
			metadata_json,
			pr_quality_score::float8,
			(1 - (embedding <=> $1))::float8 AS similarity
		FROM bchad_code_patterns
		WHERE product_id = $2
		  AND stage_type = $3`

	args := []any{
		pgvector.NewVector(queryVector),
		filters.ProductID,
		filters.StageType,
	}

	var extraClauses []string
	paramIdx := 4 // next $N index

	if filters.HasPermissions != nil {
		extraClauses = append(extraClauses, fmt.Sprintf("AND has_permissions = $%d", paramIdx))
		args = append(args, *filters.HasPermissions)
		paramIdx++
	}
	if filters.HasAudit != nil {
		extraClauses = append(extraClauses, fmt.Sprintf("AND has_audit = $%d", paramIdx))
		args = append(args, *filters.HasAudit)
		paramIdx++
	}

	extraClauses = append(extraClauses,
		"ORDER BY embedding <=> $1",
		fmt.Sprintf("LIMIT $%d", paramIdx),
	)
	args = append(args, filters.Limit)

	query := baseQuery + "\n\t\t  " + strings.Join(extraClauses, "\n\t\t  ")
	return query, args
}

// countTokens counts tokens in content using the tiktoken encoder.
func (s *Searcher) countTokens(content string) int {
	tokens := s.encoder.Encode(content, nil, nil)
	return len(tokens)
}
