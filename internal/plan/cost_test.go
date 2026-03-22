package plan

import (
	"context"
	"math"
	"os"
	"testing"

	"github.com/athena-digital/bchad/internal/spec"
)

func TestEstimateStageCost_KnownStages(t *testing.T) {
	tests := []struct {
		stageID  string
		minCost  float64
		maxCost  float64
	}{
		// From the framework doc cost model (including retry multiplier):
		// migrate: ~$0.04, config: ~$0.02, api: ~$0.53, frontend: ~$0.61, tests: ~$0.46
		{"migrate", 0.03, 0.06},
		{"config", 0.01, 0.04},
		{"api", 0.40, 0.70},
		{"frontend", 0.45, 0.80},
		{"tests", 0.35, 0.60},
	}

	for _, tt := range tests {
		t.Run(tt.stageID, func(t *testing.T) {
			cost, err := EstimateStageCost(tt.stageID)
			if err != nil {
				t.Fatalf("EstimateStageCost(%q) error: %v", tt.stageID, err)
			}
			if cost < tt.minCost || cost > tt.maxCost {
				t.Errorf("EstimateStageCost(%q) = $%.4f, want $%.4f–$%.4f",
					tt.stageID, cost, tt.minCost, tt.maxCost)
			}
		})
	}
}

func TestEstimateStageCost_UnknownStage(t *testing.T) {
	_, err := EstimateStageCost("unknown_stage")
	if err == nil {
		t.Fatal("EstimateStageCost(unknown) expected error, got nil")
	}
}

func TestEstimatePlanCost_TotalInRange(t *testing.T) {
	data, err := os.ReadFile("../../testdata/specs/payment-methods.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ps, err := spec.Parse(data, "../../schemas/bchadspec.v1.json")
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	g := NewGenerator()
	plan, err := g.Generate(context.Background(), ps)
	if err != nil {
		t.Fatalf("Generate(): %v", err)
	}

	// Framework doc total: ~$1.66. Allow a reasonable range.
	const wantMin, wantMax = 1.00, 2.50
	if plan.ProjectedCost < wantMin || plan.ProjectedCost > wantMax {
		t.Errorf("ProjectedCost = $%.4f, want $%.2f–$%.2f", plan.ProjectedCost, wantMin, wantMax)
	}
}

func TestEstimatePlanCost_StageCostsSet(t *testing.T) {
	data, err := os.ReadFile("../../testdata/specs/payment-methods.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ps, err := spec.Parse(data, "../../schemas/bchadspec.v1.json")
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	g := NewGenerator()
	plan, err := g.Generate(context.Background(), ps)
	if err != nil {
		t.Fatalf("Generate(): %v", err)
	}

	var stageSum float64
	for _, s := range plan.Stages {
		if s.EstimatedCost <= 0 {
			t.Errorf("stage %q EstimatedCost is zero or negative", s.ID)
		}
		stageSum += s.EstimatedCost
	}

	// Plan total should equal the sum of stage costs (within floating point epsilon).
	if math.Abs(plan.ProjectedCost-stageSum) > 0.001 {
		t.Errorf("ProjectedCost $%.6f != sum of stage costs $%.6f", plan.ProjectedCost, stageSum)
	}
}

func TestCostThresholdFlag(t *testing.T) {
	// A plan with total cost < $10 should not be flagged.
	// The CRUD+UI plan is ~$1.66 — well under the $10 default threshold.
	data, err := os.ReadFile("../../testdata/specs/payment-methods.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ps, err := spec.Parse(data, "../../schemas/bchadspec.v1.json")
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	g := NewGenerator()
	plan, err := g.Generate(context.Background(), ps)
	if err != nil {
		t.Fatalf("Generate(): %v", err)
	}

	const threshold = 10.0
	if plan.ProjectedCost > threshold {
		t.Errorf("expected plan cost $%.4f < threshold $%.2f", plan.ProjectedCost, threshold)
	}
}
