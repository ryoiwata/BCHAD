package spec

import (
	"os"
	"strings"
	"testing"
)

func TestValidate_ValidSpec(t *testing.T) {
	v, err := NewValidator(testSchemaPath(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	data := []byte(`{
		"product": "payments-dashboard",
		"pattern": "crud_ui",
		"entity": {
			"name": "PaymentMethod",
			"fields": [
				{"name": "label", "kind": "string", "required": true},
				{"name": "type", "kind": "enum", "values": ["credit_card", "ach"], "required": true}
			]
		}
	}`)

	if err := v.Validate(data); err != nil {
		t.Errorf("Validate() returned unexpected error: %v", err)
	}
}

func TestValidate_ReturnsAllErrors(t *testing.T) {
	v, err := NewValidator(testSchemaPath(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	// Missing both "product" and "entity" — should produce multiple errors.
	data := []byte(`{"pattern": "crud_ui"}`)

	err = v.Validate(data)
	if err == nil {
		t.Fatal("Validate() expected error, got nil")
	}

	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("expected 'validation failed' in error, got: %s", err.Error())
	}
}

func TestValidate_InvalidKindEnum(t *testing.T) {
	v, err := NewValidator(testSchemaPath(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	data := []byte(`{
		"product": "payments-dashboard",
		"pattern": "crud_ui",
		"entity": {
			"name": "Foo",
			"fields": [{"name": "bar", "kind": "text"}]
		}
	}`)

	if err := v.Validate(data); err == nil {
		t.Fatal("Validate() expected error for invalid kind, got nil")
	}
}

func TestValidate_InvalidJSON(t *testing.T) {
	v, err := NewValidator(testSchemaPath(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	if err := v.Validate([]byte(`{not valid json`)); err == nil {
		t.Fatal("Validate() expected error for invalid JSON, got nil")
	}
}

func TestValidate_EnumFieldRequiresValues(t *testing.T) {
	v, err := NewValidator(testSchemaPath(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	// enum field without values — should fail the JSON Schema conditional constraint.
	data := []byte(`{
		"product": "payments-dashboard",
		"pattern": "crud_ui",
		"entity": {
			"name": "Foo",
			"fields": [{"name": "type", "kind": "enum"}]
		}
	}`)

	if err := v.Validate(data); err == nil {
		t.Fatal("Validate() expected error for enum without values, got nil")
	}
}

func TestNewValidatorFromBytes(t *testing.T) {
	schemaBytes, err := os.ReadFile(testSchemaPath(t))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}

	v, err := NewValidatorFromBytes("bchadspec.v1.json", schemaBytes)
	if err != nil {
		t.Fatalf("NewValidatorFromBytes: %v", err)
	}

	data := []byte(`{
		"product": "test",
		"pattern": "crud_ui",
		"entity": {"name": "Item", "fields": [{"name": "name", "kind": "string"}]}
	}`)

	if err := v.Validate(data); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
}
