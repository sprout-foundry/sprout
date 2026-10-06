package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test doubles for the injected seams
// ---------------------------------------------------------------------------

// stubRunner is a BuildRunner that records its calls and returns a canned
// error. It never spawns a process, so the tests describe the orchestrator's
// behaviour, not a shell's.
type stubRunner struct {
	calls   int
	lastDir string
	lastCmd string
	err     error
}

func (s *stubRunner) run(_ context.Context, root, command string) error {
	s.calls++
	s.lastDir = root
	s.lastCmd = command
	return s.err
}

// stubFingerprint is a TreeFingerprint returning a scripted sequence of
// values, one per call (the last value repeats once the sequence is spent).
// It records how many times it was asked and the skip paths each call carried,
// so a test can prove the gate ran before the build and that the build output
// directory is excluded.
type stubFingerprint struct {
	values  []string
	err     error
	calls   int
	lastArg []string
}

func (s *stubFingerprint) fingerprint(_ string, skip ...string) (string, error) {
	s.calls++
	s.lastArg = skip
	if s.err != nil {
		return "", s.err
	}
	if len(s.values) == 0 {
		return "", nil
	}
	i := s.calls - 1
	if i >= len(s.values) {
		i = len(s.values) - 1
	}
	return s.values[i], nil
}

// buildReq is a filled-in BuildRequest so each test reads as the behaviour it
// exercises.
func buildReq(root string) BuildRequest {
	return BuildRequest{
		Root:     root,
		Command:  "npm run build",
		BuildDir: filepath.Join(root, "dist"),
		Project:  "web-app",
		Kind:     KindPreview,
		Version:  "1.0.0",
	}
}

// passingSnapshot is a verification snapshot that passed on the tree the stub
// fingerprint reports.
func passingSnapshot(fp string) VerificationSnapshot {
	return VerificationSnapshot{Passed: true, Fingerprint: fp}
}

// ---------------------------------------------------------------------------
// Happy path
// ---------------------------------------------------------------------------

// TestBuildAndDeploy_BuildsInWorkspaceThenUploads is the acceptance anchor:
// with a passing verification over an unchanged tree, the manifest's build
// command runs in the project root, and the target receives the built output
// directory.
func TestBuildAndDeploy_BuildsInWorkspaceThenUploads(t *testing.T) {
	root := t.TempDir()
	req := buildReq(root)

	runner := &stubRunner{}
	fp := &stubFingerprint{values: []string{"tree-at-verification"}}
	target := NewFake()

	d := &Deployer{Target: target, Run: runner.run, Fingerprint: fp.fingerprint}

	got, err := d.BuildAndDeploy(context.Background(), req, passingSnapshot("tree-at-verification"))
	require.NoError(t, err)

	// The build ran once, in the workspace, with the manifest's command.
	assert.Equal(t, 1, runner.calls, "build runs exactly once")
	assert.Equal(t, root, runner.lastDir, "build runs in the project root, not the target")
	assert.Equal(t, "npm run build", runner.lastCmd, "build uses the manifest's command")

	// The target recorded the deploy and received the built directory.
	calls := target.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "Deploy", calls[0].Method)
	assert.Equal(t, "web-app", calls[0].Project)

	require.NotEmpty(t, got.ID)
	assert.Equal(t, "web-app", got.Project)
	assert.Equal(t, req.Version, got.Version)
	assert.Equal(t, StatusReady, got.Status)
}

// TestBuildAndDeploy_FingerprintCheckedBeforeAndAfterBuild asserts the gate
// brackets the build: the fingerprint is read before building (to refuse a
// tree that moved since verification) and again after, so a change during the
// build is caught too.
func TestBuildAndDeploy_FingerprintCheckedBeforeAndAfterBuild(t *testing.T) {
	root := t.TempDir()
	req := buildReq(root)
	runner := &stubRunner{}
	fp := &stubFingerprint{values: []string{"same", "same"}}
	target := NewFake()

	d := &Deployer{Target: target, Run: runner.run, Fingerprint: fp.fingerprint}
	_, err := d.BuildAndDeploy(context.Background(), req, passingSnapshot("same"))
	require.NoError(t, err)

	assert.Equal(t, 2, fp.calls, "fingerprint read before and after the build")
	assert.Equal(t, 1, runner.calls)
	assert.Equal(t, []string{req.BuildDir}, fp.lastArg,
		"the build output directory is excluded from the fingerprint")
}

