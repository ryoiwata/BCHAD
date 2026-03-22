package spec

import (
	"context"
	"os"
	"testing"

	"github.com/athena-digital/bchad/internal/gateway"
)

func TestStripMarkdownFences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no fences",
			input: `{"product": "test"}`,
			want:  `{"product": "test"}`,
		},
		{
			name:  "json fence",
			input: "```json\n{\"product\": \"test\"}\n```",
			want:  `{"product": "test"}`,
		},
		{
			name:  "plain fence",
			input: "```\n{\"product\": \"test\"}\n```",
			want:  `{"product": "test"}`,
		},
		{
			name:  "surrounding whitespace",
			input: "  ```json\n{\"k\": \"v\"}\n```  ",
			want:  `{"k": "v"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripMarkdownFences(tt.input)
			if got != tt.want {
				t.Errorf("stripMarkdownFences() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNLTranslator_ValidResponse(t *testing.T) {
	v, err := NewValidator(testSchemaPath(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	// Fixture: a valid BCHADSpec JSON that the "LLM" would return.
	fixtureJSON := `{
		"product": "node-express-prisma-v1",
		"pattern": "crud_ui",
		"entity": {
			"name": "PaymentMethod",
			"fields": [
				{"name": "type", "kind": "enum", "values": ["credit_card", "ach"], "required": true},
				{"name": "label", "kind": "string", "required": true}
			]
		},
		"permissions": "payment_methods:manage",
		"audit": true,
		"integrations": ["vault"],
		"ui": {"list": true, "detail": true, "form": true}
	}`

	// The translator validates the LLM output; test that a valid fixture passes.
	if err := v.Validate([]byte(fixtureJSON)); err != nil {
		t.Fatalf("fixture should be valid BCHADSpec: %v", err)
	}

	// Test stripMarkdownFences has no effect on plain JSON.
	stripped := stripMarkdownFences(fixtureJSON)
	if err := v.Validate([]byte(stripped)); err != nil {
		t.Errorf("stripped fixture is invalid: %v", err)
	}
}

func TestNLTranslator_NeedsClarificationAllowed(t *testing.T) {
	v, err := NewValidator(testSchemaPath(t))
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	// needs_clarification=true on a field is valid BCHADSpec.
	data := []byte(`{
		"product": "node-express-prisma-v1",
		"pattern": "crud_ui",
		"entity": {
			"name": "Widget",
			"fields": [
				{"name": "name", "kind": "string"},
				{"name": "priority", "kind": "string", "needs_clarification": true, "reason": "unclear if enum or free text"}
			]
		}
	}`)

	if err := v.Validate(data); err != nil {
		t.Errorf("spec with needs_clarification should be valid: %v", err)
	}
}

// TestNLTranslator_Integration calls the real Anthropic API to translate a NL brief.
// Skipped unless ANTHROPIC_API_KEY is set.
func TestNLTranslator_Integration(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set — skipping integration test")
	}

	briefBytes, err := os.ReadFile("../../examples/payment-methods-nl.txt")
	if err != nil {
		t.Fatalf("read NL brief: %v", err)
	}

	schemaBytes, err := os.ReadFile(testSchemaPath(t))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}

	v, err := NewValidatorFromBytes("bchadspec.v1.json", schemaBytes)
	if err != nil {
		t.Fatalf("NewValidatorFromBytes: %v", err)
	}

	client := gateway.NewClient(apiKey)
	translator := NewNLTranslator(client, v)

	result, err := translator.Translate(context.Background(), string(briefBytes), "node-express-prisma-v1")
	if err != nil {
		t.Fatalf("Translate() error: %v", err)
	}

	if result.Spec.Product == "" {
		t.Error("expected non-empty product in translated spec")
	}
	if result.Spec.Pattern == "" {
		t.Error("expected non-empty pattern in translated spec")
	}
	if result.Spec.Entity.Name == "" {
		t.Error("expected non-empty entity name in translated spec")
	}
	if len(result.Spec.Entity.Fields) == 0 {
		t.Error("expected at least one field in translated spec")
	}

	t.Logf("translated: product=%s pattern=%s entity=%s fields=%d clarifications=%v",
		result.Spec.Product, result.Spec.Pattern, result.Spec.Entity.Name,
		len(result.Spec.Entity.Fields), result.ClarificationFields,
	)
}
