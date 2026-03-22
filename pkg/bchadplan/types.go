// Package bchadplan defines the BCHADPlan type and its sub-types.
// BCHADPlan is the generation plan produced from a BCHADSpec. It defines the DAG of
// stages to execute, their dependencies, model assignments, cost estimates, and approval
// gates. It is validated against schemas/bchadplan.v1.json at every component boundary.
package bchadplan

// BCHADPlan is the generation plan for a feature. It is produced by the plan generator
// from a BCHADSpec and a codebase profile, and consumed by the Temporal workflow executor.
type BCHADPlan struct {
	// ID is the unique plan identifier. Format: pf-YYYYMMDD-NNN (e.g. "pf-20260315-001").
	ID string `json:"id"`

	// Product is the target product identifier.
	Product string `json:"product"`

	// Pattern is the generation pattern used to build this plan.
	Pattern string `json:"pattern"`

	// Entity is the PascalCase entity name being generated.
	Entity string `json:"entity"`

	// ProjectedCost is the total projected LLM cost for this plan in USD,
	// including expected retries.
	ProjectedCost float64 `json:"projected_cost"`

	// Stages is the ordered list of pipeline stages in dependency order.
	Stages []PlanStage `json:"stages"`

	// EstimatedTotalFiles is the sum of EstimatedFiles across all stages.
	EstimatedTotalFiles int `json:"estimated_total_files,omitempty"`

	// HumanApprovalGates lists the stage IDs that require human approval before execution.
	HumanApprovalGates []string `json:"human_approval_gates,omitempty"`

	// SecurityReview indicates this plan includes stages with sensitive fields or compliance
	// requirements that trigger security review.
	SecurityReview bool `json:"security_review,omitempty"`

	// RepoURL is the HTTPS URL of the target GitHub repository, e.g.
	// "https://github.com/owner/repo". Populated from the codebase structural profile.
	RepoURL string `json:"repo_url,omitempty"`
}

// PlanStage is a single stage in the generation DAG.
type PlanStage struct {
	// ID is the stage identifier.
	// Standard values: "migrate", "config", "api", "frontend", "tests".
	ID string `json:"id"`

	// Type is the stage type descriptor.
	// Standard values: "db_migration", "feature_flags_and_permissions", "rest_endpoints",
	// "react_components", "test_suite".
	Type string `json:"type"`

	// DependsOn lists the stage IDs that must complete before this stage can start.
	DependsOn []string `json:"depends_on,omitempty"`

	// HumanApproval indicates this stage requires explicit human approval before execution.
	HumanApproval bool `json:"human_approval"`

	// Model is the Claude model identifier to use for this stage.
	Model string `json:"model"`

	// Description is a human-readable description of what this stage will generate.
	Description string `json:"description"`

	// EstimatedFiles is the estimated number of files this stage will create or modify.
	EstimatedFiles int `json:"estimated_files,omitempty"`

	// EstimatedCost is the projected LLM cost for this stage in USD.
	EstimatedCost float64 `json:"estimated_cost,omitempty"`

	// CodebaseRefs lists file paths in the target repository retrieved as context examples.
	CodebaseRefs []string `json:"codebase_refs,omitempty"`
}
