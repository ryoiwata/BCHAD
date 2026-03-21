// Package workflows contains Temporal workflow and activity definitions.
//
// PipelineWorkflow orchestrates a full spec-to-PR run using Temporal's durable
// execution model. Each stage is an activity with category-specific retry
// policies. Approval gates use Temporal signals. Status queries use Temporal
// query handlers. Tier 2 integration gates run as child workflows.
package workflows
