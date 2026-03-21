package plan

import (
	"context"
	"os"
	"regexp"
	"testing"

	"github.com/athena-digital/bchad/internal/spec"
)

func loadPaymentMethodsSpec(t *testing.T) *spec.ParsedSpec {
	t.Helper()
	data, err := os.ReadFile("../../testdata/specs/payment-methods.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	schemaPath := "../../schemas/bchadspec.v1.json"
	ps, err := spec.Parse(data, schemaPath)
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	return ps
}

func TestGeneratePlan_CRUDUIProduces5Stages(t *testing.T) {
	ps := loadPaymentMethodsSpec(t)
	g := NewGenerator()

	plan, err := g.Generate(context.Background(), ps)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	if len(plan.Stages) != 5 {
		t.Errorf("expected 5 stages, got %d", len(plan.Stages))
	}

	stageIDs := make([]string, len(plan.Stages))
	for i, s := range plan.Stages {
		stageIDs[i] = s.ID
	}

	want := []string{"migrate", "config", "api", "frontend", "tests"}
	for i, id := range want {
		if i >= len(stageIDs) || stageIDs[i] != id {
			t.Errorf("stage[%d]: got %q, want %q", i, stageIDs[i], id)
		}
	}
}

func TestGeneratePlan_DependencyOrdering(t *testing.T) {
	ps := loadPaymentMethodsSpec(t)
	g := NewGenerator()

	plan, err := g.Generate(context.Background(), ps)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	stageByID := make(map[string]int)
	for i, s := range plan.Stages {
		stageByID[s.ID] = i
	}

	tests := []struct {
		stage string
		dep   string
	}{
		{"api", "migrate"},
		{"frontend", "api"},
		{"tests", "api"},
		{"tests", "frontend"},
		{"tests", "config"},
	}

	for _, tt := range tests {
		stage := plan.Stages[stageByID[tt.stage]]
		found := false
		for _, d := range stage.DependsOn {
			if d == tt.dep {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("stage %q should depend on %q, but depends_on = %v", tt.stage, tt.dep, stage.DependsOn)
		}
	}

	// migrate and config have no dependencies.
	for _, noDep := range []string{"migrate", "config"} {
		stage := plan.Stages[stageByID[noDep]]
		if len(stage.DependsOn) != 0 {
			t.Errorf("stage %q should have no dependencies, got %v", noDep, stage.DependsOn)
		}
	}
}

func TestGeneratePlan_ModelSelection(t *testing.T) {
	ps := loadPaymentMethodsSpec(t)
	g := NewGenerator()

	plan, err := g.Generate(context.Background(), ps)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	stageByID := make(map[string]string)
	for _, s := range plan.Stages {
		stageByID[s.ID] = s.Model
	}

	haikuStages := []string{"migrate", "config"}
	for _, id := range haikuStages {
		if stageByID[id] != modelHaiku {
			t.Errorf("stage %q: expected haiku model %q, got %q", id, modelHaiku, stageByID[id])
		}
	}

	sonnetStages := []string{"api", "frontend", "tests"}
	for _, id := range sonnetStages {
		if stageByID[id] != modelSonnet {
			t.Errorf("stage %q: expected sonnet model %q, got %q", id, modelSonnet, stageByID[id])
		}
	}
}

func TestGeneratePlan_MigrateRequiresApproval(t *testing.T) {
	ps := loadPaymentMethodsSpec(t)
	g := NewGenerator()

	plan, err := g.Generate(context.Background(), ps)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	for _, s := range plan.Stages {
		if s.ID == "migrate" && !s.HumanApproval {
			t.Error("migrate stage should have HumanApproval=true")
		}
	}

	found := false
	for _, gate := range plan.HumanApprovalGates {
		if gate == "migrate" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'migrate' in HumanApprovalGates")
	}
}

func TestGeneratePlan_PlanIDFormat(t *testing.T) {
	ps := loadPaymentMethodsSpec(t)
	g := NewGenerator()

	plan, err := g.Generate(context.Background(), ps)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	re := regexp.MustCompile(`^pf-\d{8}-\d{3}$`)
	if !re.MatchString(plan.ID) {
		t.Errorf("plan ID %q does not match pattern pf-YYYYMMDD-NNN", plan.ID)
	}
}

func TestGeneratePlan_CodebaseRefsPopulated(t *testing.T) {
	ps := loadPaymentMethodsSpec(t)
	g := NewGenerator()

	plan, err := g.Generate(context.Background(), ps)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	for _, s := range plan.Stages {
		if len(s.CodebaseRefs) == 0 {
			t.Errorf("stage %q has no codebase_refs", s.ID)
		}
	}
}

func TestGeneratePlan_NilSpecReturnsError(t *testing.T) {
	g := NewGenerator()

	_, err := g.Generate(context.Background(), nil)
	if err == nil {
		t.Fatal("Generate(nil) expected error, got nil")
	}
}
