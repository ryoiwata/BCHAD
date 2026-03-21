// Command worker is the Temporal worker for the BCHAD software factory.
//
// It connects to the Temporal dev server, registers all workflows and activities,
// and processes pipeline tasks dispatched by the control plane.
package main

import (
	"log/slog"
	"os"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/athena-digital/bchad/workflows"
)

func main() {
	temporalHost := os.Getenv("BCHAD_TEMPORAL_HOST")
	if temporalHost == "" {
		slog.Error("BCHAD_TEMPORAL_HOST is required but not set")
		os.Exit(1)
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
		slog.Error("failed to connect to Temporal server", "host", temporalHost, "error", err)
		os.Exit(1)
	}
	defer c.Close()

	slog.Info("BCHAD Temporal worker started",
		"host", temporalHost,
		"namespace", namespace,
	)

	w := worker.New(c, "bchad-pipeline", worker.Options{})

	w.RegisterWorkflow(workflows.PipelineWorkflow)
	w.RegisterActivity(workflows.ExecuteStageActivity)
	w.RegisterActivity(workflows.AssemblePRActivity)
	w.RegisterActivity(workflows.Tier2GateActivity)

	if err := w.Run(worker.InterruptCh()); err != nil {
		slog.Error("worker stopped with error", "error", err)
		os.Exit(1)
	}
}
