package retrieval

import "encoding/json"

// SearchFilters narrows a vector search to a specific product and stage.
type SearchFilters struct {
	ProductID      string
	StageType      string
	HasPermissions *bool // nil = don't filter
	HasAudit       *bool // nil = don't filter
	Limit          int   // max rows to return (default 10)
}

// SearchResult is a single row returned by filtered vector search.
type SearchResult struct {
	ID           string
	ContentText  string
	Metadata     json.RawMessage
	Similarity   float64 // cosine similarity in [0,1]
	QualityScore float64 // pr_quality_score from bchad_code_patterns
	TokenCount   int     // estimated token count via tiktoken
}

// Priority classifies a retrieval result within the context budget.
type Priority string

const (
	PriorityPrimary   Priority = "primary"
	PrioritySecondary Priority = "secondary"
)

// RankedResult is a SearchResult decorated with token budget accounting.
type RankedResult struct {
	SearchResult
	CombinedScore    float64  // similarity * quality_score used for ranking
	Priority         Priority // primary or secondary
	Truncated        bool     // true if method bodies were removed to fit budget
	CumulativeTokens int      // total tokens up to and including this result
}

// RankingResult is the output of the ranking phase.
type RankingResult struct {
	Primary   []RankedResult
	Secondary []RankedResult
	TotalTokens int
}
