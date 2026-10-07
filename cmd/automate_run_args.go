//go:build !js

package cmd

// automate_run_args.go — the agent-subprocess argument assembly for
// `sprout automate run`, split out of automate_run.go to keep that file
// under the source-file size guideline.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/automate"
)

// buildAgentSubprocessArgs constructs the argument list for the sprout agent
// subprocess that executes the workflow. Extracted for testability.
func buildAgentSubprocessArgs(path string, summary *automate.Summary) []string {
	args := []string{"agent", "--workflow-config", path, "--yes", "--no-web-ui"}

	// Plumb --max-iterations from the workflow JSON.
	// Non-zero values are passed explicitly; 0 (unlimited) is the default so
	// we don't pass the flag when it's 0 or nil.
	if summary != nil && summary.Initial != nil && summary.Initial.MaxIterations > 0 {
		args = append(args, "--max-iterations", strconv.Itoa(summary.Initial.MaxIterations))
	}

	if automateBudgetUSD > 0 {
		args = append(args, "--budget-usd", fmt.Sprintf("%g", automateBudgetUSD))
	}
	if strings.TrimSpace(automateBudgetWarn) != "" {
		args = append(args, "--budget-warn", automateBudgetWarn)
	}
	if automateHeartbeatSeconds > 0 {
		args = append(args, "--heartbeat", fmt.Sprintf("%d", automateHeartbeatSeconds))
	}

	return args
}
