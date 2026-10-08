// runner_verify_gate.go — the runner's verification-gate guarantees:
// the run's non-interactive security posture, the git baseline it takes
// in each fresh copy, and the backstop that forces verification when the
// turn's change window stayed closed but the copy changed anyway.
//
// The turn-end hook is driven by the agent's change tracker: it verifies
// only when the turn recorded application-code changes in its window
// (Agent.TurnChangedApplicationPaths). Two failure modes can leave that
// window empty on a turn that did change the copy:
//
//   - A headless run has no approval surface. In an interactive session
//     a shell command the classifier marks for approval is approved by
//     the user; in a benchmark run there is nobody to ask, so the command
//     is declined and the edit never lands — the model's work is silently
//     lost. The runner therefore gives each run the non-interactive
//     posture the run needs to actually work (applyRunSecurityPosture).
//   - A change can reach the filesystem without reaching the tracker's
//     window: a tool path not wired to the tracker, a shell command whose
//     walk was skipped, or a subagent merge that missed the window. The
//     git baseline and the backstop below catch that residual case: when
//     the tracker reports no changes yet the copy has application-code
//     changes on disk, the runner runs verification through the agent's
//     forced seam so the run still produces a verdict.
package benchmark

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/git"
)

// applyRunSecurityPosture gives the run's headless agent the security
// posture a non-interactive run needs to complete its task.
//
// A benchmark run has no approval surface: no terminal prompt (stdin is
// closed) and no Web UI. Any shell command the classifier escalates to
// "prompt" is therefore declined — with nobody to ask, the approval
// adapter's headless path returns a rejection — and the model's edit never
// lands, so the run is scored on a copy that was never changed. The run's
// workspace is a throwaway copy of the starter, so the runner lifts the
// CAUTION-tier shell prompts only: unsafe-shell mode auto-approves the
// shell_command prompts the classifier escalates (the --unsafe-shell
// posture), while DANGEROUS classification and the classifier's hard blocks
// (destructive system paths) stay enforced by the handlers' own
// IsHardBlock early-returns. This is narrower than the blanket unsafe mode,
// which would skip approval for every non-hard-block operation rather than
// just shell: the run needs the shell edit to land, not all security checks
// lifted.
func applyRunSecurityPosture(ag *agent.Agent) {
	if ag == nil {
		return
	}
	ag.SetUnsafeShellMode(true)
}

// initGitBaseline initializes a git repository in the run's fresh copy
// and commits the copy's starting state, so the backstop below can ask
// git what the turn changed with a single `git status` — independent of
// the agent's change tracker.
//
// It is best-effort: a missing git binary or any failure is ignored
// (baselineReady=false), and the run proceeds. The baseline is a backstop
// for the tracker, never a run-blocking requirement, and a benchmark must
// not fail a run because the host has no git. The commit is made with a
// local, throwaway identity so it never depends on the user's git config
// and never touches a real repository (the copy is a fresh MkdirTemp
// directory with no .git of its own).
func initGitBaseline(runDir string) bool {
	if _, err := exec.LookPath("git"); err != nil {
		return false
	}
	// Each step is a fixed git invocation (no caller-supplied arguments):
	// init, stage everything, then commit with a local throwaway identity.
	if err := runGit(runDir, "init", "-q"); err != nil {
		return false
	}
	if err := runGit(runDir, "add", "-A"); err != nil {
		return false
	}
	err := runGit(runDir,
		"-c", "user.email=benchmark@sprout.local",
		"-c", "user.name=sprout benchmark",
		"commit", "-q", "-m", "benchmark baseline")
	return err == nil
}

// runGit runs one git command in runDir with the environment a benchmark
// needs: no system git config and no interactive credential prompt, so the
// baseline never depends on (or mutates) the host's git configuration.
func runGit(runDir string, args ...string) error {
	cmd := git.SafeGitCmd(runDir, args...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	return cmd.Run()
}

// gitChangedApplicationPaths returns the workspace-relative paths the
// copy changed since the baseline commit that count as application code
// (agent.IsApplicationCodePath). It reads `git status --porcelain` and
// parses each entry's path, handling renames ("old -> new") by taking the
// destination. A missing baseline, no git, or a git error returns nil: the
// backstop must never fire on an unreadable status, and a nil result is
// the safe "no independent evidence" answer.
func gitChangedApplicationPaths(runDir string, baselineReady bool) []string {
	if !baselineReady {
		return nil
	}
	cmd := git.SafeGitCmd(runDir, "status", "--porcelain")
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		// Porcelain v1: two status columns, a space, then the path. A
		// rename/copy entry is "R  old -> new"; take the destination.
		path := strings.TrimSpace(line[3:])
		path = strings.Trim(path, `"`)
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = strings.Trim(path[idx+4:], `"`)
		}
		if path == "" {
			continue
		}
		if agent.IsApplicationCodePath(path) {
			paths = append(paths, path)
		}
	}
	return paths
}

// runVerificationBackstop forces the turn's verification when the
// turn-end hook never ran but the copy changed application code.
//
// It is invoked after the turn, before the runner reads the result. When
// the turn-end hook already stored a result, or the tracker's window is
// non-empty (the hook will run or has run), it does nothing.
// Otherwise it asks git what changed since the baseline; if any changed
// path is application code it runs verification through the agent's
// forced seam (Agent.RunForcedTurnEndVerification) — the same trusted
// checks the hook runs, stored as the turn's result so the runner's
// existing LastVerificationResult read picks it up.
//
// The change list is not passed to verification: the checks come only
// from the manifest and the explicit configuration, and the require-a-
// test input already treats the (empty) window as authoritative — the
// backstop's job is to produce the verdict the window's gap would have
// erased, not to reclassify the turn's changes.
func (r *Runner) runVerificationBackstop(ctx context.Context, ag *agent.Agent, runDir string, baselineReady bool) {
	if ag == nil {
		return
	}
	if ag.LastVerificationResult() != nil {
		return
	}
	if len(ag.TurnChangedApplicationPaths()) > 0 {
		return
	}
	if len(gitChangedApplicationPaths(runDir, baselineReady)) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// A setup error leaves no result; the run is recorded with its own
	// outcome (no passing result, no pass), exactly as a hook-run setup
	// error would.
	_, _ = ag.RunForcedTurnEndVerification(ctx)
}
