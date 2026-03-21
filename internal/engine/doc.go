// Package engine executes DAG stages with verification and error recovery.
//
// It drives the Temporal workflow, dispatching stages in dependency order,
// collecting stage artifacts, routing errors to the classifier, and assembling
// the final PR. The engine does not implement retry logic directly — that is
// defined by Temporal activity retry policies derived from the error taxonomy.
package engine
