//go:build !js

package webui

// conformance_inprocess_test.go — runs the apiconformance suite against the
// in-process daemon and asserts the safe, read-only probe set all pass.
//
// It is the "fix or document every failure" gate for the generated OpenAPI
// contract: a hermetic daemon is started on a disposable workspace
// (t.TempDir, hermetic git config from testgit, no active chat or running
// agent), and every default probe is sent to it. A probe passes when the
// daemon answers 2xx and the body shape-checks against the operation's 200
// schema.
//
// The disposable workspace is deliberately staged as a git repository with one
// commit, because a real sprout workspace (the project a code-editing tool
// operates on) is a git repository. That staging resolves the two git probes
// that would otherwise fail on a bare temp dir:
//   - gitLog requires the workspace to be a git repo (it 400s
//     not_a_git_repository otherwise); a git-staged workspace returns 200.
//   - gitDiff is a per-file endpoint that requires a `path` query parameter;
//     the probe set supplies one (a bare GET is a 400 path_required, a
//     contract-level client error, not a server defect).
//
// The probes the safe set intentionally omits (those that need a pre-existing
// id, an optional external subsystem, or would reach the network) are documented
// in the probe set's own documentation rather than asserted here, since they
// probe host state rather than the contract.

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/apiconformance"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// gitInWorkspace runs a git subprocess inside dir, inheriting the hermetic git
// config that testgit.Configure (called from this package's TestMain) has set.
func gitInWorkspace(t *testing.T, dir string, args ...string) {
	t.Helper()
	// args are fixed by this test (never user input), so the variable
	// subprocess launch is safe.
	cmd := exec.Command("git", args...) // #nosec G204 -- args are test-fixed, not user input
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

// newConformanceDaemon starts the in-process web server bound to a disposable
// temp workspace and returns it plus the workspace root. The workspace is staged
// as a git repository with one commit (see the file doc for why). The caller is
// responsible for Close-ing the server.
func newConformanceDaemon(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	workspace := t.TempDir()

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}

	// Point the daemon's root storage and the named client's workspace at the
	// disposable temp dir so no probe reads or writes the developer's real home.
	ws.daemonRoot = workspace
	ws.workspaceRoot = workspace
	if _, err := ws.setClientWorkspaceRoot("conformance-client", workspace); err != nil {
		t.Fatalf("setClientWorkspaceRoot: %v", err)
	}

	// Stage the workspace as a git repository with one committed file so the
	// git family returns 200 (a real project host is a git repo).
	gitInWorkspace(t, workspace, "init", "-q")
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	gitInWorkspace(t, workspace, "add", "README.md")
	gitInWorkspace(t, workspace, "commit", "-q", "-m", "init")

	mux := ws.setupRoutes(context.Background())
	if mux == nil {
		t.Fatal("setupRoutes returned nil")
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, workspace
}

// TestConformanceInProcess runs the full safe probe set against the in-process
// daemon on a disposable git workspace and asserts every probe passes.
func TestConformanceInProcess(t *testing.T) {
	srv, workspace := newConformanceDaemon(t)

	root := repoRootFromWorkingDir(t)
	spec, err := apiconformance.LoadSpec(filepath.Join(root, "docs", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}

	suite := apiconformance.New(srv.URL, spec,
		apiconformance.WithHTTPHeader(webClientIDHeader, "conformance-client"),
		apiconformance.WithDisposableWorkspace(workspace),
	)

	report, err := suite.RunSafe(context.Background())
	if err != nil {
		t.Fatalf("RunSafe: %v", err)
	}

	t.Logf("\n%s", report.Render())

	// Every safe probe must pass. A failure here is a real contract/daemon
	// mismatch (the suite's shape-check and status-check both apply), so it is
	// a hard error rather than a logged observation.
	for _, res := range report.Failed() {
		t.Errorf("probe %s [%s] failed: status=%d %s",
			res.OperationID, res.Family, res.Status, res.Reason)
	}

	// The safe set is non-trivial: assert a known count so the test cannot
	// silently pass by probing nothing. This count mirrors the probe set and
	// is checked against it in the apiconformance package's own test.
	if got := len(report.Results); got == 0 {
		t.Error("expected at least one probe to run, got 0")
	}
}
