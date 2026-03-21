// Package artifacts defines the StageArtifact, GateResult, GeneratedFile, TrustScore,
// and TrustSignals types. These are the contracts between pipeline stages and between
// the execution plane and the trust scoring system.
package artifacts

// StageArtifact is the output of a completed pipeline stage. It is stored in S3 and
// referenced by ID in downstream stage prompts. Validated against schemas/stage_artifact.v1.json.
type StageArtifact struct {
	// StageID is the stage identifier matching the BCHADPlan stage ID.
	StageID string `json:"stage_id"`

	// StageType is the stage type descriptor (e.g. "db_migration", "rest_endpoints").
	StageType string `json:"stage_type"`

	// Status is the completion status of the stage.
	// Valid values: "passed", "failed", "awaiting_approval", "skipped".
	Status string `json:"status"`

	// GeneratedFiles lists the files created or modified by this stage.
	GeneratedFiles []GeneratedFile `json:"generated_files,omitempty"`

	// Outputs is a key-value map of structured outputs produced by this stage,
	// consumed by downstream stages. Keys are output identifiers such as
	// "schema_definition", "endpoint_contracts", "component_paths", "migration_summary".
	Outputs map[string]string `json:"outputs,omitempty"`

	// GateResult is the verification gate result for this stage.
	GateResult GateResult `json:"gate_result"`

	// Attempts is the total number of generation attempts made, including the final one.
	Attempts int `json:"attempts"`

	// Cost is the total LLM cost for this stage across all attempts, in USD.
	Cost float64 `json:"cost"`
}

// GeneratedFile is a file that was created or modified by a stage.
type GeneratedFile struct {
	// Path is the file path relative to the repository root.
	Path string `json:"path"`

	// Action indicates whether the file was created new or modified in-place.
	// Valid values: "create", "modify".
	Action string `json:"action"`

	// Language is the programming language of the file (e.g. "typescript", "python", "sql").
	Language string `json:"language,omitempty"`
}

// GateResult is the output of a verification gate. Validated against schemas/gate_result.v1.json.
type GateResult struct {
	// Passed indicates whether all checks in this gate passed.
	Passed bool `json:"passed"`

	// Tier is the gate tier: 1 (per-stage, fast) or 2 (integration, full CI).
	Tier int `json:"tier"`

	// Checks lists the individual check results within this gate.
	Checks []GateCheck `json:"checks,omitempty"`

	// ErrorOutput is the raw error output captured from the verification container.
	// Full content is stored in S3; this field may be truncated.
	ErrorOutput string `json:"error_output,omitempty"`

	// ErrorCategory is the error classification used to route the retry strategy.
	// Empty string when the gate passed.
	// Valid values: "syntax", "style", "type", "logic", "context", "conflict",
	// "security", "specification", "".
	ErrorCategory string `json:"error_category"`

	// DurationMS is the wall-clock duration of the gate execution in milliseconds.
	DurationMS int `json:"duration_ms"`
}

// GateCheck is the result of a single check within a gate.
type GateCheck struct {
	// Name is the check identifier (e.g. "typecheck", "lint", "route_conflicts", "semgrep").
	Name string `json:"name"`

	// Passed indicates whether this individual check passed.
	Passed bool `json:"passed"`

	// Output is the brief error or success message from this check.
	Output string `json:"output,omitempty"`
}

// TrustScore is the computed trust level for a specific engineer on a specific product.
// Trust is per-engineer per-product — a senior engineer can be Phase 3 on one product
// and Phase 1 on another.
type TrustScore struct {
	// EngineerID is the unique identifier of the engineer.
	EngineerID string `json:"engineer_id"`

	// ProductID is the product this trust score applies to.
	ProductID string `json:"product_id"`

	// Score is the computed numeric trust score (0–100).
	Score float64 `json:"score"`

	// Phase is the trust phase based on the score and completed run count.
	// Valid values: "supervised", "gated", "monitored".
	Phase string `json:"phase"`

	// Signals contains the raw signal values used to compute the score.
	Signals TrustSignals `json:"signals"`
}

// TrustSignals contains the raw signal values used to compute a TrustScore.
// Each signal has a defined weight in the scoring formula.
type TrustSignals struct {
	// CIPassRate is the fraction of pipeline runs where the PR passed CI without edits.
	// Weight: 0.30.
	CIPassRate float64 `json:"ci_pass_rate"`

	// EditVolume is the fraction of generated lines that the engineer edited before merge.
	// Lower is better (indicates the output required less manual correction).
	// Weight: 0.25.
	EditVolume float64 `json:"edit_volume"`

	// RetryRate is the average number of retries per stage across recent runs.
	// Lower is better. Weight: 0.15.
	RetryRate float64 `json:"retry_rate"`

	// OverrideCount is the count of manual approval overrides in recent runs.
	// Lower is better. Weight: 0.15.
	OverrideCount float64 `json:"override_count"`

	// TimeToMerge is the normalized time from PR creation to merge.
	// Lower (faster merge) is better. Weight: 0.15.
	TimeToMerge float64 `json:"time_to_merge"`

	// CompletedRuns is the total number of completed pipeline runs used to build this score.
	CompletedRuns int `json:"completed_runs"`
}
