package workflows

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/testsuite"

	"github.com/athena-digital/bchad/pkg/artifacts"
	"github.com/athena-digital/bchad/pkg/bchadplan"
)

const (
	// testModelHaiku and testModelSonnet mirror the plan package constants for workflow tests.
	testModelHaiku  = "claude-haiku-3-5-sonnet-latest"
	testModelSonnet = "claude-sonnet-4-20250514"
)

// testPlan returns a minimal BCHADPlan fixture for workflow tests.
func testPlan() bchadplan.BCHADPlan {
	return bchadplan.BCHADPlan{
		ID:      "pf-20260315-001",
		Product: "node-express-prisma-v1",
		Pattern: "crud_ui",
		Entity:  "PaymentMethod",
		Stages: []bchadplan.PlanStage{
			{
				ID: "migrate", Type: "db_migration",
				DependsOn: nil, HumanApproval: true,
				Model: testModelHaiku, EstimatedCost: 0.04,
			},
			{
				ID: "config", Type: "feature_flags_and_permissions",
				DependsOn: nil, HumanApproval: false,
				Model: testModelHaiku, EstimatedCost: 0.02,
			},
			{
				ID: "api", Type: "rest_endpoints",
				DependsOn: []string{"migrate"}, HumanApproval: false,
				Model: testModelSonnet, EstimatedCost: 0.53,
			},
			{
				ID: "frontend", Type: "react_components",
				DependsOn: []string{"api"}, HumanApproval: false,
				Model: testModelSonnet, EstimatedCost: 0.61,
			},
			{
				ID: "tests", Type: "test_suite",
				DependsOn: []string{"api", "frontend", "config"}, HumanApproval: false,
				Model: testModelSonnet, EstimatedCost: 0.46,
			},
		},
		ProjectedCost:      1.66,
		HumanApprovalGates: []string{"migrate"},
	}
}

// testInput returns a minimal PipelineInput for workflow tests.
func testInput() PipelineInput {
	return PipelineInput{
		RunID:      "run-test-001",
		Plan:       testPlan(),
		EngineerID: "engineer-alice",
		ProductID:  "node-express-prisma-v1",
		TrustPhase: "supervised",
	}
}

type workflowTestSuite struct {
	testsuite.WorkflowTestSuite
}

func TestPipelineWorkflow_StageOrdering(t *testing.T) {
	s := &workflowTestSuite{}
	env := s.NewTestWorkflowEnvironment()

	var executedStages []string

	// Mock ExecuteStageActivity to record which stages ran.
	env.OnActivity(ExecuteStageActivity, mock.Anything, mock.Anything).Return(
		func(_ context.Context, input StageInput) (*StageOutput, error) {
			executedStages = append(executedStages, input.Stage.ID)
			return &StageOutput{
				Artifact: fixtureArtifact(input.Stage.ID, input.Stage.Type, input.Stage.EstimatedCost),
			}, nil
		},
	)
	env.OnActivity(AssemblePRActivity, mock.Anything, mock.Anything).Return(
		func(_ context.Context, input PRInput) (*PROutput, error) {
			return &PROutput{PRURL: "https://github.com/stub/pull/1", BranchName: "bchad/pf-test"}, nil
		},
	)
	env.OnActivity(Tier2GateActivity, mock.Anything, mock.Anything).Return(
		func(_ context.Context, input Tier2Input) (*Tier2Output, error) {
			return &Tier2Output{Passed: true}, nil
		},
	)

	// Approve the migrate stage signal.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ApprovalSignalName, ApprovalDecision{
			StageID:  "migrate",
			Decision: "approve",
		})
	}, 0)

	env.ExecuteWorkflow(PipelineWorkflow, testInput())

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	// Verify ordering: migrate before api before frontend before tests.
	pos := make(map[string]int)
	for i, id := range executedStages {
		pos[id] = i
	}

	type orderCheck struct{ before, after string }
	checks := []orderCheck{
		{"migrate", "api"},
		{"api", "frontend"},
		{"api", "tests"},
		{"frontend", "tests"},
		{"config", "tests"},
	}
	for _, c := range checks {
		if pos[c.before] >= pos[c.after] {
			t.Errorf("expected %q before %q, but got positions %d vs %d",
				c.before, c.after, pos[c.before], pos[c.after])
		}
	}
}

func TestPipelineWorkflow_ApprovalBlocking(t *testing.T) {
	s := &workflowTestSuite{}
	env := s.NewTestWorkflowEnvironment()

	env.OnActivity(ExecuteStageActivity, mock.Anything, mock.Anything).Return(
		func(_ context.Context, input StageInput) (*StageOutput, error) {
			return &StageOutput{
				Artifact: fixtureArtifact(input.Stage.ID, input.Stage.Type, input.Stage.EstimatedCost),
			}, nil
		},
	)
	env.OnActivity(AssemblePRActivity, mock.Anything, mock.Anything).Return(
		func(_ context.Context, input PRInput) (*PROutput, error) {
			return &PROutput{PRURL: "https://github.com/stub/pull/1", BranchName: "bchad/test"}, nil
		},
	)
	env.OnActivity(Tier2GateActivity, mock.Anything, mock.Anything).Return(
		func(_ context.Context, input Tier2Input) (*Tier2Output, error) {
			return &Tier2Output{Passed: true}, nil
		},
	)

	// Send approval after workflow starts.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ApprovalSignalName, ApprovalDecision{
			StageID:  "migrate",
			Decision: "approve",
		})
	}, 0)

	env.ExecuteWorkflow(PipelineWorkflow, testInput())

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete after approval")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("unexpected workflow error: %v", err)
	}

	var result PipelineOutput
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("GetWorkflowResult: %v", err)
	}
	if result.Status != "complete" {
		t.Errorf("expected status 'complete', got %q", result.Status)
	}
}

