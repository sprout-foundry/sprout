// quality.go — the quality-after-edits runner: after a turn that changed
// application code, the runtime runs the project's formatter and then its
// linter and reports a structured result. It is the sibling of the
// verification runner: the same trusted-command discipline, the same
// executor, the same bounded excerpts — a command comes only from the
// starter manifest or the explicit project configuration, never from model
// output.
//
// The runner is deliberately mechanical: it executes formatter then linter,
// records each check's outcome, and reports whether the formatter rewrote
// files (a runtime repair — the formatter fixed the formatting in place).
// The caller decides what to do with the findings; the turn-end hook feeds a
// failing linter back to the model the same way the verification repair loop
// does.

package verify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/startermanifest"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// QualityKind names one step of a quality run: the formatter or the linter.
type QualityKind string

const (
	// QualityFormat is the formatter step. A formatter may rewrite files in
	// place; a rewrite is the runtime's own repair.
	QualityFormat QualityKind = "format"
	// QualityLint is the linter step. It reports findings; a non-zero exit
	// is a failure the caller feeds back to the model.
	QualityLint QualityKind = "lint"
)

// QualityCommands are the trusted formatter and linter commands for one
// project's quality run. An empty field means "no command for that step" —
// the runner never guesses one.
type QualityCommands struct {
	// Format is the trusted formatter command, or "" when none is configured.
	Format string `json:"format,omitempty"`
	// Lint is the trusted linter command, or "" when none is configured.
	Lint string `json:"lint,omitempty"`
}

// QualityCheck is the outcome of one quality step.
type QualityCheck struct {
	// Kind is the step (format or lint).
	Kind QualityKind `json:"kind"`
	// Command is the trusted command that ran, or "" when the step was
	// skipped because no command was configured.
	Command string `json:"command,omitempty"`
	// Skipped marks a step that did not run (no command configured, or the
	// run was cancelled before it started). A skipped step does not gate the
	// result but is listed in it.
	Skipped bool `json:"skipped,omitempty"`
	// Passed reports whether the step passed. For a skipped step it is false.
	Passed bool `json:"passed,omitempty"`
	// Reason explains a skipped or abnormal step (timeout, executor error,
	// cancellation). Empty for a clean pass or a normal non-zero exit (the
	// excerpt carries the evidence there).
	Reason string `json:"reason,omitempty"`
	// Excerpt is a bounded excerpt of the step's combined output.
	Excerpt string `json:"excerpt,omitempty"`
	// Changed reports whether the step rewrote files in the workspace.
	// Reported only for the formatter, whose in-place rewrite is the
	// runtime's own repair (the linter is informational).
	Changed bool `json:"changed,omitempty"`
	// Duration is the wall time the step took to execute.
	Duration time.Duration `json:"duration,omitempty"`
}

// QualityResult is the structured outcome of one quality run.
type QualityResult struct {
	// Checks are the step outcomes, in run order (format then lint).
	Checks []QualityCheck `json:"checks"`
	// Errors are run-level findings (a corrupt manifest, a configuration
	// source failure) that prevented some commands from resolving. They are
	// reported, never swallowed.
	Errors []string `json:"errors,omitempty"`
}

// Failed reports whether the run failed: an executed step failed, or a
// run-level error was recorded. A run whose steps were all skipped did not
// fail — it checked nothing (see Passed).
func (r *QualityResult) Failed() bool {
	if r == nil {
		return false
	}
	if len(r.Errors) > 0 {
		return true
	}
	for _, c := range r.Checks {
		if !c.Skipped && !c.Passed {
			return true
		}
	}
	return false
}

// Passed reports whether the quality run passed: nothing failed and at least
// one step actually ran. An all-skipped run passed nothing.
func (r *QualityResult) Passed() bool {
	if r == nil {
		return false
	}
	if r.Failed() {
		return false
	}
	for _, c := range r.Checks {
		if !c.Skipped {
			return true
		}
	}
	return false
}

