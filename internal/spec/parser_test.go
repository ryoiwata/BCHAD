package spec

import (
	"os"
	"path/filepath"
	"testing"
)

func testSchemaPath(t *testing.T) string {
	t.Helper()
	// From internal/spec/, the schemas dir is ../../schemas/
	path, err := filepath.Abs("../../schemas/bchadspec.v1.json")
	if err != nil {
		t.Fatalf("resolve schema path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("schema not found at %s: %v", path, err)
	}
	return path
}

func TestParse_ValidPaymentMethods(t *testing.T) {
	data, err := os.ReadFile("../../testdata/specs/payment-methods.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	ps, err := Parse(data, testSchemaPath(t))
	if err != nil {
		t.Fatalf("Parse() returned error: %v", err)
	}

	if ps.Spec.Entity.Name != "PaymentMethod" {
		t.Errorf("entity name: got %q, want %q", ps.Spec.Entity.Name, "PaymentMethod")
	}
	if ps.TableName != "payment_methods" {
		t.Errorf("table name: got %q, want %q", ps.TableName, "payment_methods")
	}
	if ps.RoutePrefix != "/api/v1/payment-methods" {
		t.Errorf("route prefix: got %q, want %q", ps.RoutePrefix, "/api/v1/payment-methods")
	}
	if ps.ComponentDir != "src/components/payment-methods" {
		t.Errorf("component dir: got %q, want %q", ps.ComponentDir, "src/components/payment-methods")
	}
}

func TestParse_MinimalSpec(t *testing.T) {
	data, err := os.ReadFile("../../testdata/specs/minimal.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	ps, err := Parse(data, testSchemaPath(t))
	if err != nil {
		t.Fatalf("Parse() returned error: %v", err)
	}

	if ps.Spec.Entity.Name != "Widget" {
		t.Errorf("entity name: got %q, want %q", ps.Spec.Entity.Name, "Widget")
	}
	if ps.TableName != "widgets" {
		t.Errorf("table name: got %q, want %q", ps.TableName, "widgets")
	}
}

func TestParse_InvalidMissingEntity(t *testing.T) {
	data, err := os.ReadFile("../../testdata/specs/invalid-missing-entity.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	_, err = Parse(data, testSchemaPath(t))
	if err == nil {
		t.Fatal("Parse() expected error for missing entity, got nil")
	}
}

func TestParse_InvalidBadPattern(t *testing.T) {
	data := []byte(`{
		"product": "payments-dashboard",
		"pattern": "invalid_pattern",
		"entity": {"name": "Foo", "fields": [{"name": "bar", "kind": "string"}]}
	}`)

	_, err := Parse(data, testSchemaPath(t))
	if err == nil {
		t.Fatal("Parse() expected error for bad pattern, got nil")
	}
}

func TestFieldNormalization(t *testing.T) {
	tests := []struct {
		kind    string
		wantDB  string
	}{
		{"string", "TEXT"},
		{"enum", "TEXT"},
		{"boolean", "BOOLEAN"},
		{"integer", "INTEGER"},
		{"float", "NUMERIC"},
		{"date", "TIMESTAMPTZ"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			got := toDBType(tt.kind)
			if got != tt.wantDB {
				t.Errorf("toDBType(%q) = %q, want %q", tt.kind, got, tt.wantDB)
			}
		})
	}
}

func TestToTableName(t *testing.T) {
	tests := []struct {
		entity string
		want   string
	}{
		{"PaymentMethod", "payment_methods"},
		{"Widget", "widgets"},
		{"ClaimsRecord", "claims_records"},
		{"Invoice", "invoices"},
		{"Category", "categories"},
		{"Status", "statuses"},
	}

	for _, tt := range tests {
		t.Run(tt.entity, func(t *testing.T) {
			got := toTableName(tt.entity)
			if got != tt.want {
				t.Errorf("toTableName(%q) = %q, want %q", tt.entity, got, tt.want)
			}
		})
	}
}

func TestToRoutePrefix(t *testing.T) {
	tests := []struct {
		entity string
		want   string
	}{
		{"PaymentMethod", "/api/v1/payment-methods"},
		{"Widget", "/api/v1/widgets"},
		{"Invoice", "/api/v1/invoices"},
	}

	for _, tt := range tests {
		t.Run(tt.entity, func(t *testing.T) {
			got := toRoutePrefix(tt.entity)
			if got != tt.want {
				t.Errorf("toRoutePrefix(%q) = %q, want %q", tt.entity, got, tt.want)
			}
		})
	}
}
