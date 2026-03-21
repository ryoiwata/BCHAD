package retrieval

import (
	"regexp"
	"sort"
)

// primaryThreshold is the minimum combined score for a result to be primary.
// Results below this are secondary (filled if budget remains).
const primaryThreshold = 0.6

// methodBodyPattern matches TypeScript/JavaScript method bodies for truncation.
// It strips the body content while preserving the function signature.
var methodBodyPattern = regexp.MustCompile(`(\{[^{}]*\{[^{}]*\}[^{}]*\}|\{[^{}]{200,}\})`)

// Ranker sorts retrieval results by combined score and fills them into a token budget.
type Ranker struct{}

// NewRanker creates a Ranker.
func NewRanker() *Ranker {
	return &Ranker{}
}

// Rank distributes search results across primary and secondary buckets
// within the given token budget. Results are ranked by similarity × quality_score.
//
// Budget allocation:
//  1. Score and sort all results by combinedScore = similarity × qualityScore.
//  2. Fill primary (score ≥ threshold) results first, up to budget.
//  3. Fill secondary results with remaining budget.
//  4. Truncate method bodies if a result doesn't fit; skip if still too large.
func (r *Ranker) Rank(results []SearchResult, tokenBudget int) RankingResult {
	if len(results) == 0 || tokenBudget <= 0 {
		return RankingResult{}
	}

	// Score and sort descending
	type scored struct {
		sr    SearchResult
		score float64
	}
	scored_ := make([]scored, len(results))
	for i, sr := range results {
		combined := sr.Similarity * sr.QualityScore
		scored_[i] = scored{sr: sr, score: combined}
	}
	sort.Slice(scored_, func(i, j int) bool {
		return scored_[i].score > scored_[j].score
	})

	var primary, secondary []RankedResult
	remaining := tokenBudget

	for _, s := range scored_ {
		if remaining <= 0 {
			break
		}

		sr := s.sr
		combined := s.score

		// Try to fit in full; if not, try truncated version
		if sr.TokenCount <= remaining {
			rr := RankedResult{
				SearchResult:  sr,
				CombinedScore: combined,
				Truncated:     false,
			}
			if combined >= primaryThreshold {
				rr.Priority = PriorityPrimary
				rr.CumulativeTokens = tokenBudget - remaining + sr.TokenCount
				primary = append(primary, rr)
			} else {
				rr.Priority = PrioritySecondary
				rr.CumulativeTokens = tokenBudget - remaining + sr.TokenCount
				secondary = append(secondary, rr)
			}
			remaining -= sr.TokenCount
			continue
		}

		// Try truncation: remove method bodies to shrink token count
		truncated := truncateMethodBodies(sr.ContentText)
		truncatedTokens := estimateTokens(truncated)
		if truncatedTokens <= remaining {
			truncatedSR := sr
			truncatedSR.ContentText = truncated
			truncatedSR.TokenCount = truncatedTokens

			rr := RankedResult{
				SearchResult:  truncatedSR,
				CombinedScore: combined,
				Truncated:     true,
			}
			if combined >= primaryThreshold {
				rr.Priority = PriorityPrimary
				rr.CumulativeTokens = tokenBudget - remaining + truncatedTokens
				primary = append(primary, rr)
			} else {
				rr.Priority = PrioritySecondary
				rr.CumulativeTokens = tokenBudget - remaining + truncatedTokens
				secondary = append(secondary, rr)
			}
			remaining -= truncatedTokens
			continue
		}

		// Too large even truncated — skip this result
	}

	total := tokenBudget - remaining
	return RankingResult{
		Primary:     primary,
		Secondary:   secondary,
		TotalTokens: total,
	}
}

// truncateMethodBodies removes long method/function bodies from code content,
// preserving signatures. This allows more examples to fit within the token budget.
func truncateMethodBodies(content string) string {
	// Replace large curly-brace bodies (>200 chars) with a placeholder
	return methodBodyPattern.ReplaceAllStringFunc(content, func(match string) string {
		if len(match) > 200 {
			return "{ /* ... truncated ... */ }"
		}
		return match
	})
}

// estimateTokens provides a rough token count (chars/4 is a common approximation).
// Used after truncation to avoid a full tiktoken encode on every candidate.
func estimateTokens(content string) int {
	return (len(content) + 3) / 4
}