func TestPipelineWorkflow_ApprovalRejection(t *testing.T) {
	s := &workflowTestSuite{}
	env := s.NewTestWorkflowEnvironment()

	env.OnActivity(ExecuteStageActivity, mock.Anything, mock.Anything).Return(
		func(_ context.Context, input StageInput) (*StageOutput, error) {
			return &StageOutput{Artifact: fixtureArtifact(input.Stage.ID, input.Stage.Type, input.Stage.EstimatedCost)}, nil
		},
	)

	// Reject the migrate stage.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ApprovalSignalName, ApprovalDecision{
			StageID:      "migrate",
			Decision:     "reject",
			GuidanceNote: "migration SQL needs review",
		})
	}, 0)

	env.ExecuteWorkflow(PipelineWorkflow, testInput())

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete after rejection")
	}
	// Rejection should result in a workflow error.
	if err := env.GetWorkflowError(); err == nil {
		t.Fatal("expected workflow error after rejection, got nil")
	}
}

func TestPipelineWorkflow_StatusQuery(t *testing.T) {
	s := &workflowTestSuite{}
	env := s.NewTestWorkflowEnvironment()

	env.OnActivity(ExecuteStageActivity, mock.Anything, mock.Anything).Return(
		func(_ context.Context, input StageInput) (*StageOutput, error) {
			return &StageOutput{
				Artifact: fixtureArtifact(input.Stage.ID, input.Stage.Type, input.Stage.EstimatedCost),
			}, nil
		},
	)
	env.OnActivity(AssemblePRActivity, mock.Anything, mock.Anything).Return(
		func(_ context.Context, input PRInput) (*PROutput, error) {
			return &PROutput{PRURL: "https://github.com/stub/pull/1", BranchName: "bchad/test"}, nil
		},
	)
	env.OnActivity(Tier2GateActivity, mock.Anything, mock.Anything).Return(
		func(_ context.Context, input Tier2Input) (*Tier2Output, error) {
			return &Tier2Output{Passed: true}, nil
		},
	)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ApprovalSignalName, ApprovalDecision{
			StageID:  "migrate",
			Decision: "approve",
		})
	}, 0)

	env.ExecuteWorkflow(PipelineWorkflow, testInput())

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
}

func TestPipelineWorkflow_CompleteOutput(t *testing.T) {
	s := &workflowTestSuite{}
	env := s.NewTestWorkflowEnvironment()

	env.OnActivity(ExecuteStageActivity, mock.Anything, mock.Anything).Return(
		func(_ context.Context, input StageInput) (*StageOutput, error) {
			return &StageOutput{
				Artifact: fixtureArtifact(input.Stage.ID, input.Stage.Type, input.Stage.EstimatedCost),
			}, nil
		},
	)
	env.OnActivity(AssemblePRActivity, mock.Anything, mock.Anything).Return(
		func(_ context.Context, input PRInput) (*PROutput, error) {
			return &PROutput{
				PRURL:      "https://github.com/athena-digital/payments/pull/42",
				BranchName: "bchad/pf-20260315-001",
			}, nil
		},
	)
	env.OnActivity(Tier2GateActivity, mock.Anything, mock.Anything).Return(
		func(_ context.Context, input Tier2Input) (*Tier2Output, error) {
			return &Tier2Output{Passed: true, CIRunURL: "https://github.com/athena-digital/payments/actions/runs/1"}, nil
		},
	)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ApprovalSignalName, ApprovalDecision{
			StageID:  "migrate",
			Decision: "approve",
		})
	}, 0)

	env.ExecuteWorkflow(PipelineWorkflow, testInput())

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	var result PipelineOutput
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("GetWorkflowResult: %v", err)
	}

	if result.PRURL != "https://github.com/athena-digital/payments/pull/42" {
		t.Errorf("PRURL: got %q", result.PRURL)
	}
	if result.Status != "complete" {
		t.Errorf("Status: got %q, want 'complete'", result.Status)
	}
}

// fixtureArtifact returns a stub StageArtifact for workflow test mocks.
func fixtureArtifact(stageID, stageType string, cost float64) artifacts.StageArtifact {
	return artifacts.StageArtifact{
		StageID:   stageID,
		StageType: stageType,
		Status:    "passed",
		Outputs:   map[string]string{"stub": "value"},
		GateResult: artifacts.GateResult{
			Passed: true,
			Tier:   1,
		},
		Attempts: 1,
		Cost:     cost,
	}
}