// Changed reports whether any step rewrote files (the formatter's in-place
// repair).
func (r *QualityResult) Changed() bool {
	if r == nil {
		return false
	}
	for _, c := range r.Checks {
		if c.Changed {
			return true
		}
	}
	return false
}

// Summary renders a compact, deterministic one-line summary of the run.
func (r *QualityResult) Summary() string {
	if r == nil {
		return "no quality result"
	}
	parts := make([]string, 0, len(r.Checks)+len(r.Errors))
	for _, c := range r.Checks {
		var status string
		switch {
		case c.Skipped:
			status = "skipped"
		case !c.Passed:
			status = "failed"
		default:
			status = "passed"
		}
		part := string(c.Kind) + ": " + status
		if c.Command != "" {
			part += " (" + c.Command + ")"
		}
		if c.Changed {
			part += " [files changed]"
		}
		if c.Reason != "" {
			part += " — " + c.Reason
		}
		parts = append(parts, part)
	}
	parts = append(parts, r.Errors...)
	return strings.Join(parts, "; ")
}

// QualityConfigCommandsProvider resolves a project's explicit formatter and
// linter commands — the trusted source a human sets in the project's
// configuration. Model output never reaches this provider.
type QualityConfigCommandsProvider func(root string) (QualityCommands, error)

// QualityRunner runs the formatter and linter of a quality-after-edits run.
//
// Every field is injectable so the run is testable without a shell; New()
// wires the production defaults. A nil field is treated as "absent" rather
// than panicking — but a QualityRunner without an executor cannot run
// anything.
type QualityRunner struct {
	// Manifest loads the project's starter manifest. Default:
	// starterstore.LoadStarterManifest (.sprout/starter.json).
	Manifest ManifestLoader
	// ConfigCommands resolves the project's explicit formatter/linter
	// commands. Nil means "no explicit configuration source".
	ConfigCommands QualityConfigCommandsProvider
	// Exec executes each resolved step command. Default: &ShellExecutor{}.
	Exec Executor
	// Timeout bounds a single step. Default: DefaultTimeout; a non-positive
	// value uses DefaultTimeout.
	Timeout time.Duration
	// MaxExcerptBytes bounds QualityCheck.Excerpt. Default:
	// DefaultMaxExcerptBytes.
	MaxExcerptBytes int
	// Fingerprint bounds the workspace change detection: it walks the root,
	// skipping a fixed set of heavy directories, and hashes each regular
	// file's content up to MaxFileBytes. Nil uses the default fingerprint
	// (defaultFingerprint). Injectable so a test can force a deterministic
	// before/after comparison.
	Fingerprint func(root string) (workspaceFingerprint, error)
}

// MaxProbeFileBytes caps the size of a file the fingerprint hashes: a large
// file (a binary, a bundle) is represented by its size only, so a quality run
// never reads a project's entire tree into memory.
const MaxProbeFileBytes = 1 << 20

// maxFingerprintFiles bounds how many files a fingerprint walks, so a
// pathological tree cannot stall a turn. The walk is deterministic
// (sorted), so a bounded run is stable across calls.
const maxFingerprintFiles = 20000

// qualitySkipDirs are the directories the fingerprint walk skips: build
// output, dependency trees, VCS metadata, and sprout's own state. The
// formatter never rewrites files there, and walking them would be expensive.
var qualitySkipDirs = map[string]bool{
	".git": true, ".sprout": true, "node_modules": true, "vendor": true,
	"dist": true, "build": true, "out": true, "target": true,
	".next": true, ".turbo": true, "__pycache__": true, ".venv": true,
	"venv": true,
}

// workspaceFingerprint is a content hash over a project's regular files: the
// signal that the formatter rewrote something. It is a map from root-relative
// path to the file's content hash so a before/after comparison is exact.
type workspaceFingerprint map[string]string

func (w workspaceFingerprint) equal(other workspaceFingerprint) bool {
	if len(w) != len(other) {
		return false
	}
	for path, sum := range w {
		if other[path] != sum {
			return false
		}
	}
	return true
}