// ---------------------------------------------------------------------------
// The verification gate
// ---------------------------------------------------------------------------

// TestBuildAndDeploy_TreeChangedSinceVerificationRefused asserts the
// "what was verified is what ships" rule: when the tree has moved since
// verification, the deploy is refused with the typed error and neither the
// build nor the target runs.
func TestBuildAndDeploy_TreeChangedSinceVerificationRefused(t *testing.T) {
	root := t.TempDir()
	runner := &stubRunner{}
	fp := &stubFingerprint{values: []string{"tree-now"}}
	target := NewFake()

	d := &Deployer{Target: target, Run: runner.run, Fingerprint: fp.fingerprint}

	_, err := d.BuildAndDeploy(context.Background(), buildReq(root), passingSnapshot("tree-at-verification"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTreeChanged)

	assert.Zero(t, runner.calls, "the build never runs when the tree moved")
	assert.Empty(t, target.Calls(), "nothing is uploaded when the tree moved")
}

// TestBuildAndDeploy_NoPassingVerificationRefused covers both shapes of "no
// passing verification": a snapshot that never passed (a nil verification
// result maps here) is refused with the typed error, before anything runs.
func TestBuildAndDeploy_NoPassingVerificationRefused(t *testing.T) {
	root := t.TempDir()
	runner := &stubRunner{}
	fp := &stubFingerprint{values: []string{"any"}}
	target := NewFake()

	d := &Deployer{Target: target, Run: runner.run, Fingerprint: fp.fingerprint}

	_, err := d.BuildAndDeploy(context.Background(), buildReq(root),
		VerificationSnapshot{Passed: false, Fingerprint: "any"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoPassingVerification)

	assert.Zero(t, runner.calls)
	assert.Zero(t, fp.calls, "the gate runs before the fingerprint is even read")
	assert.Empty(t, target.Calls(), "nothing is uploaded without a passing verification")
}

// ---------------------------------------------------------------------------
// Build failure
// ---------------------------------------------------------------------------

// TestBuildAndDeploy_BuildFailureUploadsNothing asserts a failing build is an
// error (wrapping the cause) and that the target is never called: a broken
// build must not ship the previous artifact or an empty one.
func TestBuildAndDeploy_BuildFailureUploadsNothing(t *testing.T) {
	root := t.TempDir()
	buildErr := errors.New("exit status 1")
	runner := &stubRunner{err: buildErr}
	fp := &stubFingerprint{values: []string{"same"}}
	target := NewFake()

	d := &Deployer{Target: target, Run: runner.run, Fingerprint: fp.fingerprint}

	_, err := d.BuildAndDeploy(context.Background(), buildReq(root), passingSnapshot("same"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBuildFailed)
	assert.ErrorIs(t, err, buildErr, "the underlying build error is preserved")

	assert.Equal(t, 1, runner.calls)
	assert.Empty(t, target.Calls(), "a failed build uploads nothing")
}

// ---------------------------------------------------------------------------
// Breaking the rule on purpose
// ---------------------------------------------------------------------------

// TestBuildAndDeploy_TreeChangedDuringBuildRefused is the rule-breaking test:
// verification passed for the tree, but the fingerprint differs after the
// build has run. The deploy is refused and nothing is uploaded, so the build's
// artifact never ships for an unverified tree.
func TestBuildAndDeploy_TreeChangedDuringBuildRefused(t *testing.T) {
	root := t.TempDir()
	runner := &stubRunner{}
	// First read matches the snapshot, second (after the build) does not.
	fp := &stubFingerprint{values: []string{"tree-at-verification", "tree-mutated-during-build"}}
	target := NewFake()

	d := &Deployer{Target: target, Run: runner.run, Fingerprint: fp.fingerprint}

	_, err := d.BuildAndDeploy(context.Background(), buildReq(root), passingSnapshot("tree-at-verification"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTreeChanged)

	assert.Equal(t, 1, runner.calls, "the build ran")
	assert.Empty(t, target.Calls(), "but nothing was uploaded for the moved tree")
}

// ---------------------------------------------------------------------------
// Misconfiguration and seam defaults
// ---------------------------------------------------------------------------

// TestBuildAndDeploy_NoBuildCommandRefused asserts an empty build command is
// refused: the manifest's command is the single source of the build and is
// never guessed.
func TestBuildAndDeploy_NoBuildCommandRefused(t *testing.T) {
	runner := &stubRunner{}
	fp := &stubFingerprint{values: []string{"same"}}
	target := NewFake()
	d := &Deployer{Target: target, Run: runner.run, Fingerprint: fp.fingerprint}

	req := buildReq(t.TempDir())
	req.Command = "  "
	_, err := d.BuildAndDeploy(context.Background(), req, passingSnapshot("same"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoBuildCommand)
	assert.Zero(t, runner.calls)
	assert.Empty(t, target.Calls())
}

// TestBuildAndDeploy_NoTargetRefused asserts a Deployer with no target is a
// construction error, refused before any gate or build work.
func TestBuildAndDeploy_NoTargetRefused(t *testing.T) {
	runner := &stubRunner{}
	d := &Deployer{Run: runner.run, Fingerprint: (&stubFingerprint{values: []string{"same"}}).fingerprint}

	_, err := d.BuildAndDeploy(context.Background(), buildReq(t.TempDir()), passingSnapshot("same"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoDeployTarget)
	assert.Zero(t, runner.calls)
}

// TestBuildAndDeploy_FingerprintErrorIsReported asserts a fingerprint failure
// is surfaced (not treated as a match), so a tree whose state cannot be read
// is never deployed.
func TestBuildAndDeploy_FingerprintErrorIsReported(t *testing.T) {
	fpErr := errors.New("walk failed")
	fp := &stubFingerprint{err: fpErr}
	runner := &stubRunner{}
	target := NewFake()
	d := &Deployer{Target: target, Run: runner.run, Fingerprint: fp.fingerprint}

	_, err := d.BuildAndDeploy(context.Background(), buildReq(t.TempDir()), passingSnapshot("same"))
	require.Error(t, err)
	assert.ErrorIs(t, err, fpErr)
	assert.Zero(t, runner.calls)
	assert.Empty(t, target.Calls())
}

// ---------------------------------------------------------------------------
// DefaultTreeFingerprint
// ---------------------------------------------------------------------------

// TestDefaultTreeFingerprint_ChangesWithTree asserts the default fingerprint is
// stable for an unchanged tree and changes when a file's content/size changes,
// which is the property the gate relies on.
func TestDefaultTreeFingerprint_ChangesWithTree(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("one"), 0o644))

	first, err := DefaultTreeFingerprint(root)
	require.NoError(t, err)
	second, err := DefaultTreeFingerprint(root)
	require.NoError(t, err)
	assert.Equal(t, first, second, "an unchanged tree fingerprints identically")

	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("a much longer body"), 0o644))
	third, err := DefaultTreeFingerprint(root)
	require.NoError(t, err)
	assert.NotEqual(t, first, third, "a changed file changes the fingerprint")
}

// TestDefaultTreeFingerprint_PrunesBuildOutput asserts build output and VCS
// directories are pruned: the build legitimately creates its output, so it
// must not read as a change to the verified source tree.
func TestDefaultTreeFingerprint_PrunesBuildOutput(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "src.txt"), []byte("source"), 0o644))

	before, err := DefaultTreeFingerprint(root)
	require.NoError(t, err)

	// Adding build output under a pruned directory must not move the
	// fingerprint.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "dist"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "dist", "index.html"), []byte("built"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "node_modules"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "node_modules", "dep.js"), []byte("x"), 0o644))

	after, err := DefaultTreeFingerprint(root)
	require.NoError(t, err)
	assert.Equal(t, before, after, "pruned directories do not affect the fingerprint")
}

