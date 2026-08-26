//go:build !js

package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sprout-foundry/sprout/pkg/txn"
)

// ETH-2 transactional escalation: `sprout txn-status` / `txn-push` /
// `txn-pull` CLI contract tests. stdout must be EXACTLY one JSON object of
// the pinned shape; human output goes to stderr only.

// runTxnCmdForTest executes one of the txn commands against fresh buffers.
// A fresh cobra.Command is built per run AND the package-level flag vars are
// reset first: AddFlagSet shares the underlying Flag values, so a --in=<path>
// set by one test would otherwise leak into the next through txnPushIn.
func runTxnCmdForTest(t *testing.T, template *cobra.Command, stdin *bytes.Buffer, args ...string) (string, string, error) {
	t.Helper()
	resetTxnFlagVars()

	// A fresh command per run, wearing the package-level command's RunE and
	// flags: cobra flag VALUES persist on the package-level commands, so
	// copying them keeps runs independent without re-declaring the flags.
	cmd := &cobra.Command{
		Use:           template.Use,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          template.RunE,
	}
	cmd.Flags().AddFlagSet(template.Flags())

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if stdin != nil {
		cmd.SetIn(stdin)
	}
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

// resetTxnFlagVars restores the declared flag defaults. The flags are bound
// by StringVar to these package vars, so assigning them re-points the flag
// value a fresh run reads.
func resetTxnFlagVars() {
	txnStatusRepoDir = ""
	txnPushRepoDir = ""
	txnPushIn = "-"
	txnPullRepoDir = ""
	txnPullOut = "-"
}

func runTxnTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func newTxnCLIRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
	dir := t.TempDir()
	runTxnTestGit(t, dir, "init", "-b", "main")
	runTxnTestGit(t, dir, "config", "user.email", "test@example.com")
	runTxnTestGit(t, dir, "config", "user.name", "Test User")
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTxnTestGit(t, dir, "add", ".")
	runTxnTestGit(t, dir, "commit", "-m", "c1")
	return dir
}

// ---------- txn-status ----------

func TestTxnStatusCmd_ReportsTreeState(t *testing.T) {
	dir := newTxnCLIRepo(t)
	// A committed file that is then removed gives a real delete.
	if err := os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("bye\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTxnTestGit(t, dir, "add", "extra.txt")
	runTxnTestGit(t, dir, "commit", "-m", "c2")

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("u"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "extra.txt")); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := runTxnCmdForTest(t, txnStatusCmd, nil, "--dir="+dir)
	if err != nil {
		t.Fatalf("txn-status: %v", err)
	}

	var got txn.Status
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not the contract JSON: %v\nstdout=%q", err, stdout)
	}
	if !got.InGitRepo || got.Branch != "main" {
		t.Fatalf("status = %+v; stdout=%q", got, stdout)
	}
	if got.TotalChanges != 3 {
		t.Fatalf("total_changes = %d, want 3; stdout=%q", got.TotalChanges, stdout)
	}
	if len(got.DirtyFiles) != 1 || len(got.UntrackedFiles) != 1 || len(got.DeletedFiles) != 1 {
		t.Fatalf("lists = %v / %v / %v; stdout=%q", got.DirtyFiles, got.UntrackedFiles, got.DeletedFiles, stdout)
	}
	trimmed := strings.TrimSpace(stdout)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		t.Fatalf("stdout must be a single JSON object, got %q", stdout)
	}
}

func TestTxnStatusCmd_NotARepoIsReportable(t *testing.T) {
	stdout, _, err := runTxnCmdForTest(t, txnStatusCmd, nil, "--dir="+t.TempDir())
	if err != nil {
		t.Fatalf("not-a-repo must exit 0, got %v", err)
	}
	var got txn.Status
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout not contract JSON: %v", err)
	}
	if got.InGitRepo {
		t.Fatalf("in_git_repo = true, want false; stdout=%q", stdout)
	}
}

// ---------- txn-push ----------

