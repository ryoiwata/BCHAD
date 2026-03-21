// Package budget allocates token budgets across prompt sections.
//
// The Context Budget Allocator partitions the model's context window across the
// five prompt layers (system, adapter, codebase brief, upstream outputs, instruction)
// by priority. Fixed sections are always included; upstream outputs and primary
// examples fill the remaining space before secondary examples and arch notes.
// It truncates gracefully and never exceeds the model's context window.
package budget
