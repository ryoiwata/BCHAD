package spec

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/athena-digital/bchad/pkg/bchadspec"
)

// ParsedSpec is the fully resolved BCHADSpec with codebase conventions applied.
// Downstream components (plan generator, prompt assembler) consume this struct.
type ParsedSpec struct {
	Spec         bchadspec.BCHADSpec
	TableName    string            // snake_case plural, e.g. "payment_methods"
	RoutePrefix  string            // REST route prefix, e.g. "/api/v1/payment-methods"
	ComponentDir string            // frontend component dir, e.g. "src/components/payment-methods"
	FieldTypes   map[string]string // field name → PostgreSQL type, e.g. "label" → "TEXT"
}

// Parse validates a BCHADSpec JSON and resolves codebase conventions.
// schemaPath is the path to schemas/bchadspec.v1.json (absolute or relative to caller).
func Parse(data []byte, schemaPath string) (*ParsedSpec, error) {
	absSchema, err := filepath.Abs(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("resolve schema path: %w", err)
	}
	v, err := NewValidator(absSchema)
	if err != nil {
		return nil, fmt.Errorf("create validator: %w", err)
	}
	return ParseWithValidator(data, v)
}

// ParseWithValidator validates a BCHADSpec JSON using a pre-compiled validator
// and resolves codebase conventions. Use this when the schema is embedded.
func ParseWithValidator(data []byte, v *Validator) (*ParsedSpec, error) {
	if err := v.Validate(data); err != nil {
		return nil, err
	}
	var spec bchadspec.BCHADSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("unmarshal spec: %w", err)
	}
	return resolveConventions(&spec), nil
}

// resolveConventions applies naming and structural convention resolution to a validated BCHADSpec.
func resolveConventions(spec *bchadspec.BCHADSpec) *ParsedSpec {
	tableName := toTableName(spec.Entity.Name)
	routePrefix := toRoutePrefix(spec.Entity.Name)
	componentDir := toComponentDir(spec.Entity.Name)

	fieldTypes := make(map[string]string, len(spec.Entity.Fields))
	for _, f := range spec.Entity.Fields {
		fieldTypes[f.Name] = toDBType(f.Kind)
	}

	return &ParsedSpec{
		Spec:         *spec,
		TableName:    tableName,
		RoutePrefix:  routePrefix,
		ComponentDir: componentDir,
		FieldTypes:   fieldTypes,
	}
}

// toTableName converts a PascalCase entity name to a snake_case plural table name.
// "PaymentMethod" → "payment_methods", "Widget" → "widgets".
func toTableName(entityName string) string {
	return pluralizeSnake(toSnakeCase(entityName))
}

// toRoutePrefix converts a PascalCase entity name to a REST route prefix.
// "PaymentMethod" → "/api/v1/payment-methods".
func toRoutePrefix(entityName string) string {
	return "/api/v1/" + pluralizeKebab(toKebabCase(entityName))
}

// toComponentDir converts a PascalCase entity name to a frontend component directory.
// "PaymentMethod" → "src/components/payment-methods".
func toComponentDir(entityName string) string {
	return "src/components/" + pluralizeKebab(toKebabCase(entityName))
}

// toDBType maps a BCHADSpec field kind to a PostgreSQL column type.
func toDBType(kind string) string {
	switch kind {
	case "string":
		return "TEXT"
	case "enum":
		return "TEXT"
	case "boolean":
		return "BOOLEAN"
	case "integer":
		return "INTEGER"
	case "float":
		return "NUMERIC"
	case "date":
		return "TIMESTAMPTZ"
	default:
		return "TEXT"
	}
}

// toSnakeCase converts PascalCase to snake_case.
// "PaymentMethod" → "payment_method"
func toSnakeCase(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			prev := runes[i-1]
			if unicode.IsLower(prev) || (i+1 < len(runes) && unicode.IsLower(runes[i+1])) {
				b.WriteRune('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// toKebabCase converts PascalCase to kebab-case.
// "PaymentMethod" → "payment-method"
func toKebabCase(s string) string {
	return strings.ReplaceAll(toSnakeCase(s), "_", "-")
}

// pluralizeSnake pluralizes the last word of a snake_case identifier.
func pluralizeSnake(s string) string {
	if s == "" {
		return s
	}
	idx := strings.LastIndex(s, "_")
	if idx < 0 {
		return pluralWord(s)
	}
	return s[:idx+1] + pluralWord(s[idx+1:])
}

// pluralizeKebab pluralizes the last word of a kebab-case identifier.
func pluralizeKebab(s string) string {
	if s == "" {
		return s
	}
	idx := strings.LastIndex(s, "-")
	if idx < 0 {
		return pluralWord(s)
	}
	return s[:idx+1] + pluralWord(s[idx+1:])
}

// pluralWord applies simple English plural rules to a single lowercase word.
func pluralWord(word string) string {
	switch {
	case strings.HasSuffix(word, "ss") ||
		strings.HasSuffix(word, "sh") ||
		strings.HasSuffix(word, "ch") ||
		strings.HasSuffix(word, "x") ||
		strings.HasSuffix(word, "z"):
		return word + "es"
	case strings.HasSuffix(word, "us"):
		// e.g. status → statuses, bonus → bonuses
		return word + "es"
	case strings.HasSuffix(word, "ay") ||
		strings.HasSuffix(word, "ey") ||
		strings.HasSuffix(word, "oy") ||
		strings.HasSuffix(word, "uy"):
		return word + "s"
	case strings.HasSuffix(word, "y"):
		return word[:len(word)-1] + "ies"
	case strings.HasSuffix(word, "s"):
		return word // already plural (e.g. series, address)
	default:
		return word + "s"
	}
}