// NewQualityRunner returns a QualityRunner with the production defaults: the
// starter manifest from the project's .sprout/ directory, the shell executor,
// and the default step and excerpt bounds. The caller wires the explicit
// project configuration source (QualityConfigurationCommands from a merged
// configuration.Config) when one exists.
func NewQualityRunner() *QualityRunner {
	return &QualityRunner{
		Manifest:        starterstore.LoadStarterManifest,
		Exec:            &ShellExecutor{},
		Timeout:         DefaultTimeout,
		MaxExcerptBytes: DefaultMaxExcerptBytes,
	}
}

// QualitySnapshot is the frozen input to a quality run: the trusted formatter
// and linter commands captured once, so every repair round of the turn
// executes the same commands even if a model edits .sprout/starter.json
// mid-turn. It mirrors the verification runner's snapshot discipline for the
// same reason: the model must not be able to change what "clean" means by
// editing the manifest during a repair round.
type QualitySnapshot struct {
	// Commands are the resolved formatter and linter commands.
	Commands QualityCommands
	// Errors are the run-level findings the resolution produced (a corrupt
	// manifest, a configuration source failure).
	Errors []string
}

// Snapshot resolves and freezes the project's quality commands. Call it once
// at the turn's start and pass the result to RunQualitySnapshot for every
// quality run of the turn. It is a no-op on a nil runner (returns nil):
// RunQualitySnapshot reports the setup error.
func (r *QualityRunner) Snapshot(root string) *QualitySnapshot {
	if r == nil {
		return nil
	}
	snap := &QualitySnapshot{}
	manifest, manifestErr := r.manifestLoad(root)
	if manifestErr != "" {
		snap.Errors = append(snap.Errors, manifestErr)
	}
	var configCommands QualityCommands
	if r.ConfigCommands != nil {
		c, err := r.ConfigCommands(root)
		if err != nil {
			snap.Errors = append(snap.Errors, "project configuration: "+err.Error())
		} else {
			configCommands = c
		}
	}
	snap.Commands = QualityCommands{
		Format: firstNonBlank(manifestFormat(manifest), configCommands.Format),
		Lint:   firstNonBlank(manifestLint(manifest), configCommands.Lint),
	}
	return snap
}

// RunQuality executes the project's formatter then its linter against freshly
// resolved commands (a snapshot captured immediately before the run) and
// returns the structured result. See RunQualitySnapshot for the behavior.
func (r *QualityRunner) RunQuality(ctx context.Context, root string) (*QualityResult, error) {
	if r == nil {
		return nil, errors.New("verify: nil quality runner")
	}
	return r.RunQualitySnapshot(ctx, root, r.Snapshot(root))
}

// RunQualitySnapshot executes the project's formatter then its linter against
// the frozen inputs a turn captured with Snapshot and returns the structured
// result. A step's command comes from the snapshot, never from a re-read of
// the manifest: a step with no trusted command is skipped (never guessed).
//
// The formatter runs first so its in-place rewrite is visible to the linter
// and to the checks the rest of the turn runs; a formatter that rewrote files
// sets the format check's Changed flag. A formatter failure never aborts the
// run — the linter still runs and both outcomes are reported.
//
// RunQualitySnapshot returns a non-nil error only for setup problems (no
// executor, empty root, nil runner, or a nil snapshot). Run-level findings (a
// corrupt manifest, a configuration source failure) are recorded from the
// snapshot on the result, never swallowed.
func (r *QualityRunner) RunQualitySnapshot(ctx context.Context, root string, snap *QualitySnapshot) (*QualityResult, error) {
	if r == nil {
		return nil, errors.New("verify: nil quality runner")
	}
	if r.Exec == nil {
		return nil, errors.New("verify: no executor configured (use NewQualityRunner() or set QualityRunner.Exec)")
	}
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("verify: project root is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if snap == nil {
		return nil, errors.New("verify: quality snapshot is required (call Snapshot first)")
	}

	result := &QualityResult{Checks: []QualityCheck{}}
	result.Errors = append(result.Errors, snap.Errors...)
	cmds := snap.Commands

	result.Checks = append(result.Checks,
		QualityCheck{Kind: QualityFormat, Command: cmds.Format},
		QualityCheck{Kind: QualityLint, Command: cmds.Lint},
	)

	for i := range result.Checks {
		if ctx.Err() != nil {
			result.Checks[i].Skipped = true
			result.Checks[i].Reason = "quality run cancelled before this step"
			continue
		}
		r.runQualityCheck(ctx, root, &result.Checks[i])
	}
	return result, nil
}

