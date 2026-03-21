// Package assembly creates branches, commits, and PRs.
//
// After all stages pass, it assembles the generated files into a feature branch
// with one commit per stage, generates a PR description including a generation
// report (stages, files, retries, cost), and opens the PR via the GitHub API.
package assembly
