package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"

	"github.com/athena-digital/bchad/workflows"
)

func init() {
	rootCmd.AddCommand(approveCmd)
	approveCmd.Flags().StringP("stage", "s", "", "stage ID to approve (required)")
	approveCmd.Flags().StringP("guidance", "g", "", "guidance note for rejection (optional)")
	approveCmd.Flags().Bool("reject", false, "reject the stage instead of approving")
	_ = approveCmd.MarkFlagRequired("stage")
}

var approveCmd = &cobra.Command{
	Use:   "approve <run-id>",
	Short: "Send an approval or rejection signal to a pipeline run",
	Long: `Sends an approval or rejection decision to a waiting pipeline workflow.

Examples:
  bchad approve run-pf-20260315-001 --stage migrate
  bchad approve run-pf-20260315-001 --stage migrate --reject --guidance "Needs review"`,
	Args: cobra.ExactArgs(1),
	RunE: runApprove,
}

func runApprove(cmd *cobra.Command, args []string) error {
	runID := args[0]
	stageID, _ := cmd.Flags().GetString("stage")
	guidance, _ := cmd.Flags().GetString("guidance")
	reject, _ := cmd.Flags().GetBool("reject")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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

	decision := "approve"
	if reject {
		decision = "reject"
	}

	signal := workflows.ApprovalDecision{
		StageID:      stageID,
		Decision:     decision,
		GuidanceNote: guidance,
	}

	if err := c.SignalWorkflow(ctx, runID, "", workflows.ApprovalSignalName, signal); err != nil {
		return fmt.Errorf("send approval signal to workflow %s: %w", runID, err)
	}

	if reject {
		fmt.Printf("✗ Rejected stage '%s' on run %s\n", stageID, runID)
		if guidance != "" {
			fmt.Printf("  Guidance: %s\n", guidance)
		}
	} else {
		fmt.Printf("✓ Approved stage '%s' on run %s\n", stageID, runID)
	}

	return nil
}
