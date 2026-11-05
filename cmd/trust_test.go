//go:build !js

package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/injection"
)

// trustTestRoot is a synthetic absolute workspace root. Never a real path:
// test fixtures must not carry a developer's or a user's directory layout.
const trustTestRoot = "/tmp/sprout-trust-fixture/repo"

// TestRunTrustSetAndList covers the core of `trust set` and `trust list`:
// a decision recorded through the manager is read back by the lister, and the
// lister reports the classification rather than the raw boolean.
func TestRunTrustSetAndList(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	var setOut bytes.Buffer
	if err := runTrustSet(&setOut, mgr, trustTestRoot, "deps/acme", injection.TrustThirdParty); err != nil {
		t.Fatalf("runTrustSet third-party: %v", err)
	}
	if err := runTrustSet(&setOut, mgr, trustTestRoot, "our-fork", injection.TrustFirstParty); err != nil {
		t.Fatalf("runTrustSet first-party: %v", err)
	}

	// Persisted, not just in memory.
	cfg, err := configuration.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	repo := cfg.TrustedContentRoots[trustTestRoot]
	if len(repo) != 2 {
		t.Fatalf("persisted decisions = %v, want 2 entries", repo)
	}
	if !repo["deps/acme"] {
		t.Errorf("deps/acme = false, want true (third-party)")
	}
	if repo["our-fork"] {
		t.Errorf("our-fork = true, want false (ours)")
	}

	var listOut bytes.Buffer
	if err := runTrustList(&listOut, trustTestRoot); err != nil {
		t.Fatalf("runTrustList: %v", err)
	}
	got := listOut.String()
	if !strings.Contains(got, "deps/acme  third-party") {
		t.Errorf("list output missing third-party entry:\n%s", got)
	}
	if !strings.Contains(got, "our-fork  first-party") {
		t.Errorf("list output missing first-party entry:\n%s", got)
	}
	// Sorted output: deps/acme sorts before our-fork.
	if strings.Index(got, "deps/acme") > strings.Index(got, "our-fork") {
		t.Errorf("list output is not sorted:\n%s", got)
	}
}

// TestRunTrustListEmpty pins the "no decisions recorded" message — an empty
// list must say so rather than printing a bare heading.
func TestRunTrustListEmpty(t *testing.T) {
	_, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	var out bytes.Buffer
	if err := runTrustList(&out, trustTestRoot); err != nil {
		t.Fatalf("runTrustList: %v", err)
	}
	if !strings.Contains(out.String(), "no decisions recorded") {
		t.Errorf("empty list output = %q, want it to contain %q", out.String(), "no decisions recorded")
	}
}

// TestRunTrustSetNormalizesPath confirms "./deps/acme" and "deps/acme" are one
// decision, so a re-ask cannot produce two records for the same directory.
func TestRunTrustSetNormalizesPath(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	var out bytes.Buffer
	if err := runTrustSet(&out, mgr, trustTestRoot, "./deps/acme/", injection.TrustThirdParty); err != nil {
		t.Fatalf("runTrustSet: %v", err)
	}
	cfg, err := configuration.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	repo := cfg.TrustedContentRoots[trustTestRoot]
	if len(repo) != 1 {
		t.Fatalf("decisions = %v, want exactly 1 normalized entry", repo)
	}
	if !repo["deps/acme"] {
		t.Errorf("expected key %q, got %v", "deps/acme", repo)
	}
}

// TestRunTrustSetRejectsUnknownClass guards the store against a decision that
// means nothing: "unknown" is what absence means, and writing it would make
// the user's answer indistinguishable from no answer.
func TestRunTrustSetRejectsUnknownClass(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	var out bytes.Buffer
	if err := runTrustSet(&out, mgr, trustTestRoot, "deps/acme", injection.TrustUnknown); err == nil {
		t.Fatal("runTrustSet(TrustUnknown) = nil error, want rejection")
	}
	cfg, err := configuration.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.TrustedContentRoots[trustTestRoot]) != 0 {
		t.Errorf("rejected decision was persisted anyway: %v", cfg.TrustedContentRoots)
	}
}

// TestRunTrustForget covers removal, the idempotent second forget, and the
// per-repo scoping: forgetting in one repo must not touch another's record.
func TestRunTrustForget(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	otherRoot := "/tmp/sprout-trust-fixture/other-repo"
	var out bytes.Buffer
	if err := runTrustSet(&out, mgr, trustTestRoot, "deps/acme", injection.TrustThirdParty); err != nil {
		t.Fatalf("runTrustSet: %v", err)
	}
	if err := runTrustSet(&out, mgr, otherRoot, "deps/acme", injection.TrustThirdParty); err != nil {
		t.Fatalf("runTrustSet other root: %v", err)
	}

	if err := runTrustForget(&out, mgr, trustTestRoot, "deps/acme"); err != nil {
		t.Fatalf("runTrustForget: %v", err)
	}
	cfg, err := configuration.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := cfg.TrustedContentRoots[trustTestRoot]; ok {
		t.Errorf("emptied repo should be dropped from the store, got %v", cfg.TrustedContentRoots)
	}
	if !cfg.TrustedContentRoots[otherRoot]["deps/acme"] {
		t.Errorf("forget in one repo removed another repo's decision: %v", cfg.TrustedContentRoots)
	}

	// Forgetting again is not an error, but says so.
	var second bytes.Buffer
	if err := runTrustForget(&second, mgr, trustTestRoot, "deps/acme"); err != nil {
		t.Fatalf("second runTrustForget: %v", err)
	}
	if !strings.Contains(second.String(), "no decision recorded") {
		t.Errorf("second forget output = %q, want a plain no-op message", second.String())
	}
}