func txnManifestJSON(t *testing.T, files map[string]string, deletes ...string) string {
	t.Helper()
	type entry struct {
		Path          string `json:"path"`
		ContentBase64 string `json:"content_base64"`
		Size          int    `json:"size"`
		Mode          string `json:"mode"`
	}
	var entries []entry
	for path, content := range files {
		entries = append(entries, entry{
			Path:          path,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte(content)),
			Size:          len(content),
			Mode:          "0644",
		})
	}
	payload := struct {
		Base struct {
			GitSha string `json:"git_sha"`
			Client string `json:"client"`
		} `json:"base"`
		Files     []entry    `json:"files"`
		Deletes   []string   `json:"deletes"`
		Truncated bool       `json:"truncated"`
		Skipped   []struct{} `json:"skipped"`
	}{}
	payload.Base.Client = "wasm"
	payload.Files = entries
	payload.Deletes = deletes
	if payload.Deletes == nil {
		payload.Deletes = []string{}
	}
	if payload.Files == nil {
		payload.Files = []entry{}
	}
	if payload.Skipped == nil {
		payload.Skipped = []struct{}{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestTxnPushCmd_AppliesManifestFromStdin(t *testing.T) {
	dir := t.TempDir()

	stdin := bytes.NewBufferString(txnManifestJSON(t,
		map[string]string{"src/main.go": "package main\n"},
		"old.txt"))

	stdout, _, err := runTxnCmdForTest(t, txnPushCmd, stdin, "--dir="+dir, "--in=-")
	if err != nil {
		t.Fatalf("txn-push: %v", err)
	}
	var result txn.ApplyResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout is not the contract JSON: %v\nstdout=%q", err, stdout)
	}
	if result.Applied != 1 || result.Status != txn.StatusOK {
		t.Fatalf("result = %+v; stdout=%q", result, stdout)
	}
	data, err := os.ReadFile(filepath.Join(dir, "src/main.go"))
	if err != nil || string(data) != "package main\n" {
		t.Fatalf("src/main.go = %q err=%v", data, err)
	}
}

func TestTxnPushCmd_AppliesManifestFromFile(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(txnManifestJSON(t,
		map[string]string{"a.txt": "one", "b.txt": "two"})), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := runTxnCmdForTest(t, txnPushCmd, nil, "--dir="+dir, "--in="+manifestPath)
	if err != nil {
		t.Fatalf("txn-push: %v", err)
	}
	var result txn.ApplyResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout not contract JSON: %v", err)
	}
	if result.Applied != 2 {
		t.Fatalf("applied = %d, want 2; stdout=%q", result.Applied, stdout)
	}
}

func TestTxnPushCmd_PartialIsExit0(t *testing.T) {
	dir := t.TempDir()
	stdin := bytes.NewBufferString(`{
		"base": {"git_sha": "", "client": "wasm"},
		"files": [
			{"path": "ok.txt", "content_base64": "` + base64.StdEncoding.EncodeToString([]byte("x")) + `"},
			{"path": "../escape.txt", "content_base64": "` + base64.StdEncoding.EncodeToString([]byte("y")) + `"}
		],
		"deletes": [], "truncated": false, "skipped": []
	}`)

	stdout, _, err := runTxnCmdForTest(t, txnPushCmd, stdin, "--dir="+dir)
	if err != nil {
		t.Fatalf("a partial apply is reportable and must exit 0, got %v", err)
	}
	var result txn.ApplyResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout not contract JSON: %v", err)
	}
	if result.Status != txn.StatusPartial || len(result.Skipped) != 1 {
		t.Fatalf("result = %+v; stdout=%q", result, stdout)
	}
}

func TestTxnPushCmd_InvalidJSONIsUsageError(t *testing.T) {
	stdin := bytes.NewBufferString("not json")
	stdout, stderr, err := runTxnCmdForTest(t, txnPushCmd, stdin, "--dir="+t.TempDir())
	if err == nil {
		t.Fatalf("expected a non-zero exit for an unreadable manifest, stdout=%q stderr=%q", stdout, stderr)
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("stdout must be {\"error\":...} JSON, got %q", stdout)
	}
	if payload.Error == "" {
		t.Fatal("error field must be populated")
	}
	if stderr == "" {
		t.Fatal("human-readable context must go to stderr")
	}
}

