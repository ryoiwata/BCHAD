// Package gateway provides the in-process LLM API client with cost tracking.
//
// It is a Go library, not a separate service. It sends requests to the Anthropic
// Messages API via direct HTTP (no SDK), tracks token counts and costs per stage,
// enforces rate limits via Valkey, handles streaming SSE responses, and logs
// prompt hashes to the audit trail in PostgreSQL.
package gateway
