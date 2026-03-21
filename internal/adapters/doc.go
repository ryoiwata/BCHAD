// Package adapters maps pattern-level intent to language-specific generation.
//
// Each adapter knows the framework map, toolchain commands, import style,
// linter config, and ORM patterns for a specific language (TypeScript/Express,
// Python/FastAPI, Go/Chi). Adapters provide Layer 2 of the five-layer prompt
// structure and the toolchain commands for Tier 1 verification gates.
package adapters
