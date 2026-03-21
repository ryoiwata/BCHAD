//go:build tools

// Package main holds blank imports of all external dependencies used by BCHAD
// components that are not yet implemented. This prevents go mod tidy from
// removing them from go.mod before the packages that use them are written.
//
// This file is excluded from normal builds by the "tools" build tag.
package main

import (
	_ "github.com/go-chi/chi/v5"
	_ "github.com/go-git/go-git/v5"
	_ "github.com/google/go-github/v62/github"
	_ "github.com/invopop/jsonschema"
	_ "github.com/pgvector/pgvector-go"
	_ "github.com/pkoukk/tiktoken-go"
	_ "github.com/shurcooL/githubv4"
	_ "github.com/smacker/go-tree-sitter"
	_ "go.opentelemetry.io/otel"
	_ "go.opentelemetry.io/otel/trace"
)
