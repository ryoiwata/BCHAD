package workflows

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/athena-digital/bchad/pkg/artifacts"
	"github.com/athena-digital/bchad/pkg/bchadplan"
)

// StageInput is the input to ExecuteStageActivity.
type StageInput struct {
	RunID         string             `json:"run_id"`
	PlanID        string             `json:"plan_id"`
	Stage         bchadplan.PlanStage `json:"stage"`
	UpstreamOutputs map[string]string `json:"upstream_outputs"` // stage_id → outputs JSON
	ProductID     string             `json:"product_id"`
	EngineerID    string             `json:"engineer_id"`
	GuidanceNote  string             `json:"guidance_note,omitempty"` // set on retries after rejection
}

// StageOutput is the output of ExecuteStageActivity.
type StageOutput struct {
	Artifact artifacts.StageArtifact `json:"artifact"`
}

// PRInput is the input to AssemblePRActivity.
type PRInput struct {
	RunID      string   `json:"run_id"`
	PlanID     string   `json:"plan_id"`
	ProductID  string   `json:"product_id"`
	EngineerID string   `json:"engineer_id"`
	StageIDs   []string `json:"stage_ids"`
}

// PROutput is the output of AssemblePRActivity.
type PROutput struct {
	// PRURL is the URL of the created pull request.
	PRURL string `json:"pr_url"`
	// BranchName is the branch created for this pipeline run.
	BranchName string `json:"branch_name"`
}

// Tier2Input is the input to Tier2GateActivity.
type Tier2Input struct {
	RunID     string `json:"run_id"`
	PlanID    string `json:"plan_id"`
	PRURL     string `json:"pr_url"`
	ProductID string `json:"product_id"`
}

// Tier2Output is the output of Tier2GateActivity.
type Tier2Output struct {
	Passed    bool   `json:"passed"`
	CIRunURL  string `json:"ci_run_url,omitempty"`
	ErrorOutput string `json:"error_output,omitempty"`
}

// ExecuteStageActivity is the Phase 2 stub for stage execution.
// Phase 3 will replace this with real LLM generation, context budget allocation,
// verification gates, and error classification.
func ExecuteStageActivity(ctx context.Context, input StageInput) (*StageOutput, error) {
	slog.Info("activity: execute stage (stub)",
		"run_id", input.RunID,
		"stage_id", input.Stage.ID,
		"stage_type", input.Stage.Type,
		"product_id", input.ProductID,
		"model", input.Stage.Model,
	)
	fmt.Printf("[STUB] ExecuteStageActivity: would execute %s stage for %s on %s\n",
		input.Stage.ID, input.ProductID, input.EngineerID)

	// Return a fixture StageArtifact indicating the stage passed.
	artifact := artifacts.StageArtifact{
		StageID:   input.Stage.ID,
		StageType: input.Stage.Type,
		Status:    "passed",
		GeneratedFiles: []artifacts.GeneratedFile{
			{Path: fmt.Sprintf("stub/%s/generated.ts", input.Stage.ID), Action: "create", Language: "typescript"},
		},
		Outputs: map[string]string{
			"stub_output": fmt.Sprintf("stub output for stage %s", input.Stage.ID),
		},
		GateResult: artifacts.GateResult{
			Passed:     true,
			Tier:       1,
			DurationMS: 100,
		},
		Attempts: 1,
		Cost:     input.Stage.EstimatedCost,
	}

	return &StageOutput{Artifact: artifact}, nil
}

// AssemblePRActivity is the Phase 2 stub for PR assembly.
// Phase 3 will replace this with real branch creation, per-stage commits, and PR creation.
func AssemblePRActivity(ctx context.Context, input PRInput) (*PROutput, error) {
	slog.Info("activity: assemble PR (stub)",
		"run_id", input.RunID,
		"plan_id", input.PlanID,
		"product_id", input.ProductID,
	)
	fmt.Printf("[STUB] AssemblePRActivity: would create PR for plan %s on %s\n",
		input.PlanID, input.ProductID)

	branchName := fmt.Sprintf("bchad/%s", input.PlanID)

	return &PROutput{
		PRURL:      fmt.Sprintf("https://github.com/athena-digital/%s/pull/stub-1234", input.ProductID),
		BranchName: branchName,
	}, nil
}

// Tier2GateActivity is the Phase 2 stub for Tier 2 (full CI) gate execution.
// Phase 3 will replace this with real ECS Fargate task dispatch and CI result polling.
func Tier2GateActivity(ctx context.Context, input Tier2Input) (*Tier2Output, error) {
	slog.Info("activity: tier2 gate (stub)",
		"run_id", input.RunID,
		"plan_id", input.PlanID,
		"pr_url", input.PRURL,
	)
	fmt.Printf("[STUB] Tier2GateActivity: would run full CI for PR %s\n", input.PRURL)

	return &Tier2Output{
		Passed:   true,
		CIRunURL: fmt.Sprintf("%s/checks", input.PRURL),
	}, nil
}
