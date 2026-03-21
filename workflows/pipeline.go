package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/athena-digital/bchad/pkg/bchadplan"
)

// PipelineInput is the input to PipelineWorkflow.
type PipelineInput struct {
	RunID      string           `json:"run_id"`
	Plan       bchadplan.BCHADPlan `json:"plan"`
	EngineerID string           `json:"engineer_id"`
	ProductID  string           `json:"product_id"`
	TrustPhase string           `json:"trust_phase"` // "supervised", "gated", "monitored"
}

// PipelineOutput is the result of a completed PipelineWorkflow.
type PipelineOutput struct {
	RunID          string  `json:"run_id"`
	Status         string  `json:"status"` // "complete" or "failed"
	PRURL          string  `json:"pr_url,omitempty"`
	AccumulatedCost float64 `json:"accumulated_cost_usd"`
}

// PipelineWorkflow orchestrates a full spec-to-PR pipeline run.
// It executes stages in dependency order, blocks on approval signals before
// gated stages, and tracks accumulated cost. Stage execution is stubbed in Phase 2.
//
// Temporal requirements:
//   - This function must be deterministic — no I/O, no random, no time.Now() directly.
//   - All I/O happens in activities called via workflow.ExecuteActivity.
//   - Signals are received via workflow.GetSignalChannel.
//   - Queries are served via workflow.SetQueryHandler.
func PipelineWorkflow(ctx workflow.Context, input PipelineInput) (*PipelineOutput, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("pipeline: starting",
		"run_id", input.RunID,
		"plan_id", input.Plan.ID,
		"product", input.ProductID,
		"engineer", input.EngineerID,
		"stages", len(input.Plan.Stages),
	)

	// Set search attributes for Temporal dashboard filtering.
	// These enable filtering by product, engineer, and trust phase in the Temporal UI.
	_ = workflow.UpsertTypedSearchAttributes(ctx,
		temporal.NewSearchAttributeKeyKeyword("product").ValueSet(input.ProductID),
		temporal.NewSearchAttributeKeyKeyword("engineer").ValueSet(input.EngineerID),
		temporal.NewSearchAttributeKeyKeyword("trust_phase").ValueSet(input.TrustPhase),
	)

	// --- Signal channel ---
	approvalCh := workflow.GetSignalChannel(ctx, ApprovalSignalName)

	// --- Mutable workflow state (updated by activities and signals) ---
	status := &PipelineStatus{
		RunID:  input.RunID,
		Status: "executing",
	}
	for _, s := range input.Plan.Stages {
		status.PendingStages = append(status.PendingStages, s.ID)
		status.StageDetails = append(status.StageDetails, StageDetail{
			StageID: s.ID,
			Status:  "pending",
		})
	}

	// --- Query handler: returns current pipeline status ---
	if err := workflow.SetQueryHandler(ctx, StatusQueryName, func() (*PipelineStatus, error) {
		return status, nil
	}); err != nil {
		return nil, fmt.Errorf("register status query handler: %w", err)
	}

	// --- Activity options ---
	// Use non-blocking heartbeat context; individual retries are per error category (Phase 3).
	actOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Minute,
		HeartbeatTimeout:    30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1, // Phase 2: no retries in stubs; Phase 3 will use category-specific policies.
		},
	}
	actCtx := workflow.WithActivityOptions(ctx, actOpts)

	// Map from stage ID to its artifact outputs, for passing to downstream stages.
	upstreamOutputs := make(map[string]string)
	var accumulatedCost float64

	// --- Execute stages sequentially in dependency order ---
	// v1 is strictly sequential: migrate → config → api → frontend → tests.
	// v2 will use parallel fan-out/fan-in for independent stages.
	for i := range input.Plan.Stages {
		stage := input.Plan.Stages[i]

		// Update status: this stage is now running.
		markStageRunning(status, stage.ID)

		// Block on approval signal if this stage requires human approval.
		if stage.HumanApproval {
			status.Status = "awaiting_approval"
			logger.Info("pipeline: awaiting approval",
				"run_id", input.RunID,
				"stage_id", stage.ID,
			)

			decision, timedOut := waitForApproval(ctx, approvalCh, stage.ID, 24*time.Hour)
			if timedOut {
				status.Status = "failed"
				markStageFailed(status, stage.ID)
				return &PipelineOutput{
					RunID:           input.RunID,
					Status:          "failed",
					AccumulatedCost: accumulatedCost,
				}, fmt.Errorf("approval timeout: stage %s approval deadline exceeded", stage.ID)
			}
			if decision.Decision == "reject" {
				status.Status = "paused"
				markStageFailed(status, stage.ID)
				logger.Warn("pipeline: stage rejected by engineer",
					"run_id", input.RunID,
					"stage_id", stage.ID,
					"guidance", decision.GuidanceNote,
				)
				return &PipelineOutput{
					RunID:           input.RunID,
					Status:          "failed",
					AccumulatedCost: accumulatedCost,
				}, fmt.Errorf("stage %s rejected: %s", stage.ID, decision.GuidanceNote)
			}
			status.Status = "executing"
		}

		// Execute the stage activity.
		stageInput := StageInput{
			RunID:           input.RunID,
			PlanID:          input.Plan.ID,
			Stage:           stage,
			UpstreamOutputs: upstreamOutputs,
			ProductID:       input.ProductID,
			EngineerID:      input.EngineerID,
		}

		var stageOut StageOutput
		if err := workflow.ExecuteActivity(actCtx, ExecuteStageActivity, stageInput).Get(ctx, &stageOut); err != nil {
			status.Status = "failed"
			markStageFailed(status, stage.ID)
			return &PipelineOutput{
				RunID:           input.RunID,
				Status:          "failed",
				AccumulatedCost: accumulatedCost,
			}, fmt.Errorf("stage %s failed: %w", stage.ID, err)
		}

		// Accumulate cost and propagate outputs to downstream stages.
		accumulatedCost += stageOut.Artifact.Cost
		status.AccumulatedCost = accumulatedCost
		for k, v := range stageOut.Artifact.Outputs {
			upstreamOutputs[stage.ID+"."+k] = v
		}

		markStageComplete(status, stage.ID, stageOut.Artifact.Cost)
	}

	// --- Assemble PR ---
	status.Status = "assembling_pr"
	status.RunningStage = "pr_assembly"

	prInput := PRInput{
		RunID:      input.RunID,
		PlanID:     input.Plan.ID,
		ProductID:  input.ProductID,
		EngineerID: input.EngineerID,
		RepoURL:    input.Plan.RepoURL,
	}
	for _, s := range input.Plan.Stages {
		prInput.StageIDs = append(prInput.StageIDs, s.ID)
	}

	var prOut PROutput
	if err := workflow.ExecuteActivity(actCtx, AssemblePRActivity, prInput).Get(ctx, &prOut); err != nil {
		status.Status = "failed"
		return &PipelineOutput{
			RunID:           input.RunID,
			Status:          "failed",
			AccumulatedCost: accumulatedCost,
		}, fmt.Errorf("PR assembly failed: %w", err)
	}

	// --- Tier 2 gate ---
	status.Status = "tier2_gate"

	tier2Input := Tier2Input{
		RunID:     input.RunID,
		PlanID:    input.Plan.ID,
		PRURL:     prOut.PRURL,
		ProductID: input.ProductID,
	}

	var tier2Out Tier2Output
	if err := workflow.ExecuteActivity(actCtx, Tier2GateActivity, tier2Input).Get(ctx, &tier2Out); err != nil {
		status.Status = "failed"
		return &PipelineOutput{
			RunID:           input.RunID,
			Status:          "failed",
			AccumulatedCost: accumulatedCost,
		}, fmt.Errorf("tier 2 gate failed: %w", err)
	}

	if !tier2Out.Passed {
		status.Status = "failed"
		return &PipelineOutput{
			RunID:           input.RunID,
			Status:          "failed",
			AccumulatedCost: accumulatedCost,
		}, fmt.Errorf("tier 2 CI gate did not pass")
	}

	status.Status = "complete"
	status.RunningStage = ""

	logger.Info("pipeline: complete",
		"run_id", input.RunID,
		"pr_url", prOut.PRURL,
		"accumulated_cost_usd", accumulatedCost,
	)

	return &PipelineOutput{
		RunID:           input.RunID,
		Status:          "complete",
		PRURL:           prOut.PRURL,
		AccumulatedCost: accumulatedCost,
	}, nil
}

