package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"

	"github.com/athena-digital/bchad/internal/gateway"
	"github.com/athena-digital/bchad/internal/plan"
	"github.com/athena-digital/bchad/internal/spec"
	"github.com/athena-digital/bchad/pkg/bchadplan"
	"github.com/athena-digital/bchad/workflows"
)

// stdinScanner is a single shared scanner for os.Stdin. Creating a new
// bufio.Scanner per prompt call causes the scanner's internal buffer to
// consume ahead-of-line bytes from the pipe, losing subsequent input.
var stdinScanner = bufio.NewScanner(os.Stdin)

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().StringP("spec", "s", "", "path to BCHADSpec JSON file or natural language brief (required)")
	runCmd.Flags().StringP("product", "p", "", "product ID override (uses spec value by default)")
	runCmd.Flags().Bool("no-confirm", false, "skip interactive plan confirmation (for CI use)")
	_ = runCmd.MarkFlagRequired("spec")
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Submit a feature spec and execute the generation pipeline",
	Long: `Parses a BCHADSpec (JSON or natural language), generates a plan with cost
estimates, asks for confirmation, then starts the Temporal pipeline workflow.

Examples:
  bchad run --spec examples/payment-methods.json
  bchad run --spec "Add a payment methods page with CRUD operations"`,
	RunE: runPipeline,
}

func runPipeline(cmd *cobra.Command, _ []string) error {
	specArg, _ := cmd.Flags().GetString("spec")
	productOverride, _ := cmd.Flags().GetString("product")
	noConfirm, _ := cmd.Flags().GetBool("no-confirm")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	// --- Resolve engineer identity ---
	engineerID := resolveEngineerID(ctx)

	// --- Load and parse the spec ---
	ps, err := loadAndParseSpec(ctx, specArg, productOverride)
	if err != nil {
		return fmt.Errorf("spec: %w", err)
	}

	// Check for fields needing clarification.
	var needsClarification []string
	for _, f := range ps.Spec.Entity.Fields {
		if f.NeedsClarification {
			needsClarification = append(needsClarification, fmt.Sprintf("  - %s: %s", f.Name, f.Reason))
		}
	}
	if len(needsClarification) > 0 {
		fmt.Println("\n⚠  These fields need clarification before proceeding:")
		for _, c := range needsClarification {
			fmt.Println(c)
		}
		fmt.Println("\nEdit the spec to resolve these fields and re-run.")
		return fmt.Errorf("spec has %d field(s) requiring clarification", len(needsClarification))
	}

	// --- Generate the plan ---
	g := plan.NewGenerator()
	bchadPlan, err := g.Generate(ctx, ps)
	if err != nil {
		return fmt.Errorf("plan generation: %w", err)
	}

	// --- Print the plan ---
	printPlan(bchadPlan)

	// --- Confirm execution ---
	if !noConfirm {
		if !promptYesNo(fmt.Sprintf("\nExecute plan %s? [y/n]: ", bchadPlan.ID)) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// --- Connect to Temporal and start the workflow ---
	temporalHost := os.Getenv("BCHAD_TEMPORAL_HOST")
	if temporalHost == "" {
		temporalHost = "localhost:7233"
	}
	namespace := os.Getenv("BCHAD_TEMPORAL_NAMESPACE")
	if namespace == "" {
		namespace = "bchad"
	}

	c, err := client.Dial(client.Options{
		HostPort:  temporalHost,
		Namespace: namespace,
	})
	if err != nil {
		return fmt.Errorf("connect to Temporal at %s: %w", temporalHost, err)
	}
	defer c.Close()

	runID := fmt.Sprintf("run-%s-%d", bchadPlan.ID, time.Now().UnixMilli())

	wfInput := workflows.PipelineInput{
		RunID:      runID,
		Plan:       *bchadPlan,
		EngineerID: engineerID,
		ProductID:  ps.Spec.Product,
		TrustPhase: "supervised",
	}

	wfOptions := client.StartWorkflowOptions{
		ID:        runID,
		TaskQueue: workflows.PipelineTaskQueue,
	}

	we, err := c.ExecuteWorkflow(ctx, wfOptions, workflows.PipelineWorkflow, wfInput)
	if err != nil {
		return fmt.Errorf("start pipeline workflow: %w", err)
	}

	fmt.Printf("\nPipeline started: run_id=%s workflow_id=%s\n", runID, we.GetID())
	fmt.Printf("Track status: bchad status %s\n\n", runID)

	// --- Stream stage status updates ---
	fmt.Println("Waiting for pipeline to complete...")
	fmt.Println("(Use Ctrl+C to detach; pipeline continues in the background)")
	fmt.Println()

	// Poll for approval gates and completion.
	if err := monitorPipeline(ctx, c, we, runID, bchadPlan.HumanApprovalGates); err != nil {
		return fmt.Errorf("pipeline: %w", err)
	}

	return nil
}

// loadAndParseSpec loads a spec from a file path or NL string and returns a ParsedSpec.
func loadAndParseSpec(ctx context.Context, specArg, productOverride string) (*spec.ParsedSpec, error) {
	schemaPath, err := resolveSchemaPath()
	if err != nil {
		return nil, fmt.Errorf("resolve schema path: %w", err)
	}

	// If it looks like a file path, read it.
	if _, statErr := os.Stat(specArg); statErr == nil {
		data, err := os.ReadFile(specArg)
		if err != nil {
			return nil, fmt.Errorf("read spec file %s: %w", specArg, err)
		}
		return parseSpecData(data, schemaPath, productOverride)
	}

	// Try to parse as JSON directly.
	if json.Valid([]byte(specArg)) {
		return parseSpecData([]byte(specArg), schemaPath, productOverride)
	}

	// Treat as natural language brief.
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set — required for natural language spec translation")
	}

	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("read schema: %w", err)
	}

	v, err := spec.NewValidatorFromBytes("bchadspec.v1.json", schemaBytes)
	if err != nil {
		return nil, fmt.Errorf("create validator: %w", err)
	}

	gw := gateway.NewClient(apiKey)
	translator := spec.NewNLTranslator(gw, v)

	productID := productOverride
	fmt.Printf("Translating natural language brief to BCHADSpec...\n")
	result, err := translator.Translate(ctx, specArg, productID)
	if err != nil {
		return nil, fmt.Errorf("NL translation: %w", err)
	}

	// Show the translated spec for confirmation.
	translated, _ := json.MarshalIndent(result.Spec, "", "  ")
	fmt.Printf("\nTranslated spec:\n%s\n", string(translated))

	if len(result.ClarificationFields) > 0 {
		fmt.Printf("\nFields needing clarification: %v\n", result.ClarificationFields)
	}

	if !promptYesNo("Use this spec? [y/n]: ") {
		return nil, fmt.Errorf("spec rejected by engineer")
	}

	specData, _ := json.Marshal(result.Spec)
	return spec.ParseWithValidator(specData, v)
}

