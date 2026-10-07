package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// qualityManifest is a starter manifest declaring a formatter and a linter.
const qualityManifest = `{
  "starter": {"id": "web-app", "version": "1.0.0"},
  "format": "make format",
  "lint": "make lint"
}`

// qualityManifestFormatOnly declares only a formatter: the linter is a gap the
// explicit configuration may fill.
const qualityManifestFormatOnly = `{
  "starter": {"id": "web-app", "version": "1.0.0"},
  "format": "make format"
}`

// TestQualitySnapshotFreezesCommands pins the frozen-input discipline: a
// snapshot captured before the manifest changes keeps the original commands
// across runs, so a mid-turn manifest edit cannot change what "clean" means.
func TestQualitySnapshotFreezesCommands(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, qualityManifest)

	exec := &fakeExecutor{}
	r := NewQualityRunner()
	r.Exec = exec

	snap := r.Snapshot(root)
	require.NotNil(t, snap)
	assert.Equal(t, "make format", snap.Commands.Format)
	assert.Equal(t, "make lint", snap.Commands.Lint)

	// Tamper with the manifest after the snapshot.
	writeManifestFile(t, root, `{"starter":{"id":"web-app","version":"1.0.0"},"format":"evil format","lint":"evil lint"}`)

	res, err := r.RunQualitySnapshot(context.Background(), root, snap)
	require.NoError(t, err)
	assert.Equal(t, "make format", res.Checks[0].Command, "the snapshot must freeze the formatter")
	assert.Equal(t, "make lint", res.Checks[1].Command, "the snapshot must freeze the linter")
	assert.Equal(t, []string{"make format", "make lint"}, exec.executed)
}

// TestQualityRunFormatThenLint pins the run order and the manifest resolution:
// the formatter runs first, then the linter, each with the manifest's command.
func TestQualityRunFormatThenLint(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, qualityManifest)

	exec := &fakeExecutor{}
	r := NewQualityRunner()
	r.Exec = exec

	res, err := r.RunQuality(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, res.Checks, 2)
	assert.Equal(t, QualityFormat, res.Checks[0].Kind)
	assert.Equal(t, "make format", res.Checks[0].Command)
	assert.True(t, res.Checks[0].Passed)
	assert.Equal(t, QualityLint, res.Checks[1].Kind)
	assert.Equal(t, "make lint", res.Checks[1].Command)
	assert.True(t, res.Checks[1].Passed)
	assert.Equal(t, []string{"make format", "make lint"}, exec.executed, "the formatter must run before the linter")
	assert.True(t, res.Passed())
	assert.False(t, res.Changed(), "a formatter that changed nothing reports no change")
}

// TestQualityConfigFillsManifestGap pins the trusted-source precedence: the
// manifest's command wins where set, the explicit configuration fills the gap.
func TestQualityConfigFillsManifestGap(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, qualityManifestFormatOnly)

	exec := &fakeExecutor{}
	r := NewQualityRunner()
	r.Exec = exec
	r.ConfigCommands = func(string) (QualityCommands, error) {
		return QualityCommands{Format: "config format", Lint: "config lint"}, nil
	}

	res, err := r.RunQuality(context.Background(), root)
	require.NoError(t, err)
	assert.Equal(t, "make format", res.Checks[0].Command, "the manifest's formatter wins")
	assert.Equal(t, "config lint", res.Checks[1].Command, "the configuration fills the linter gap")
}

// TestQualitySkipsStepsWithoutTrustedCommand pins that a step with no trusted
// command is skipped, never guessed, and does not fail the run.
func TestQualitySkipsStepsWithoutTrustedCommand(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, `{"starter":{"id":"web-app","version":"1.0.0"}}`)

	exec := &fakeExecutor{}
	r := NewQualityRunner()
	r.Exec = exec

	res, err := r.RunQuality(context.Background(), root)
	require.NoError(t, err)
	assert.Empty(t, exec.executed, "no command was configured, so nothing ran")
	require.Len(t, res.Checks, 2)
	for _, c := range res.Checks {
		assert.True(t, c.Skipped)
		assert.Contains(t, c.Reason, "no "+string(c.Kind)+" command available")
	}
	assert.False(t, res.Passed(), "an all-skipped run verified nothing")
	assert.False(t, res.Failed(), "an all-skipped run did not fail")
}

