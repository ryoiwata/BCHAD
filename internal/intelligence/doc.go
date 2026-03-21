// Package intelligence indexes codebases and extracts patterns.
//
// It extracts structural profiles (file tree, framework detection, linter configs)
// and code patterns (annotated examples from merged PRs scored by recency, review
// quality, and Tree-sitter structural completeness), then generates Voyage Code 3
// embeddings and stores them in pgvector for retrieval.
package intelligence
