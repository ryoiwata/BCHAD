package spec

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Validator validates raw JSON against the BCHADSpec JSON Schema (Draft 2020-12).
type Validator struct {
	schema *jsonschema.Schema
}

// NewValidator compiles the BCHADSpec schema from an absolute file path.
func NewValidator(schemaPath string) (*Validator, error) {
	c := jsonschema.NewCompiler()
	schema, err := c.Compile("file://" + schemaPath)
	if err != nil {
		return nil, fmt.Errorf("compile bchadspec schema from %s: %w", schemaPath, err)
	}
	return &Validator{schema: schema}, nil
}

// NewValidatorFromBytes compiles the BCHADSpec schema from raw JSON bytes.
// schemaURL is used as the schema identifier (e.g. "bchadspec.v1.json").
func NewValidatorFromBytes(schemaURL string, schemaJSON []byte) (*Validator, error) {
	// AddResource requires a parsed JSON value (any), not raw bytes.
	var doc any
	if err := json.Unmarshal(schemaJSON, &doc); err != nil {
		return nil, fmt.Errorf("parse schema JSON: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaURL, doc); err != nil {
		return nil, fmt.Errorf("add schema resource %s: %w", schemaURL, err)
	}
	schema, err := c.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("compile schema from bytes: %w", err)
	}
	return &Validator{schema: schema}, nil
}

// Validate validates the raw JSON bytes against the BCHADSpec schema.
// Returns all validation errors (not just the first) formatted for human consumption.
func (v *Validator) Validate(data []byte) error {
	var instance any
	if err := json.Unmarshal(data, &instance); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	err := v.schema.Validate(instance)
	if err == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if errors.As(err, &ve) {
		// ve.Error() formats the full validation error tree — all causes included.
		return fmt.Errorf("BCHADSpec validation failed:\n%s", ve.Error())
	}
	return err
}