// TestQualityFormatterFailureDoesNotAbortLint pins that a failing formatter
// is reported but the linter still runs (and the run-level call never errors).
func TestQualityFormatterFailureDoesNotAbortLint(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, qualityManifest)

	exec := &fakeExecutor{results: map[string]Outcome{
		"make format": {Passed: false, Output: "formatter blew up\n"},
	}}
	r := NewQualityRunner()
	r.Exec = exec

	res, err := r.RunQuality(context.Background(), root)
	require.NoError(t, err, "a formatter failure is a finding, not a setup error")
	assert.False(t, res.Checks[0].Passed)
	assert.Equal(t, "formatter blew up\n", res.Checks[0].Excerpt)
	assert.True(t, res.Checks[1].Passed, "the linter still ran after the formatter failed")
	assert.Equal(t, []string{"make format", "make lint"}, exec.executed)
	assert.True(t, res.Failed())
}

// TestQualityFormatterChangeDetected pins the in-place-repair signal: when the
// formatter rewrites a file, the format check's Changed flag is set (an
// injected fingerprint makes the before/after comparison deterministic).
func TestQualityFormatterChangeDetected(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, qualityManifest)

	calls := 0
	r := NewQualityRunner()
	r.Exec = &fakeExecutor{results: map[string]Outcome{"make format": {Passed: true}}}
	r.Fingerprint = func(string) (workspaceFingerprint, error) {
		calls++
		if calls == 1 { // the formatter's "before" snapshot
			return workspaceFingerprint{"a.go": "before"}, nil
		}
		return workspaceFingerprint{"a.go": "after"}, nil
	}

	res, err := r.RunQuality(context.Background(), root)
	require.NoError(t, err)
	assert.True(t, res.Checks[0].Changed, "the formatter rewrote a file")
	assert.False(t, res.Checks[1].Changed, "only the formatter can report a change")
	assert.True(t, res.Changed())
}

// TestQualityCorruptManifestIsRunLevelFinding pins that a corrupt manifest is
// recorded as a run-level finding and the run proceeds with the configuration
// source, never swallowing the problem.
func TestQualityCorruptManifestIsRunLevelFinding(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, `{"starter": {`) // malformed JSON

	exec := &fakeExecutor{}
	r := NewQualityRunner()
	r.Exec = exec
	r.ConfigCommands = func(string) (QualityCommands, error) {
		return QualityCommands{Format: "config format", Lint: "config lint"}, nil
	}

	res, err := r.RunQuality(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, res.Errors, 1)
	assert.Contains(t, res.Errors[0], "starter manifest")
	assert.Equal(t, "config format", res.Checks[0].Command, "the configuration fills both gaps")
	assert.Equal(t, []string{"config format", "config lint"}, exec.executed)
}

// TestQualitySetupErrors pins the setup errors: a nil runner, a runner with no
// executor, and an empty root each return a non-nil error and no result.
func TestQualitySetupErrors(t *testing.T) {
	var nilRunner *QualityRunner
	if _, err := nilRunner.RunQuality(context.Background(), "/x"); err == nil {
		t.Error("nil runner: want a setup error")
	}
	r := &QualityRunner{}
	if _, err := r.RunQuality(context.Background(), "/x"); err == nil {
		t.Error("no executor: want a setup error")
	}
	r2 := NewQualityRunner()
	if _, err := r2.RunQuality(context.Background(), "  "); err == nil {
		t.Error("empty root: want a setup error")
	}
}

// TestDefaultFingerprintDetectsContentChange pins the default walk: a content
// change to an application file changes the fingerprint, and a skipped
// directory (a build output tree) is invisible to it.
func TestDefaultFingerprintDetectsContentChange(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "dist"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "dist", "bundle.js"), []byte("v1"), 0o644))

	before, err := defaultFingerprint(root)
	require.NoError(t, err)

	// A change in a skipped directory must not register.
	require.NoError(t, os.WriteFile(filepath.Join(root, "dist", "bundle.js"), []byte("v2"), 0o644))
	same, err := defaultFingerprint(root)
	require.NoError(t, err)
	assert.True(t, before.equal(same), "a change under a skipped directory is invisible")

	// A change in an application file must register.
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), []byte("package b\n"), 0o644))
	after, err := defaultFingerprint(root)
	require.NoError(t, err)
	assert.False(t, before.equal(after), "a content change must register")
}
