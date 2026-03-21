// Package spec handles BCHADSpec parsing, NL translation, and validation.
//
// It accepts JSON specs (structured input from the CLI) and natural-language
// briefs (free-form text from engineers), normalizing both into a validated
// BCHADSpec. Ambiguous NL fields are marked as needs_clarification=true so
// the engineer can resolve them interactively before plan generation proceeds.
package spec
