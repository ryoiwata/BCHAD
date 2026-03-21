package retrieval

import (
	"encoding/json"
	"strings"
	"testing"
)

func makeResult(id string, similarity, quality float64, tokenCount int) SearchResult {
	return SearchResult{
		ID:           id,
		ContentText:  strings.Repeat("x", tokenCount*4), // ~4 chars per token
		Metadata:     json.RawMessage(`{}`),
		Similarity:   similarity,
		QualityScore: quality,
		TokenCount:   tokenCount,
	}
}

func TestRanker_BasicBudgetFilling(t *testing.T) {
	ranker := NewRanker()

	results := []SearchResult{
		makeResult("a", 0.95, 0.90, 100), // combined = 0.855, primary
		makeResult("b", 0.85, 0.85, 150), // combined = 0.7225, primary
		makeResult("c", 0.50, 0.60, 200), // combined = 0.30, secondary
	}

	ranked := ranker.Rank(results, 500)

	totalPrimary := len(ranked.Primary)
	totalSecondary := len(ranked.Secondary)

	if totalPrimary == 0 {
		t.Error("expected at least one primary result")
	}
	if ranked.TotalTokens > 500 {
		t.Errorf("TotalTokens = %d, want <= 500", ranked.TotalTokens)
	}
	_ = totalSecondary
}

func TestRanker_RanksHighScoreFirst(t *testing.T) {
	ranker := NewRanker()

	results := []SearchResult{
		makeResult("low", 0.50, 0.60, 50),  // combined = 0.30
		makeResult("high", 0.95, 0.95, 50), // combined = 0.9025
		makeResult("mid", 0.75, 0.80, 50),  // combined = 0.60
	}

	ranked := ranker.Rank(results, 500)

	all := append(ranked.Primary, ranked.Secondary...)
	if len(all) < 3 {
		t.Fatalf("expected 3 results, got %d", len(all))
	}

	// First result should have highest combined score
	if all[0].ID != "high" {
		t.Errorf("first result ID = %q, want %q", all[0].ID, "high")
	}
	// Last result should have lowest combined score
	if all[len(all)-1].ID != "low" {
		t.Errorf("last result ID = %q, want %q", all[len(all)-1].ID, "low")
	}
}

func TestRanker_StaysWithinBudget(t *testing.T) {
	ranker := NewRanker()

	results := []SearchResult{
		makeResult("a", 0.95, 0.90, 400), // combined = 0.855
		makeResult("b", 0.85, 0.85, 400), // combined = 0.7225
		makeResult("c", 0.70, 0.70, 400), // combined = 0.49
	}

	budget := 500
	ranked := ranker.Rank(results, budget)

	if ranked.TotalTokens > budget {
		t.Errorf("TotalTokens = %d, exceeds budget %d", ranked.TotalTokens, budget)
	}

	all := append(ranked.Primary, ranked.Secondary...)
	// Only one of the 400-token results should fit in a 500-token budget
	if len(all) > 1 {
		t.Errorf("expected at most 1 result for budget=%d, got %d", budget, len(all))
	}
}

func TestRanker_TruncatesLargePatterns(t *testing.T) {
	ranker := NewRanker()

	// A result with a large method body that can be truncated
	largeContent := `router.get('/items', auth.required, async (req, res, next) => {
  try {
    ` + strings.Repeat("const x = await doSomethingExpensive();\n", 100) + `
    res.json({ items: [] });
  } catch (error) {
    next(error);
  }
});`

	results := []SearchResult{
		{
			ID:           "large",
			ContentText:  largeContent,
			Metadata:     json.RawMessage(`{}`),
			Similarity:   0.90,
			QualityScore: 0.85,
			TokenCount:   2000, // too large for budget, but truncation should help
		},
	}

	budget := 200 // small budget forces truncation attempt
	ranked := ranker.Rank(results, budget)

	all := append(ranked.Primary, ranked.Secondary...)
	// If truncation worked, we should have the result; if not, it was skipped
	if len(all) > 0 && ranked.TotalTokens > budget {
		t.Errorf("TotalTokens = %d, exceeds budget %d after truncation", ranked.TotalTokens, budget)
	}
}

func TestRanker_EmptyInput(t *testing.T) {
	ranker := NewRanker()

	ranked := ranker.Rank(nil, 1000)

	if len(ranked.Primary) != 0 {
		t.Errorf("expected empty Primary, got %d", len(ranked.Primary))
	}
	if len(ranked.Secondary) != 0 {
		t.Errorf("expected empty Secondary, got %d", len(ranked.Secondary))
	}
	if ranked.TotalTokens != 0 {
		t.Errorf("expected TotalTokens = 0, got %d", ranked.TotalTokens)
	}
}

func TestRanker_ZeroBudget(t *testing.T) {
	ranker := NewRanker()

	results := []SearchResult{
		makeResult("a", 0.90, 0.85, 100),
	}

	ranked := ranker.Rank(results, 0)

	all := append(ranked.Primary, ranked.Secondary...)
	if len(all) != 0 {
		t.Errorf("expected no results with zero budget, got %d", len(all))
	}
}

func TestRanker_PrimaryVsSecondaryThreshold(t *testing.T) {
	ranker := NewRanker()

	results := []SearchResult{
		makeResult("will-be-primary", 0.95, 0.70, 50),  // combined = 0.665 ≥ 0.6 → primary
		makeResult("will-be-secondary", 0.50, 0.50, 50), // combined = 0.25 < 0.6 → secondary
	}

	ranked := ranker.Rank(results, 500)

	if len(ranked.Primary) == 0 {
		t.Error("expected at least one primary result")
	}
	if len(ranked.Secondary) == 0 {
		t.Error("expected at least one secondary result")
	}

	// Verify priorities are set correctly
	for _, r := range ranked.Primary {
		if r.Priority != PriorityPrimary {
			t.Errorf("primary result %q has priority %q", r.ID, r.Priority)
		}
	}
	for _, r := range ranked.Secondary {
		if r.Priority != PrioritySecondary {
			t.Errorf("secondary result %q has priority %q", r.ID, r.Priority)
		}
	}
}

func TestTruncateMethodBodies(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantShorter bool
	}{
		{
			"short body preserved",
			`function foo() { return 1; }`,
			false,
		},
		{
			"long body truncated",
			`function bar() { ` + strings.Repeat("const x = 1;\n", 50) + ` return x; }`,
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateMethodBodies(tt.input)
			if tt.wantShorter && len(result) >= len(tt.input) {
				t.Errorf("expected truncated output to be shorter: input=%d result=%d",
					len(tt.input), len(result))
			}
			if !tt.wantShorter && result != tt.input {
				t.Errorf("expected output unchanged for short body")
			}
		})
	}
}
