//go:build !js

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	gitops "github.com/sprout-foundry/sprout/pkg/git"
)

var (
	// syncAttemptPull defaults to true: `sprout sync` reconciles, it does not
	// merely report. --pull=false gives a strictly read-only status report.
	syncAttemptPull bool
	syncRepoDir     string
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Reconcile workspace git state (uncommitted files, ahead/behind, last commit, optional fast-forward pull) as JSON",
	Long: `Print a JSON summary of the current repository's git state.

This is the sync-on-resume half of ETH-1: the platform runs it inside a
resumed workspace container to reconcile uncommitted edits, ahead/behind
counts against the upstream and the last commit, and (by default) to attempt
a non-destructive "git pull --ff-only".

stdout is EXACTLY the JSON report (a single object), so it can be piped
straight into a parser; human-readable logging goes to stderr only.

Exit codes: 0 for every reportable state (including "not a git repository");
1 only for catastrophic failure, in which case {"error":"..."} is printed to
stdout instead.

The pull is never destructive: it only runs when the branch has an upstream
and no tracked file is dirty, uses --ff-only, and reports a git refusal as
"result":"error" with git's own message instead of working around it.`,
	RunE: runSyncCmd,
}

func runSyncCmd(cmd *cobra.Command, args []string) error {
	repoDir := syncRepoDir
	if repoDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return writeSyncCmdError(cmd, fmt.Errorf("failed to resolve working directory: %w", err))
		}
		repoDir = cwd
	}

	report, err := gitops.RunSync(context.Background(), repoDir, syncAttemptPull)
	if err != nil {
		return writeSyncCmdError(cmd, err)
	}

	// json.Encoder terminates the object with a newline — allowed by the
	// contract ("trailing newline fine") and friendlier for shells.
	return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
}

// writeSyncCmdError renders the catastrophic-failure shape. stdout stays
// machine-readable ({"error":"..."}), so any human context goes to stderr.
// The returned error is marked reported so cmd.Execute() still exits 1
// without re-printing the message.
func writeSyncCmdError(cmd *cobra.Command, err error) error {
	fmt.Fprintf(cmd.ErrOrStderr(), "sprout sync: %v\n", err)
	_ = json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
		Error string `json:"error"`
	}{Error: err.Error()})
	return markReported(err)
}

func init() {
	syncCmd.Flags().BoolVar(&syncAttemptPull, "pull", true, "Attempt a non-destructive git pull --ff-only when the repo is clean and has an upstream (true by default; --pull=false reports status only)")
	syncCmd.Flags().StringVar(&syncRepoDir, "dir", "", "Repository directory to inspect (defaults to the current working directory)")
}
