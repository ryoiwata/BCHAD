package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"

	"github.com/athena-digital/bchad/workflows"
)

func init() {
	rootCmd.AddCommand(statusCmd)
}

var statusCmd = &cobra.Command{
	Use:   "status <run-id>",
	Short: "Query the status of a pipeline run",
	Args:  cobra.ExactArgs(1),
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	runID := args[0]

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

	resp, err := c.QueryWorkflow(ctx, runID, "", workflows.StatusQueryName)
	if err != nil {
		return fmt.Errorf("query workflow %s: %w", runID, err)
	}

	var status workflows.PipelineStatus
	if err := resp.Get(&status); err != nil {
		return fmt.Errorf("decode status: %w", err)
	}

	printStatus(&status)
	return nil
}

func printStatus(s *workflows.PipelineStatus) {
	fmt.Printf("\n=== Pipeline Run: %s ===\n", s.RunID)
	fmt.Printf("Status:     %s\n", s.Status)
	if s.RunningStage != "" {
		fmt.Printf("Running:    %s\n", s.RunningStage)
	}
	fmt.Printf("Cost:       $%.4f\n", s.AccumulatedCost)
	fmt.Println()
	fmt.Printf("%-12s %s\n", "STAGE", "STATUS")
	fmt.Println(strings.Repeat("-", 40))
	for _, d := range s.StageDetails {
		status := d.Status
		if d.Cost > 0 {
			status = fmt.Sprintf("%s ($%.4f)", d.Status, d.Cost)
		}
		fmt.Printf("%-12s %s\n", d.StageID, status)
	}
	fmt.Println()
}
