package bchadplan_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/athena-digital/bchad/pkg/bchadplan"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func schemasDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(filename), "..", "..", "schemas")
}

func loadSchema(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	dir := schemasDir(t)

	c := jsonschema.NewCompiler()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read schemas dir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read schema %s: %v", e.Name(), err)
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("parse schema %s: %v", e.Name(), err)
		}
		id, _ := raw["$id"].(string)
		if id == "" {
			t.Fatalf("schema %s has no $id", e.Name())
		}
		// Pass the decoded map (not io.Reader) — v6 requires pre-decoded JSON.
		if err := c.AddResource(id, raw); err != nil {
			t.Fatalf("add resource %s: %v", id, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read schema %s: %v", name, err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse schema %s: %v", name, err)
	}
	id, _ := raw["$id"].(string)

	schema, err := c.Compile(id)
	if err != nil {
		t.Fatalf("compile schema %s: %v", id, err)
	}
	return schema
}

func marshalToAny(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestBCHADPlanSchema_Valid(t *testing.T) {
	schema := loadSchema(t, "bchadplan.v1.json")

	plan := bchadplan.BCHADPlan{
		ID:            "pf-20260315-001",
		Product:       "payments-dashboard",
		Pattern:       "crud_ui",
		Entity:        "PaymentMethod",
		ProjectedCost: 1.72,
		Stages: []bchadplan.PlanStage{
			{
				ID:             "migrate",
				Type:           "db_migration",
				DependsOn:      []string{},
				HumanApproval:  true,
				Model:          "claude-haiku-3.5",
				Description:    "Create payment_methods table with indexes",
				EstimatedFiles: 1,
				EstimatedCost:  0.04,
			},
			{
				ID:             "config",
				Type:           "feature_flags_and_permissions",
				DependsOn:      []string{},
				HumanApproval:  false,
				Model:          "claude-haiku-3.5",
				Description:    "Register payment_methods:manage permission",
				EstimatedFiles: 2,
				EstimatedCost:  0.02,
			},
			{
				ID:             "api",
				Type:           "rest_endpoints",
				DependsOn:      []string{"migrate"},
				HumanApproval:  false,
				Model:          "claude-sonnet-4",
				Description:    "Generate CRUD REST endpoints for PaymentMethod",
				EstimatedFiles: 4,
				EstimatedCost:  0.80,
			},
		},
		EstimatedTotalFiles: 7,
		HumanApprovalGates:  []string{"migrate"},
		SecurityReview:      false,
	}

	if err := schema.Validate(marshalToAny(t, plan)); err != nil {
		t.Errorf("valid BCHADPlan failed schema validation: %v", err)
	}
}

func TestBCHADPlanSchema_Invalid_MissingID(t *testing.T) {
	schema := loadSchema(t, "bchadplan.v1.json")

	invalid := map[string]any{
		"product":        "payments-dashboard",
		"pattern":        "crud_ui",
		"entity":         "PaymentMethod",
		"projected_cost": 1.72,
		"stages":         []any{},
	}

	if err := schema.Validate(invalid); err == nil {
		t.Error("expected schema validation to fail for missing id, but it passed")
	}
}

func TestBCHADPlanSchema_Invalid_BadIDFormat(t *testing.T) {
	schema := loadSchema(t, "bchadplan.v1.json")

	invalid := map[string]any{
		"id":             "not-a-valid-id",
		"product":        "payments-dashboard",
		"pattern":        "crud_ui",
		"entity":         "PaymentMethod",
		"projected_cost": 1.72,
		"stages": []any{
			map[string]any{
				"id":             "migrate",
				"type":           "db_migration",
				"human_approval": true,
				"model":          "claude-haiku-3.5",
				"description":    "Create table",
			},
		},
	}

	if err := schema.Validate(invalid); err == nil {
		t.Error("expected schema validation to fail for malformed id, but it passed")
	}
}

func TestBCHADPlanSchema_Invalid_EmptyStages(t *testing.T) {
	schema := loadSchema(t, "bchadplan.v1.json")

	invalid := map[string]any{
		"id":             "pf-20260315-001",
		"product":        "payments-dashboard",
		"pattern":        "crud_ui",
		"entity":         "PaymentMethod",
		"projected_cost": 1.72,
		"stages":         []any{},
	}

	if err := schema.Validate(invalid); err == nil {
		t.Error("expected schema validation to fail for empty stages, but it passed")
	}
}
