package artifacts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/athena-digital/bchad/pkg/artifacts"
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

// loadSchema creates a Compiler with all local schemas registered under their
// $id URLs, then compiles and returns the named schema file.
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

func TestStageArtifactSchema_Valid(t *testing.T) {
	schema := loadSchema(t, "stage_artifact.v1.json")

	artifact := artifacts.StageArtifact{
		StageID:   "migrate",
		StageType: "db_migration",
		Status:    "passed",
		GeneratedFiles: []artifacts.GeneratedFile{
			{
				Path:     "migrations/20260315000001_create_payment_methods.sql",
				Action:   "create",
				Language: "sql",
			},
		},
		Outputs: map[string]string{
			"migration_summary": "Table: payment_methods, Columns: id, merchant_id, type, label, vault_ref",
		},
		GateResult: artifacts.GateResult{
			Passed:        true,
			Tier:          1,
			ErrorCategory: "",
			DurationMS:    1234,
			Checks: []artifacts.GateCheck{
				{Name: "lint", Passed: true, Output: "ok"},
				{Name: "typecheck", Passed: true, Output: "ok"},
			},
		},
		Attempts: 1,
		Cost:     0.04,
	}

	if err := schema.Validate(marshalToAny(t, artifact)); err != nil {
		t.Errorf("valid StageArtifact failed schema validation: %v", err)
	}
}

func TestStageArtifactSchema_Invalid_MissingStageID(t *testing.T) {
	schema := loadSchema(t, "stage_artifact.v1.json")

	invalid := map[string]any{
		"stage_type": "db_migration",
		"status":     "passed",
		"gate_result": map[string]any{
			"passed":         true,
			"tier":           1,
			"error_category": "",
			"duration_ms":    100,
		},
		"attempts": 1,
		"cost":     0.04,
	}

	if err := schema.Validate(invalid); err == nil {
		t.Error("expected schema validation to fail for missing stage_id, but it passed")
	}
}

func TestStageArtifactSchema_Invalid_BadStatus(t *testing.T) {
	schema := loadSchema(t, "stage_artifact.v1.json")

	invalid := map[string]any{
		"stage_id":   "migrate",
		"stage_type": "db_migration",
		"status":     "unknown_status",
		"gate_result": map[string]any{
			"passed":         true,
			"tier":           1,
			"error_category": "",
			"duration_ms":    100,
		},
		"attempts": 1,
		"cost":     0.04,
	}

	if err := schema.Validate(invalid); err == nil {
		t.Error("expected schema validation to fail for invalid status, but it passed")
	}
}

func TestGateResultSchema_Valid(t *testing.T) {
	schema := loadSchema(t, "gate_result.v1.json")

	result := artifacts.GateResult{
		Passed: false,
		Tier:   1,
		Checks: []artifacts.GateCheck{
			{Name: "typecheck", Passed: false, Output: "src/api/routes.ts(42): error TS2345"},
			{Name: "lint", Passed: true, Output: "ok"},
		},
		ErrorOutput:   "TypeScript compilation failed with 1 error",
		ErrorCategory: "type",
		DurationMS:    3210,
	}

	if err := schema.Validate(marshalToAny(t, result)); err != nil {
		t.Errorf("valid GateResult failed schema validation: %v", err)
	}
}

func TestGateResultSchema_Invalid_BadErrorCategory(t *testing.T) {
	schema := loadSchema(t, "gate_result.v1.json")

	invalid := map[string]any{
		"passed":         false,
		"tier":           1,
		"error_category": "not-a-valid-category",
		"duration_ms":    100,
	}

	if err := schema.Validate(invalid); err == nil {
		t.Error("expected schema validation to fail for invalid error_category, but it passed")
	}
}