// parseSpecData parses raw JSON spec data and applies optional product override.
func parseSpecData(data []byte, schemaPath, productOverride string) (*spec.ParsedSpec, error) {
	ps, err := spec.Parse(data, schemaPath)
	if err != nil {
		return nil, err
	}
	if productOverride != "" {
		ps.Spec.Product = productOverride
	}
	return ps, nil
}

// printPlan prints the BCHADPlan in a human-readable format.
func printPlan(p *bchadplan.BCHADPlan) {
	fmt.Printf("\n=== BCHAD Plan: %s ===\n", p.ID)
	fmt.Printf("Product:    %s\n", p.Product)
	fmt.Printf("Pattern:    %s\n", p.Pattern)
	fmt.Printf("Entity:     %s\n", p.Entity)
	fmt.Printf("Total cost: $%.4f projected\n", p.ProjectedCost)
	if p.SecurityReview {
		fmt.Printf("Security:   review required\n")
	}
	fmt.Println()
	fmt.Printf("%-12s %-28s %-10s %-8s %s\n", "STAGE", "DESCRIPTION", "MODEL", "COST", "APPROVAL")
	fmt.Println(strings.Repeat("-", 80))
	for _, s := range p.Stages {
		model := "haiku"
		if strings.Contains(s.Model, "sonnet") {
			model = "sonnet"
		}
		approval := ""
		if s.HumanApproval {
			approval = "✓ required"
		}
		desc := s.Description
		if len(desc) > 27 {
			desc = desc[:24] + "..."
		}
		fmt.Printf("%-12s %-28s %-10s $%-7.4f %s\n", s.ID, desc, model, s.EstimatedCost, approval)
	}
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("%-12s %-28s %-10s $%-7.4f\n", "TOTAL", "", "", p.ProjectedCost)

	if len(p.HumanApprovalGates) > 0 {
		fmt.Printf("\nApproval gates: %s\n", strings.Join(p.HumanApprovalGates, ", "))
	}
	fmt.Println()
}

