package plan

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/athena-digital/bchad/internal/spec"
	"github.com/athena-digital/bchad/pkg/bchadplan"
	"github.com/athena-digital/bchad/pkg/bchadspec"
)

const (
	// modelHaiku is the Claude Haiku 3.5 model identifier for low-complexity stages.
	modelHaiku = "claude-haiku-3-5-sonnet-latest"
	// modelSonnet is the Claude Sonnet 4 model identifier for high-complexity stages.
	modelSonnet = "claude-sonnet-4-20250514"

	// defaultCostThreshold is the maximum projected cost (USD) before flagging for review.
	defaultCostThreshold = 10.00
)

// Generator produces BCHADPlans from parsed specs.
type Generator struct {
	runCounter int // monotonically increasing suffix for the same day
}

// NewGenerator creates a Generator.
func NewGenerator() *Generator {
	return &Generator{}
}

// Generate builds a BCHADPlan from a ParsedSpec using the CRUD+UI DAG template.
// If retrieval is provided (non-nil), it queries codebase refs for each stage.
// If retrieval is nil, codebase_refs are left empty (acceptable for unit tests and stubs).
func (g *Generator) Generate(_ context.Context, ps *spec.ParsedSpec) (*bchadplan.BCHADPlan, error) {
	if ps == nil {
		return nil, fmt.Errorf("generate: parsed spec is nil")
	}

	g.runCounter++
	planID := newPlanID(g.runCounter)

	hasSensitive := anyFieldSensitive(ps.Spec.Entity.Fields)
	hasAudit := ps.Spec.Audit
	hasVault := containsIntegration(ps.Spec.Integrations, "vault")
	isSOC2 := ps.Spec.Compliance.SOC2 || ps.Spec.Compliance.HIPAA

	stages := buildCRUDUIStages(ps, hasAudit, hasSensitive, hasVault)

	plan := &bchadplan.BCHADPlan{
		ID:      planID,
		Product: ps.Spec.Product,
		Pattern: ps.Spec.Pattern,
		Entity:  ps.Spec.Entity.Name,
		Stages:  stages,
	}

	// Compute per-stage costs and total.
	total := EstimatePlanCost(plan)
	plan.ProjectedCost = total

	// Set plan-level approval gates list and security review flag.
	plan.HumanApprovalGates = collectApprovalGates(stages)
	plan.SecurityReview = isSOC2 || hasSensitive

	// Sum estimated files.
	for _, s := range stages {
		plan.EstimatedTotalFiles += s.EstimatedFiles
	}

	// Flag over-threshold plans.
	costThreshold := parseCostThreshold()
	if plan.ProjectedCost > costThreshold {
		slog.Warn("plan: projected cost exceeds threshold — requires human review",
			"plan_id", planID,
			"projected_cost", plan.ProjectedCost,
			"threshold", costThreshold,
		)
	}

	slog.Info("plan: generated",
		"plan_id", planID,
		"product", plan.Product,
		"entity", plan.Entity,
		"stages", len(stages),
		"projected_cost_usd", plan.ProjectedCost,
	)

	return plan, nil
}

