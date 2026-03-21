// Command bchad is the CLI for the BCHAD software factory.
//
// Usage:
//
//	bchad [command] [flags]
//
// Run 'bchad --help' for a full list of commands and flags.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "bchad",
	Short: "BCHAD: Batch Code Harvesting, Assembly, and Deployment",
	Long: `BCHAD is a software factory that transforms feature specifications into
complete, tested, deployable pull requests — matching existing codebase
conventions across multiple products, languages, and frameworks.

It is an internal tool for Athena Digital, operating across seven SaaS products
in fintech, healthtech, and logistics.`,
}
