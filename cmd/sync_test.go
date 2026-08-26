//go:build !js

package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	gitops "github.com/sprout-foundry/sprout/pkg/git"
)

// ETH-1 sync-on-resume: `sprout sync` CLI contract tests. stdout must be
// EXACTLY one JSON object (the pinned SyncReport shape); human output goes
// to stderr only.

// runSyncCmdForTest executes syncCmd against buf and returns (stdout, stderr,
// exitErr). Flags are reset between runs because cobra flag values persist
// on the package-level command.
func runSyncCmdForTest(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	cmd := &cobra.Command{
		Use: "sync",
		// Mirror the root command: a runtime failure is not a usage
		// mistake — usage text must never pollute the JSON stdout.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          syncCmd.RunE,
	}
	cmd.Flags().AddFlagSet(syncCmd.Flags())

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestSyncCmd_CleanRepoJSON(t *testing.T) {
	dir := t.TempDir()
	runSyncTestGit(t, dir, "init", "-b", "main")
	runSyncTestGit(t, dir, "config", "user.email", "test@example.com")
	runSyncTestGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runSyncTestGit(t, dir, "add", ".")
	runSyncTestGit(t, dir, "commit", "-m", "c1")

	stdout, _, err := runSyncCmdForTest(t, "--pull=false", "--dir="+dir)
	if err != nil {
		t.Fatalf("sync cmd: %v", err)
	}

	var report gitops.SyncReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout is not the contract JSON: %v\nstdout=%q", err, stdout)
	}
	if !report.InGitRepo {
		t.Fatalf("in_git_repo = false, want true; stdout=%q", stdout)
	}
	if report.Pull.Result != gitops.SyncPullNotAttempted {
		t.Fatalf("pull.result = %q, want not_attempted", report.Pull.Result)
	}
	// stdout must be exactly one JSON object — no preamble, no trailing junk.
	trimmed := strings.TrimSpace(stdout)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		t.Fatalf("stdout must be a single JSON object, got %q", stdout)
	}
}

func TestSyncCmd_NotARepoIsReportable(t *testing.T) {
	stdout, _, err := runSyncCmdForTest(t, "--pull=false", "--dir="+t.TempDir())
	if err != nil {
		t.Fatalf("not-a-repo must exit 0, got %v", err)
	}
	var report gitops.SyncReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout not contract JSON: %v", err)
	}
	if report.InGitRepo {
		t.Fatalf("in_git_repo = true, want false; stdout=%q", stdout)
	}
}

func TestSyncCmd_CatastrophicFailureErrorJSON(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — 0000-mode dir would still be readable")
	}
	base := t.TempDir()
	bad := filepath.Join(base, "unreadable")
	if err := os.MkdirAll(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o755) })

	stdout, stderr, err := runSyncCmdForTest(t, "--pull=false", "--dir="+bad)
	if err == nil {
		t.Fatalf("expected non-zero exit for catastrophic failure, stdout=%q stderr=%q", stdout, stderr)
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("stdout must be {\"error\":...} JSON on failure, got %q", stdout)
	}
	if payload.Error == "" {
		t.Fatal("error field must be populated")
	}
	if stderr == "" {
		t.Fatal("human-readable context must go to stderr")
	}
}

func runSyncTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