// waitForApproval blocks on the approval signal for the given stage, with a timeout.
// Returns the decision and whether the timeout was reached.
func waitForApproval(ctx workflow.Context, ch workflow.ReceiveChannel, stageID string, timeout time.Duration) (ApprovalDecision, bool) {
	var decision ApprovalDecision
	timedOut := false

	sel := workflow.NewSelector(ctx)

	sel.AddReceive(ch, func(c workflow.ReceiveChannel, more bool) {
		c.Receive(ctx, &decision)
	})

	timerFired := false
	timer := workflow.NewTimer(ctx, timeout)
	sel.AddFuture(timer, func(f workflow.Future) {
		timerFired = true
	})

	sel.Select(ctx)
	timedOut = timerFired
	return decision, timedOut
}

// markStageRunning updates the status to show the given stage as running.
func markStageRunning(s *PipelineStatus, stageID string) {
	s.RunningStage = stageID
	for i, d := range s.StageDetails {
		if d.StageID == stageID {
			s.StageDetails[i].Status = "running"
		}
	}
	// Remove from pending list.
	pending := s.PendingStages[:0]
	for _, id := range s.PendingStages {
		if id != stageID {
			pending = append(pending, id)
		}
	}
	s.PendingStages = pending
}

// markStageComplete moves the stage from running to completed and records its cost.
func markStageComplete(s *PipelineStatus, stageID string, cost float64) {
	s.CompletedStages = append(s.CompletedStages, stageID)
	if s.RunningStage == stageID {
		s.RunningStage = ""
	}
	for i, d := range s.StageDetails {
		if d.StageID == stageID {
			s.StageDetails[i].Status = "passed"
			s.StageDetails[i].Cost = cost
			s.StageDetails[i].Attempts++
		}
	}
}

// markStageFailed marks the stage as failed in the status.
func markStageFailed(s *PipelineStatus, stageID string) {
	if s.RunningStage == stageID {
		s.RunningStage = ""
	}
	for i, d := range s.StageDetails {
		if d.StageID == stageID {
			s.StageDetails[i].Status = "failed"
		}
	}
}

