package workflows

// ApprovalDecision is the payload sent via ApprovalSignal.
type ApprovalDecision struct {
	// StageID is the stage being approved or rejected.
	StageID string `json:"stage_id"`
	// Decision is either "approve" or "reject".
	Decision string `json:"decision"`
	// GuidanceNote is an optional human note, used when rejecting to guide the retry.
	GuidanceNote string `json:"guidance_note,omitempty"`
}

// PipelineStatus is the response to a StatusQuery.
type PipelineStatus struct {
	RunID          string        `json:"run_id"`
	Status         string        `json:"status"` // "planning", "awaiting_approval", "executing", "complete", "failed", "paused"
	CompletedStages []string     `json:"completed_stages"`
	RunningStage   string        `json:"running_stage,omitempty"`
	PendingStages  []string      `json:"pending_stages"`
	AccumulatedCost float64      `json:"accumulated_cost_usd"`
	StageDetails   []StageDetail `json:"stage_details"`
}

// StageDetail holds per-stage status within a StatusQuery response.
type StageDetail struct {
	StageID  string  `json:"stage_id"`
	Status   string  `json:"status"` // "pending", "awaiting_approval", "running", "passed", "failed"
	Cost     float64 `json:"cost_usd,omitempty"`
	Attempts int     `json:"attempts,omitempty"`
}

// Signal and query name constants used throughout the workflow.
const (
	// ApprovalSignalName is the Temporal signal name for human approval decisions.
	ApprovalSignalName = "approval"
	// StatusQueryName is the Temporal query name for pipeline status.
	StatusQueryName = "status"
	// PipelineTaskQueue is the Temporal task queue name for the BCHAD worker.
	PipelineTaskQueue = "bchad-pipeline"
)
