// Package verify runs verification gates and classifies errors.
//
// Tier 1 gates run per-stage in disposable Docker containers using the target
// product's toolchain (lint, typecheck, unit tests, Semgrep security scan).
// The error classifier categorizes gate failures into eight categories — each
// with a different recovery strategy — driving Temporal's retry routing.
package verify
