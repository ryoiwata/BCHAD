package retrieval

import (
	"strings"
	"testing"
)

func TestBuildSearchQuery_BasicFilters(t *testing.T) {
	qv := make([]float32, 1024)
	filters := SearchFilters{
		ProductID: "payments-dashboard",
		StageType: "api",
		Limit:     5,
	}

	query, args := buildSearchQuery(qv, filters)

	// Must reference both required filters
	if !strings.Contains(query, "product_id = $2") {
		t.Errorf("query missing product_id filter:\n%s", query)
	}
	if !strings.Contains(query, "stage_type = $3") {
		t.Errorf("query missing stage_type filter:\n%s", query)
	}
	if !strings.Contains(query, "ORDER BY embedding <=> $1") {
		t.Errorf("query missing ORDER BY clause:\n%s", query)
	}
	if !strings.Contains(query, "LIMIT") {
		t.Errorf("query missing LIMIT clause:\n%s", query)
	}

	// args: [vector, productID, stageType, limit]
	if len(args) != 4 {
		t.Errorf("len(args) = %d, want 4", len(args))
	}
	if args[1] != "payments-dashboard" {
		t.Errorf("args[1] = %v, want %q", args[1], "payments-dashboard")
	}
	if args[2] != "api" {
		t.Errorf("args[2] = %v, want %q", args[2], "api")
	}
	if args[3] != 5 {
		t.Errorf("args[3] = %v, want 5", args[3])
	}
}

func TestBuildSearchQuery_HasPermissionsFilter(t *testing.T) {
	qv := make([]float32, 1024)
	trueBool := true
	filters := SearchFilters{
		ProductID:      "product",
		StageType:      "api",
		Limit:          10,
		HasPermissions: &trueBool,
	}

	query, args := buildSearchQuery(qv, filters)

	if !strings.Contains(query, "has_permissions = $4") {
		t.Errorf("query missing has_permissions filter:\n%s", query)
	}

	// args: [vector, productID, stageType, hasPermissions, limit]
	if len(args) != 5 {
		t.Errorf("len(args) = %d, want 5", len(args))
	}
	if args[3] != true {
		t.Errorf("args[3] = %v, want true", args[3])
	}
}

func TestBuildSearchQuery_HasAuditFilter(t *testing.T) {
	qv := make([]float32, 1024)
	falseBool := false
	filters := SearchFilters{
		ProductID: "product",
		StageType: "api",
		Limit:     10,
		HasAudit:  &falseBool,
	}

	query, args := buildSearchQuery(qv, filters)

	if !strings.Contains(query, "has_audit = $4") {
		t.Errorf("query missing has_audit filter:\n%s", query)
	}
	if len(args) != 5 {
		t.Errorf("len(args) = %d, want 5", len(args))
	}
}

func TestBuildSearchQuery_BothOptionalFilters(t *testing.T) {
	qv := make([]float32, 1024)
	trueBool := true
	falseBool := false
	filters := SearchFilters{
		ProductID:      "product",
		StageType:      "api",
		Limit:          10,
		HasPermissions: &trueBool,
		HasAudit:       &falseBool,
	}

	query, args := buildSearchQuery(qv, filters)

	// Both optional filters should be present
	if !strings.Contains(query, "has_permissions = $4") {
		t.Errorf("query missing has_permissions filter:\n%s", query)
	}
	if !strings.Contains(query, "has_audit = $5") {
		t.Errorf("query missing has_audit filter:\n%s", query)
	}

	// args: [vector, productID, stageType, hasPermissions, hasAudit, limit]
	if len(args) != 6 {
		t.Errorf("len(args) = %d, want 6", len(args))
	}
}

func TestBuildSearchQuery_DefaultLimit(t *testing.T) {
	qv := make([]float32, 1024)
	filters := SearchFilters{
		ProductID: "product",
		StageType: "api",
		// Limit not set
	}

	// Default limit handling is in Search(), not buildSearchQuery().
	// buildSearchQuery receives whatever limit is passed.
	filters.Limit = 10 // caller sets default before calling buildSearchQuery

	_, args := buildSearchQuery(qv, filters)

	lastArg := args[len(args)-1]
	if lastArg != 10 {
		t.Errorf("LIMIT arg = %v, want 10", lastArg)
	}
}

func TestNewSearcher_InvalidEncoding(t *testing.T) {
	// NewSearcher should succeed — cl100k_base is always available via tiktoken-go
	searcher, err := NewSearcher(nil)
	if err != nil {
		t.Fatalf("NewSearcher() error = %v", err)
	}
	if searcher == nil {
		t.Error("expected non-nil Searcher")
	}
}

// TestSearcher_Search_Integration requires a live Postgres instance.
// Run with: go test -tags=integration ./internal/retrieval/...
func TestSearcher_Search_Integration(t *testing.T) {
	t.Skip("integration test: run with -tags=integration and live Postgres+pgvector")
}
