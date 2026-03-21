package spec

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/athena-digital/bchad/internal/gateway"
	"github.com/athena-digital/bchad/pkg/bchadspec"
)

const (
	// nlTranslationModel is the Claude model used for NL-to-BCHADSpec translation.
	nlTranslationModel = "claude-sonnet-4-20250514"
	// nlMaxTokens is the maximum output tokens for the NL translation call.
	nlMaxTokens = 4096
)

// nlSystemPrompt is the fixed system prompt for NL-to-BCHADSpec translation.
const nlSystemPrompt = `You are a spec parser for BCHAD. Given a product feature brief in plain English, extract a BCHADSpec JSON.

Rules:
- Use only the fields defined in the schema below.
- Mark any field whose intent you cannot determine unambiguously as "needs_clarification": true with a "reason" explaining what is unclear.
- Do not invent integrations, permissions, or compliance requirements — only include what is clearly stated.
- entity.fields[].name must be snake_case.
- entity.name must be PascalCase.
- pattern must be one of: "crud_ui", "integration", "workflow", "analytics". Default to "crud_ui" for management pages.
- Output ONLY valid JSON matching the BCHADSpec schema. No explanation, no markdown fences.

BCHADSpec schema:
{
  "product": "string (required)",
  "pattern": "crud_ui | integration | workflow | analytics (required)",
  "entity": {
    "name": "PascalCase string (required)",
    "fields": [
      {
        "name": "snake_case string (required)",
        "kind": "string | enum | boolean | integer | float | date (required)",
        "values": ["array of strings, required when kind=enum"],
        "sensitive": "boolean (default false)",
        "required": "boolean (default true)",
        "needs_clarification": "boolean (default false)",
        "reason": "string, why clarification is needed"
      }
    ]
  },
  "permissions": "scope:action string (optional)",
  "audit": "boolean (optional)",
  "integrations": ["array of strings (optional)"],
  "ui": { "list": "bool", "detail": "bool", "form": "bool" },
  "compliance": { "soc2": "bool", "hipaa": "bool" }
}`

// NLTranslator converts natural language feature briefs to BCHADSpec using Claude.
type NLTranslator struct {
	client    *gateway.Client
	validator *Validator
}

// NewNLTranslator creates a NLTranslator.
// schemaURL and schemaJSON are used to validate the LLM output.
func NewNLTranslator(client *gateway.Client, validator *Validator) *NLTranslator {
	return &NLTranslator{
		client:    client,
		validator: validator,
	}
}

// NLTranslationResult is the output of NL-to-BCHADSpec translation.
type NLTranslationResult struct {
	// Spec is the extracted BCHADSpec. Fields with NeedsClarification=true
	// require engineer resolution before plan generation.
	Spec bchadspec.BCHADSpec
	// RawJSON is the raw JSON returned by the LLM, for debugging.
	RawJSON string
	// ClarificationFields lists field names that need engineer input.
	ClarificationFields []string
}

// Translate converts a natural language brief to a draft BCHADSpec.
// The productID is injected into the system prompt context so the LLM uses
// the correct product identifier.
func (t *NLTranslator) Translate(ctx context.Context, brief string, productID string) (*NLTranslationResult, error) {
	systemPrompt := nlSystemPrompt
	if productID != "" {
		systemPrompt += fmt.Sprintf("\n\nTarget product ID: %q", productID)
	}

	req := gateway.GenerateRequest{
		Model:     nlTranslationModel,
		MaxTokens: nlMaxTokens,
		System:    systemPrompt,
		Messages: []gateway.Message{
			{Role: "user", Content: fmt.Sprintf("Feature brief:\n\n%s", brief)},
		},
	}

	resp, err := t.client.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("nl translation LLM call: %w", err)
	}

	rawText := resp.Text()

	// Strip markdown fences if the LLM ignored the instruction.
	rawJSON := stripMarkdownFences(rawText)

	// Validate the JSON before unmarshalling.
	if err := t.validator.Validate([]byte(rawJSON)); err != nil {
		return nil, fmt.Errorf("LLM returned invalid BCHADSpec JSON: %w\n\nRaw output:\n%s", err, rawJSON)
	}

	var spec bchadspec.BCHADSpec
	if err := json.Unmarshal([]byte(rawJSON), &spec); err != nil {
		return nil, fmt.Errorf("unmarshal LLM BCHADSpec: %w", err)
	}

	// Override product ID if the LLM didn't pick it up correctly.
	if productID != "" && spec.Product == "" {
		spec.Product = productID
	}

	// Collect field names that need clarification.
	var needsClarification []string
	for _, f := range spec.Entity.Fields {
		if f.NeedsClarification {
			needsClarification = append(needsClarification, f.Name)
		}
	}

	return &NLTranslationResult{
		Spec:                spec,
		RawJSON:             rawJSON,
		ClarificationFields: needsClarification,
	}, nil
}

// stripMarkdownFences removes ```json...``` or ```...``` wrappers if present.
func stripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		// Drop first line (```json or ```) and last line (```)
		if len(lines) >= 3 {
			inner := lines[1 : len(lines)-1]
			s = strings.Join(inner, "\n")
		}
	}
	return strings.TrimSpace(s)
}
