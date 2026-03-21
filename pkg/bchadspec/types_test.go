package bchadspec_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/athena-digital/bchad/pkg/bchadspec"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// schemasDir returns the absolute path to the schemas/ directory.
func schemasDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// pkg/bchadspec/types_test.go → ../../schemas
	return filepath.Join(filepath.Dir(filename), "..", "..", "schemas")
}

// loadSchema creates a jsonschema Compiler with all local schemas registered
// under their $id URLs, then compiles and returns the named schema.
func loadSchema(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	dir := schemasDir(t)

	c := jsonschema.NewCompiler()

	// Register all schema files under their $id URLs.
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

	// Find the $id for the requested schema file.
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

func TestBCHADSpecSchema_Valid(t *testing.T) {
	schema := loadSchema(t, "bchadspec.v1.json")

	spec := bchadspec.BCHADSpec{
		Product: "payments-dashboard",
		Pattern: "crud_ui",
		Entity: bchadspec.EntitySpec{
			Name: "PaymentMethod",
			Fields: []bchadspec.FieldSpec{
				{
					Name:     "label",
					Kind:     "string",
					Required: true,
				},
				{
					Name:   "type",
					Kind:   "enum",
					Values: []string{"credit_card", "ach", "wire"},
				},
				{
					Name:      "vault_ref",
					Kind:      "string",
					Sensitive: true,
				},
			},
		},
		Permissions:  "payment_methods:manage",
		Audit:        true,
		Integrations: []string{"vault"},
		UI: bchadspec.UISpec{
			List:   true,
			Detail: true,
			Form:   true,
		},
		Compliance: bchadspec.ComplianceFlags{
			SOC2: true,
		},
	}

	if err := schema.Validate(marshalToAny(t, spec)); err != nil {
		t.Errorf("valid BCHADSpec failed schema validation: %v", err)
	}
}

func TestBCHADSpecSchema_Invalid_MissingProduct(t *testing.T) {
	schema := loadSchema(t, "bchadspec.v1.json")

	invalid := map[string]any{
		"pattern": "crud_ui",
		"entity": map[string]any{
			"name": "Widget",
			"fields": []any{
				map[string]any{"name": "title", "kind": "string"},
			},
		},
	}

	if err := schema.Validate(invalid); err == nil {
		t.Error("expected schema validation to fail for missing product, but it passed")
	}
}

func TestBCHADSpecSchema_Invalid_MissingEntity(t *testing.T) {
	schema := loadSchema(t, "bchadspec.v1.json")

	invalid := map[string]any{
		"product": "payments-dashboard",
		"pattern": "crud_ui",
	}

	if err := schema.Validate(invalid); err == nil {
		t.Error("expected schema validation to fail for missing entity, but it passed")
	}
}

func TestBCHADSpecSchema_Invalid_BadPattern(t *testing.T) {
	schema := loadSchema(t, "bchadspec.v1.json")

	invalid := map[string]any{
		"product": "payments-dashboard",
		"pattern": "unknown_pattern",
		"entity": map[string]any{
			"name": "Widget",
			"fields": []any{
				map[string]any{"name": "title", "kind": "string"},
			},
		},
	}

	if err := schema.Validate(invalid); err == nil {
		t.Error("expected schema validation to fail for invalid pattern, but it passed")
	}
}

func TestBCHADSpecSchema_Invalid_EnumWithoutValues(t *testing.T) {
	schema := loadSchema(t, "bchadspec.v1.json")

	invalid := map[string]any{
		"product": "payments-dashboard",
		"pattern": "crud_ui",
		"entity": map[string]any{
			"name": "Widget",
			"fields": []any{
				map[string]any{
					"name": "status",
					"kind": "enum",
				},
			},
		},
	}

	if err := schema.Validate(invalid); err == nil {
		t.Error("expected schema validation to fail for enum field without values, but it passed")
	}
}

func TestBCHADSpecSchema_Minimal(t *testing.T) {
	schema := loadSchema(t, "bchadspec.v1.json")

	minimal := bchadspec.BCHADSpec{
		Product: "claims-portal",
		Pattern: "crud_ui",
		Entity: bchadspec.EntitySpec{
			Name: "Widget",
			Fields: []bchadspec.FieldSpec{
				{Name: "title", Kind: "string"},
			},
		},
	}

	if err := schema.Validate(marshalToAny(t, minimal)); err != nil {
		t.Errorf("minimal valid BCHADSpec failed schema validation: %v", err)
	}
}
