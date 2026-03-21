// Package plan generates BCHADPlans from specs and codebase profiles.
//
// It applies DAG templates (CRUD+UI, integration, workflow, analytics) to a
// validated BCHADSpec, parameterizes stage dependencies and model assignments,
// estimates costs per stage, and identifies stages requiring human approval.
package plan