// manifestLoad loads the project's starter manifest and returns it with the
// run-level finding a corrupt manifest produces ("" when the manifest is
// present or missing). Missing is the normal "no starter" case, not a finding.
func (r *QualityRunner) manifestLoad(root string) (*startermanifest.StarterManifest, string) {
	if r.Manifest == nil {
		return nil, ""
	}
	manifest, err := r.Manifest(root)
	switch {
	case err == nil:
		return manifest, ""
	case errors.Is(err, starterstore.ErrNoManifest):
		return nil, ""
	default:
		return nil, "starter manifest: " + err.Error()
	}
}

// runQualityCheck executes one step and fills in its outcome. A step with no
// trusted command is skipped, not failed. The formatter's check records
// whether the step rewrote files, via a before/after workspace fingerprint.
func (r *QualityRunner) runQualityCheck(ctx context.Context, root string, c *QualityCheck) {
	if c.Command == "" {
		c.Skipped = true
		c.Reason = "no " + string(c.Kind) + " command available: set the starter manifest or the project's explicit quality configuration"
		return
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	before, beforeErr := r.fingerprint(root)
	start := time.Now()
	outcome, err := r.Exec.Run(runCtx, root, c.Command)
	c.Duration = time.Since(start)
	c.Excerpt = boundedExcerpt(outcome.Output, r.MaxExcerptBytes)
	after, afterErr := r.fingerprint(root)

	// Only the formatter reports a change (its in-place rewrite is the
	// runtime repair). A change is reported only when both fingerprints
	// succeeded and differ: a fingerprint error (an unreadable tree) must
	// never fabricate a change.
	if c.Kind == QualityFormat && beforeErr == nil && afterErr == nil {
		c.Changed = !before.equal(after)
	}

	if err != nil {
		c.Passed = false
		c.Reason = err.Error()
		return
	}
	c.Passed = outcome.Passed
	if !outcome.Passed && strings.TrimSpace(outcome.Reason) != "" {
		c.Reason = outcome.Reason
	}
}

// fingerprint returns the workspace content fingerprint the change detector
// compares before and after a step. It uses the runner's injectable
// Fingerprint when set, otherwise the default walk.
func (r *QualityRunner) fingerprint(root string) (workspaceFingerprint, error) {
	if r.Fingerprint != nil {
		return r.Fingerprint(root)
	}
	return defaultFingerprint(root)
}

// defaultFingerprint hashes the project's regular files, skipping heavy
// directories and capping each file's hashed bytes. It is deterministic: the
// walk is sorted, so the same tree always yields the same map.
func defaultFingerprint(root string) (workspaceFingerprint, error) {
	fp := make(workspaceFingerprint)
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable entry is skipped, never fatal
		}
		if d.IsDir() {
			if path != root && qualitySkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		paths = append(paths, rel)
		if len(paths) >= maxFingerprintFiles {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	for _, rel := range paths {
		sum, sumErr := hashFile(filepath.Join(root, rel))
		if sumErr != nil {
			continue // an unreadable file contributes nothing
		}
		fp[filepath.ToSlash(rel)] = sum
	}
	return fp, nil
}

// hashFile hashes at most MaxProbeFileBytes of a file's content, mixing in the
// file's size so a large file is represented by content-plus-size without
// being read whole.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, MaxProbeFileBytes)); err != nil {
		return "", err
	}
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if _, err := fmt.Fprintf(h, "|size=%d", info.Size()); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func manifestFormat(m *startermanifest.StarterManifest) string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m.Format)
}

func manifestLint(m *startermanifest.StarterManifest) string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m.Lint)
}