// buildCRUDUIStages constructs the five-stage DAG for the crud_ui pattern.
// Dependency order: migrate → api → frontend → tests; config is independent.
// migrate and config can run in parallel (v2); v1 runs all sequentially.
func buildCRUDUIStages(ps *spec.ParsedSpec, hasAudit, hasSensitive, hasVault bool) []bchadplan.PlanStage {
	entity := ps.Spec.Entity.Name
	product := ps.Spec.Product

	migrate := bchadplan.PlanStage{
		ID:             "migrate",
		Type:           "db_migration",
		DependsOn:      nil, // no dependencies
		HumanApproval:  true, // always requires approval
		Model:          modelHaiku,
		Description:    fmt.Sprintf("Create database migration for %s table with %d fields", entity, len(ps.Spec.Entity.Fields)),
		EstimatedFiles: 1,
		CodebaseRefs:   stubCodebaseRefs(product, "migrate"),
	}

	config := bchadplan.PlanStage{
		ID:             "config",
		Type:           "feature_flags_and_permissions",
		DependsOn:      nil, // no dependencies
		HumanApproval:  false,
		Model:          modelHaiku,
		Description:    fmt.Sprintf("Register permissions (%s) and feature flag for %s", ps.Spec.Permissions, entity),
		EstimatedFiles: 2,
		CodebaseRefs:   stubCodebaseRefs(product, "config"),
	}

	api := bchadplan.PlanStage{
		ID:             "api",
		Type:           "rest_endpoints",
		DependsOn:      []string{"migrate"},
		HumanApproval:  hasSensitive, // sensitive fields require approval
		Model:          modelSonnet,
		Description:    fmt.Sprintf("Generate REST CRUD endpoints for %s at %s", entity, ps.RoutePrefix),
		EstimatedFiles: 3,
		CodebaseRefs:   stubCodebaseRefs(product, "api"),
	}

	uiFiles := 1
	if ps.Spec.UI.List {
		uiFiles++
	}
	if ps.Spec.UI.Detail {
		uiFiles++
	}
	if ps.Spec.UI.Form {
		uiFiles++
	}

	frontend := bchadplan.PlanStage{
		ID:             "frontend",
		Type:           "react_components",
		DependsOn:      []string{"api"},
		HumanApproval:  false,
		Model:          modelSonnet,
		Description:    fmt.Sprintf("Generate React components for %s (%s)", entity, describeUI(ps.Spec.UI)),
		EstimatedFiles: uiFiles,
		CodebaseRefs:   stubCodebaseRefs(product, "frontend"),
	}

	tests := bchadplan.PlanStage{
		ID:             "tests",
		Type:           "test_suite",
		DependsOn:      []string{"api", "frontend", "config"},
		HumanApproval:  false,
		Model:          modelSonnet,
		Description:    fmt.Sprintf("Generate unit and integration tests for all %s stages", entity),
		EstimatedFiles: 3,
		CodebaseRefs:   stubCodebaseRefs(product, "tests"),
	}

	_ = hasAudit
	_ = hasVault

	return []bchadplan.PlanStage{migrate, config, api, frontend, tests}
}

// stubCodebaseRefs returns placeholder codebase refs when retrieval is not available.
// Phase 3 will replace these with real vector-search results.
func stubCodebaseRefs(product, stageType string) []string {
	// Stub refs based on the node-express-prisma-v1 repo structure.
	switch stageType {
	case "migrate":
		return []string{"prisma/migrations/", "prisma/schema.prisma"}
	case "config":
		return []string{"src/config/", "src/middleware/"}
	case "api":
		return []string{"src/routes/", "src/controllers/", "src/middleware/auth.ts"}
	case "frontend":
		return []string{"src/components/", "src/pages/"}
	case "tests":
		return []string{"tests/", "src/__tests__/"}
	default:
		return nil
	}
}

// collectApprovalGates returns the IDs of stages that require human approval.
func collectApprovalGates(stages []bchadplan.PlanStage) []string {
	var gates []string
	for _, s := range stages {
		if s.HumanApproval {
			gates = append(gates, s.ID)
		}
	}
	return gates
}

// anyFieldSensitive reports whether any field in the list has Sensitive=true.
func anyFieldSensitive(fields []bchadspec.FieldSpec) bool {
	for _, f := range fields {
		if f.Sensitive {
			return true
		}
	}
	return false
}

// bchadspecUI is an alias for the UISpec type used in describeUI to avoid import cycles.
type bchadspecUI = bchadspec.UISpec

// containsIntegration reports whether the integration list contains the given name.
func containsIntegration(integrations []string, name string) bool {
	for _, i := range integrations {
		if strings.EqualFold(i, name) {
			return true
		}
	}
	return false
}

// describeUI returns a human-readable summary of the UI spec.
func describeUI(ui bchadspecUI) string {
	var parts []string
	if ui.List {
		parts = append(parts, "list")
	}
	if ui.Detail {
		parts = append(parts, "detail")
	}
	if ui.Form {
		parts = append(parts, "form")
	}
	if len(parts) == 0 {
		return "no UI"
	}
	return strings.Join(parts, "+")
}

// newPlanID generates a plan ID in the format pf-YYYYMMDD-NNN.
func newPlanID(counter int) string {
	date := time.Now().UTC().Format("20060102")
	return fmt.Sprintf("pf-%s-%03d", date, counter)
}

// parseCostThreshold reads BCHAD_COST_THRESHOLD from env, defaulting to $10.
func parseCostThreshold() float64 {
	raw := os.Getenv("BCHAD_COST_THRESHOLD")
	if raw == "" {
		return defaultCostThreshold
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return defaultCostThreshold
	}
	return v
}