// TestDefaultTreeFingerprint_NestedSourceDirNotPruned asserts the
// conventional-name prune is scoped to top-level directories: a nested source
// directory called "build" is fingerprinted like any other source, so it
// cannot hide a change from the gate.
func TestDefaultTreeFingerprint_NestedSourceDirNotPruned(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "pkg", "build")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "gen.go"), []byte("package build"), 0o644))

	before, err := DefaultTreeFingerprint(root)
	require.NoError(t, err)

	// A change to the nested source must move the fingerprint.
	require.NoError(t, os.WriteFile(filepath.Join(nested, "gen.go"), []byte("package build // changed and longer"), 0o644))
	after, err := DefaultTreeFingerprint(root)
	require.NoError(t, err)
	assert.NotEqual(t, before, after, "a nested 'build' source directory is still fingerprinted")
}

// TestDefaultTreeFingerprint_SkipsExplicitPath asserts a skip path prunes the
// subtree even when its name is not one of the conventional build-output
// names: the orchestrator passes the request's BuildDir, so an unconventional
// output directory is excluded by path.
func TestDefaultTreeFingerprint_SkipsExplicitPath(t *testing.T) {
	root := t.TempDir()
	outDir := filepath.Join(root, "site-bundle") // not in fingerprintSkipDirs
	require.NoError(t, os.WriteFile(filepath.Join(root, "src.txt"), []byte("source"), 0o644))

	before, err := DefaultTreeFingerprint(root, outDir)
	require.NoError(t, err)

	// Build output appears in the output dir; the fingerprint must not move.
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "index.html"), []byte("built"), 0o644))

	after, err := DefaultTreeFingerprint(root, outDir)
	require.NoError(t, err)
	assert.Equal(t, before, after, "the explicit output directory is excluded")

	// Sanity: without the skip path, the same output does move the
	// fingerprint — that is exactly what the orchestrator's skip prevents.
	noSkip, err := DefaultTreeFingerprint(root)
	require.NoError(t, err)
	assert.NotEqual(t, before, noSkip)
}