// TestRunTrustForgetNormalizesPath mirrors the set-side normalization: the
// user may forget the path the way they see it in the tree.
func TestRunTrustForgetNormalizesPath(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	var out bytes.Buffer
	if err := runTrustSet(&out, mgr, trustTestRoot, "deps/acme", injection.TrustThirdParty); err != nil {
		t.Fatalf("runTrustSet: %v", err)
	}
	if err := runTrustForget(&out, mgr, trustTestRoot, "./deps/acme"); err != nil {
		t.Fatalf("runTrustForget: %v", err)
	}
	cfg, err := configuration.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := cfg.TrustedContentRoots[trustTestRoot]; ok {
		t.Errorf("normalized forget did not remove the record: %v", cfg.TrustedContentRoots)
	}
}

// TestRunTrustSetNilManager pins the guard: a missing manager is a permission
// problem, not a silent success.
func TestRunTrustSetNilManager(t *testing.T) {
	var out bytes.Buffer
	if err := runTrustSet(&out, nil, trustTestRoot, "deps/acme", injection.TrustThirdParty); err == nil {
		t.Fatal("runTrustSet(nil manager) = nil error, want failure")
	}
	if err := runTrustForget(&out, nil, trustTestRoot, "deps/acme"); err == nil {
		t.Fatal("runTrustForget(nil manager) = nil error, want failure")
	}
}

// TestRunTrustSetWarnsOnMarkerShadowing: a known marker outranks a recorded
// decision, so classifying a vendored path as "ours" must not look like it
// took effect.
func TestRunTrustSetWarnsOnMarkerShadowing(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	var out bytes.Buffer
	if err := runTrustSet(&out, mgr, trustTestRoot, "vendor/acme", injection.TrustFirstParty); err != nil {
		t.Fatalf("runTrustSet: %v", err)
	}
	if !strings.Contains(out.String(), "known dependency tree") {
		t.Errorf("output = %q, want a warning that the marker still wins", out.String())
	}
	if got := injection.ClassifyPath("vendor/acme/lib.go", map[string]bool{"vendor/acme": false}); got != injection.TrustThirdParty {
		t.Errorf("classification after the shadowed decision = %v, want TrustThirdParty", got)
	}
}

func TestParseTrustClass(t *testing.T) {
	cases := map[string]injection.TrustClass{
		"third-party": injection.TrustThirdParty,
		"THIRD-PARTY": injection.TrustThirdParty,
		"third_party": injection.TrustThirdParty,
		" ours ":      injection.TrustFirstParty,
		"first-party": injection.TrustFirstParty,
	}
	for in, want := range cases {
		got, err := parseTrustClass(in)
		if err != nil {
			t.Errorf("parseTrustClass(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseTrustClass(%q) = %v, want %v", in, got, want)
		}
	}
	for _, bad := range []string{"", "unknown", "yes", "third party"} {
		if _, err := parseTrustClass(bad); err == nil {
			t.Errorf("parseTrustClass(%q) = nil error, want rejection", bad)
		}
	}
}

// TestTrustWorkspaceRootStripsSproutDir pins the key shape: the store is keyed
// by the repository root, and detectGitRepo reports the .sprout config dir, so
// taking its value verbatim would key decisions on a path that moves with the
// config layout.
func TestTrustWorkspaceRootStripsSproutDir(t *testing.T) {
	prev := autoDetectedWorkspaceDir
	t.Cleanup(func() { autoDetectedWorkspaceDir = prev })

	autoDetectedWorkspaceDir = "/tmp/sprout-trust-fixture/repo/.sprout"
	root, err := trustWorkspaceRoot()
	if err != nil {
		t.Fatalf("trustWorkspaceRoot: %v", err)
	}
	if root != "/tmp/sprout-trust-fixture/repo" {
		t.Errorf("trustWorkspaceRoot() = %q, want the repo root %q", root, "/tmp/sprout-trust-fixture/repo")
	}
}

// TestTrustWorkspaceRootFallsBackToCwd covers the outside-a-repo case: with no
// auto-detected repo the working directory is the root, and it is never empty
// (an empty key would pool every directory's decisions together).
func TestTrustWorkspaceRootFallsBackToCwd(t *testing.T) {
	prev := autoDetectedWorkspaceDir
	t.Cleanup(func() { autoDetectedWorkspaceDir = prev })
	autoDetectedWorkspaceDir = ""

	root, err := trustWorkspaceRoot()
	if err != nil {
		t.Fatalf("trustWorkspaceRoot: %v", err)
	}
	if root == "" {
		t.Fatal("trustWorkspaceRoot() returned an empty root")
	}
	if !filepath.IsAbs(root) {
		t.Errorf("trustWorkspaceRoot() = %q, want an absolute path", root)
	}
}

// TestTrustCommandRegistered pins the wiring: the command exists, is in the
// config group, and carries its three subcommands. Group IDs are assigned by
// applyCommandGroups (called from Execute), so the test runs that same step.
func TestTrustCommandRegistered(t *testing.T) {
	applyCommandGroups(rootCmd)

	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "trust" {
			found = true
			if c.GroupID != "config" {
				t.Errorf("trust GroupID = %q, want %q", c.GroupID, "config")
			}
		}
	}
	if !found {
		t.Fatal("trust command is not registered on rootCmd")
	}
	names := map[string]bool{}
	for _, c := range trustCmd.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"list", "set", "forget"} {
		if !names[want] {
			t.Errorf("trust subcommand %q missing (have %v)", want, names)
		}
	}
}
