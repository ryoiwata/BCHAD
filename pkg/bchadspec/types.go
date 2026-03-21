// Package bchadspec defines the BCHADSpec type and its sub-types.
// BCHADSpec is the normalized feature specification — the primary input to the BCHAD
// software factory. It is validated against schemas/bchadspec.v1.json at every component boundary.
package bchadspec

// BCHADSpec is the normalized representation of a feature specification.
// It is produced by the spec parser from a JSON input or NL-to-spec translation,
// and consumed by the plan generator to build a BCHADPlan DAG.
type BCHADSpec struct {
	// Product is the identifier of the target product (e.g. "payments-dashboard").
	Product string `json:"product"`

	// Pattern is the generation pattern that determines the DAG template.
	// Valid values: "crud_ui", "integration", "workflow", "analytics".
	Pattern string `json:"pattern"`

	// Entity describes the domain entity to be generated.
	Entity EntitySpec `json:"entity"`

	// Permissions is the permission scope required to access this feature
	// (e.g. "payment_methods:manage").
	Permissions string `json:"permissions,omitempty"`

	// Audit indicates that every state-changing operation must include an audit log call.
	Audit bool `json:"audit,omitempty"`

	// Integrations lists the integrations to activate (e.g. ["vault", "launchdarkly"]).
	Integrations []string `json:"integrations,omitempty"`

	// UI specifies which UI views to generate.
	UI UISpec `json:"ui,omitempty"`

	// Compliance specifies compliance requirements that affect code generation.
	// Auto-detected from the product profile.
	Compliance ComplianceFlags `json:"compliance,omitempty"`
}

// EntitySpec describes the domain entity to be generated.
type EntitySpec struct {
	// Name is the PascalCase entity name (e.g. "PaymentMethod", "ClaimsRecord").
	Name string `json:"name"`

	// Fields is the list of fields on the entity. At least one field is required.
	Fields []FieldSpec `json:"fields"`
}

// FieldSpec describes a single field on the entity.
type FieldSpec struct {
	// Name is the field name in snake_case.
	Name string `json:"name"`

	// Kind is the scalar type of the field.
	// Valid values: "string", "enum", "boolean", "integer", "float", "date".
	Kind string `json:"kind"`

	// Values lists allowed enum values. Required when Kind is "enum".
	Values []string `json:"values,omitempty"`

	// Sensitive indicates the field must use the Vault integration pattern.
	Sensitive bool `json:"sensitive,omitempty"`

	// Required indicates the field is non-nullable in the database and required in the form.
	Required bool `json:"required,omitempty"`

	// NeedsClarification indicates the NL translator could not determine this field's
	// intent unambiguously. The engineer must resolve it before plan generation proceeds.
	NeedsClarification bool `json:"needs_clarification,omitempty"`

	// Reason explains why clarification is needed. Only present when NeedsClarification is true.
	Reason string `json:"reason,omitempty"`
}

// UISpec specifies which UI views to generate for this entity.
type UISpec struct {
	// List indicates a paginated list view should be generated.
	List bool `json:"list,omitempty"`

	// Detail indicates a detail/read-only view should be generated.
	Detail bool `json:"detail,omitempty"`

	// Form indicates a create/edit form should be generated.
	Form bool `json:"form,omitempty"`
}

// ComplianceFlags specifies compliance requirements that affect code generation.
// These are typically auto-detected from the product profile rather than specified manually.
type ComplianceFlags struct {
	// SOC2 indicates the product is under SOC 2 compliance.
	// Enables audit logging and data handling constraints.
	SOC2 bool `json:"soc2,omitempty"`

	// HIPAA indicates the product is under HIPAA compliance.
	// Enables PHI handling patterns and stricter access controls.
	HIPAA bool `json:"hipaa,omitempty"`
}