func TestTxnPushCmd_MissingInputFileIsIOError(t *testing.T) {
	stdout, _, err := runTxnCmdForTest(t, txnPushCmd, nil,
		"--dir="+t.TempDir(), "--in=/nonexistent/manifest.json")
	if err == nil {
		t.Fatalf("expected a non-zero exit, stdout=%q", stdout)
	}
	if !strings.Contains(stdout, `"error"`) {
		t.Fatalf("stdout must stay machine-readable, got %q", stdout)
	}
}

// ---------- txn-pull ----------

func TestTxnPullCmd_WritesManifestToStdout(t *testing.T) {
	dir := newTxnCLIRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "src/new.go"), []byte("package src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := runTxnCmdForTest(t, txnPullCmd, nil, "--dir="+dir)
	if err != nil {
		t.Fatalf("txn-pull: %v", err)
	}
	var manifest txn.DeltaManifest
	if err := json.Unmarshal([]byte(stdout), &manifest); err != nil {
		t.Fatalf("stdout is not the contract JSON: %v\nstdout=%q", err, stdout)
	}
	if manifest.Truncated {
		t.Fatalf("truncated = true; skipped=%+v", manifest.Skipped)
	}
	if len(manifest.Deletes) != 1 || manifest.Deletes[0] != "README.md" {
		t.Fatalf("deletes = %v; stdout=%q", manifest.Deletes, stdout)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "src/new.go" {
		t.Fatalf("files = %+v; stdout=%q", manifest.Files, stdout)
	}
	decoded, derr := base64.StdEncoding.DecodeString(manifest.Files[0].ContentBase64)
	if derr != nil || string(decoded) != "package src\n" {
		t.Fatalf("content = %q err=%v", decoded, derr)
	}
}

func TestTxnPullCmd_WritesManifestToFile(t *testing.T) {
	dir := newTxnCLIRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "pull.json")
	stdout, _, err := runTxnCmdForTest(t, txnPullCmd, nil, "--dir="+dir, "--out="+out)
	if err != nil {
		t.Fatalf("txn-pull: %v", err)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("stdout must stay empty when --out is a file, got %q", stdout)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var manifest txn.DeltaManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("--out file is not the contract JSON: %v", err)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "notes.txt" {
		t.Fatalf("files = %+v", manifest.Files)
	}
}

func TestTxnPullCmd_NotARepoIsReportable(t *testing.T) {
	stdout, _, err := runTxnCmdForTest(t, txnPullCmd, nil, "--dir="+t.TempDir())
	if err != nil {
		t.Fatalf("not-a-repo must exit 0, got %v", err)
	}
	var manifest txn.DeltaManifest
	if err := json.Unmarshal([]byte(stdout), &manifest); err != nil {
		t.Fatalf("stdout not contract JSON: %v", err)
	}
	if len(manifest.Files) != 0 || manifest.Truncated {
		t.Fatalf("manifest = %+v, want empty", manifest)
	}
}

func TestTxnPushPullCmd_RoundTrip(t *testing.T) {
	// The CLI mirror of the platform's three-phase flow: push into a repo,
	// then pull back and confirm the manifest describes what landed.
	dir := newTxnCLIRepo(t)

	stdin := bytes.NewBufferString(txnManifestJSON(t,
		map[string]string{"pkg/txn/new.go": "package txn\n"}, "README.md"))
	stdout, _, err := runTxnCmdForTest(t, txnPushCmd, stdin, "--dir="+dir)
	if err != nil {
		t.Fatalf("txn-push: %v", err)
	}
	var applied txn.ApplyResult
	if err := json.Unmarshal([]byte(stdout), &applied); err != nil {
		t.Fatal(err)
	}
	if applied.Status != txn.StatusOK {
		t.Fatalf("apply = %+v", applied)
	}

	stdout, _, err = runTxnCmdForTest(t, txnPullCmd, nil, "--dir="+dir)
	if err != nil {
		t.Fatalf("txn-pull: %v", err)
	}
	var manifest txn.DeltaManifest
	if err := json.Unmarshal([]byte(stdout), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Truncated {
		t.Fatalf("truncated = true; skipped=%+v", manifest.Skipped)
	}
	if len(manifest.Deletes) != 1 || manifest.Deletes[0] != "README.md" {
		t.Fatalf("deletes = %v", manifest.Deletes)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "pkg/txn/new.go" {
		t.Fatalf("files = %+v", manifest.Files)
	}
}