// monitorPipeline polls the workflow for status and handles approval gates interactively.
func monitorPipeline(ctx context.Context, c client.Client, we client.WorkflowRun, runID string, approvalGates []string) error {
	pendingApprovals := make(map[string]bool)
	for _, g := range approvalGates {
		pendingApprovals[g] = true
	}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	lastStatus := ""

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			var status workflows.PipelineStatus
			qresp, qerr := c.QueryWorkflow(ctx, we.GetID(), we.GetRunID(), workflows.StatusQueryName)
			if qerr == nil {
				qerr = qresp.Get(&status)
			}
			err := qerr
			if err != nil {
				// Workflow may have completed.
				var result workflows.PipelineOutput
				if getErr := we.Get(ctx, &result); getErr == nil {
					fmt.Printf("\n✓ Pipeline complete!\n")
					if result.PRURL != "" {
						fmt.Printf("  PR: %s\n", result.PRURL)
					}
					fmt.Printf("  Cost: $%.4f\n", result.AccumulatedCost)
					return nil
				}
				continue
			}

			if status.Status != lastStatus {
				fmt.Printf("[%s] status: %s", time.Now().Format("15:04:05"), status.Status)
				if status.RunningStage != "" {
					fmt.Printf(" (stage: %s)", status.RunningStage)
				}
				fmt.Println()
				lastStatus = status.Status
			}

			// Handle approval prompts.
			if status.Status == "awaiting_approval" && status.RunningStage != "" {
				stageID := status.RunningStage
				if pendingApprovals[stageID] {
					fmt.Printf("\n⚠  Stage '%s' requires approval.\n", stageID)
					if promptYesNo(fmt.Sprintf("Approve '%s' stage? [y/n]: ", stageID)) {
						err := c.SignalWorkflow(ctx, we.GetID(), we.GetRunID(), workflows.ApprovalSignalName,
							workflows.ApprovalDecision{
								StageID:  stageID,
								Decision: "approve",
							})
						if err != nil {
							fmt.Printf("Warning: failed to send approval signal: %v\n", err)
						} else {
							fmt.Printf("✓ Approved stage '%s'\n\n", stageID)
							delete(pendingApprovals, stageID)
						}
					} else {
						guidance := promptInput("Rejection reason (optional): ")
						_ = c.SignalWorkflow(ctx, we.GetID(), we.GetRunID(), workflows.ApprovalSignalName,
							workflows.ApprovalDecision{
								StageID:      stageID,
								Decision:     "reject",
								GuidanceNote: guidance,
							})
						fmt.Printf("✗ Rejected stage '%s'\n", stageID)
						return fmt.Errorf("stage %s rejected", stageID)
					}
				}
			}

			if status.Status == "complete" || status.Status == "failed" {
				var result workflows.PipelineOutput
				if err := we.Get(ctx, &result); err == nil {
					if result.Status == "complete" {
						fmt.Printf("\n✓ Pipeline complete!\n")
						if result.PRURL != "" {
							fmt.Printf("  PR: %s\n", result.PRURL)
						}
						fmt.Printf("  Cost: $%.4f\n", result.AccumulatedCost)
					} else {
						fmt.Printf("\n✗ Pipeline failed\n")
					}
				}
				return nil
			}
		}
	}
}

// resolveEngineerID resolves the engineer's GitHub login or falls back to $USER.
func resolveEngineerID(ctx context.Context) string {
	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		user := os.Getenv("USER")
		if user != "" {
			return user
		}
		return "unknown"
	}

	// Call GitHub API to resolve the login.
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "https://api.github.com/user", nil)
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+githubToken)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			defer func() { _ = resp.Body.Close() }()
			var ghUser struct {
				Login string `json:"login"`
			}
			if json.NewDecoder(resp.Body).Decode(&ghUser) == nil && ghUser.Login != "" {
				return ghUser.Login
			}
		}
	}

	// Fallback to $USER.
	user := os.Getenv("USER")
	if user != "" {
		fmt.Fprintf(os.Stderr, "Warning: could not resolve GitHub identity, using $USER=%s\n", user)
		return user
	}
	return "unknown"
}

// resolveSchemaPath finds schemas/bchadspec.v1.json relative to the executable.
func resolveSchemaPath() (string, error) {
	// Try relative to current working directory first.
	candidates := []string{
		"schemas/bchadspec.v1.json",
		"../schemas/bchadspec.v1.json",
		"../../schemas/bchadspec.v1.json",
	}

	exe, err := os.Executable()
	if err == nil {
		// Also try relative to the binary.
		candidates = append(candidates,
			filepath.Join(filepath.Dir(exe), "schemas/bchadspec.v1.json"),
			filepath.Join(filepath.Dir(exe), "../schemas/bchadspec.v1.json"),
		)
	}

	for _, p := range candidates {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
	}

	return "", fmt.Errorf("could not find schemas/bchadspec.v1.json; set BCHAD_SCHEMA_PATH or run from the project root")
}

// promptYesNo asks a yes/no question and returns true for 'y'/'Y'.
func promptYesNo(prompt string) bool {
	fmt.Print(prompt)
	if stdinScanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(stdinScanner.Text()))
		return answer == "y" || answer == "yes"
	}
	return false
}

// promptInput reads a line from stdin for optional input.
func promptInput(prompt string) string {
	fmt.Print(prompt)
	if stdinScanner.Scan() {
		return strings.TrimSpace(stdinScanner.Text())
	}
	return ""
}
