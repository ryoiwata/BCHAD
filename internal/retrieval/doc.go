// Package retrieval handles filtered vector search and context ranking.
//
// It queries pgvector with SQL WHERE filters on (product_id, stage_type) plus
// vector cosine distance ordering, ranks results by similarity × quality score
// within the stage's token budget, and caches retrieval results in Valkey.
package retrieval
