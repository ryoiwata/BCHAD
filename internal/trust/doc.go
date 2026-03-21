// Package trust computes trust scores and manages phase transitions.
//
// Trust is per-engineer per-product and is computed from five weighted signals:
// CI pass rate, edit volume, retry rate, override count, and time-to-merge.
// Phase transitions (Supervised → Gated → Monitored) are gated by score
// thresholds and minimum completed run counts. Automatic downgrade occurs after
// three consecutive low-scoring runs.
package trust