// TestBuildAndDeploy_BuildOutputDoesNotTripGate wires the real
// DefaultTreeFingerprint with a stub runner that writes the build output into
// req.BuildDir during the build. The deploy must succeed: the build's own
// artifact is not a tree change.
func TestBuildAndDeploy_BuildOutputDoesNotTripGate(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "src.txt"), []byte("source"), 0o644))
	req := buildReq(root)
	req.BuildDir = filepath.Join(root, "site-bundle") // unconventional name

	snap, err := DefaultTreeFingerprint(root, req.BuildDir)
	require.NoError(t, err)

	// A runner that produces output, as a real build would.
	runner := func(_ context.Context, _, _ string) error {
		if err := os.MkdirAll(req.BuildDir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(req.BuildDir, "index.html"), []byte("built"), 0o644)
	}
	target := NewFake()
	d := &Deployer{Target: target, Run: runner, Fingerprint: DefaultTreeFingerprint}

	got, err := d.BuildAndDeploy(context.Background(), req, passingSnapshot(snap))
	require.NoError(t, err, "the build's own output is not a tree change")
	assert.Equal(t, "web-app", got.Project)
	require.Len(t, target.Calls(), 1, "the built output was uploaded")
}

// TestBuildAndDeploy_SourceChangedDuringBuildRefusedWithRealFingerprint wires
// the real DefaultTreeFingerprint with a runner that edits a *source* file
// during the build. The post-build re-check must catch it and refuse, so a
// build that mutates the verified source tree never ships.
func TestBuildAndDeploy_SourceChangedDuringBuildRefusedWithRealFingerprint(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "src.txt"), []byte("source"), 0o644))

	req := buildReq(root)
	req.BuildDir = filepath.Join(root, "dist")

	snap, err := DefaultTreeFingerprint(root, req.BuildDir)
	require.NoError(t, err)

	runner := func(_ context.Context, _, _ string) error {
		// A build that rewrites the source tree — the rule-breaking case.
		return os.WriteFile(filepath.Join(root, "src.txt"), []byte("mutated during build"), 0o644)
	}
	target := NewFake()
	d := &Deployer{Target: target, Run: runner, Fingerprint: DefaultTreeFingerprint}

	_, err = d.BuildAndDeploy(context.Background(), req, passingSnapshot(snap))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTreeChanged)
	assert.Empty(t, target.Calls(), "nothing is uploaded when the source tree moved during the build")
}
