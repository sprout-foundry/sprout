// Build-and-upload: build in the workspace, then hand the built output to a
// deploy target.
//
// A deploy is two steps: build the project in the workspace, then upload the
// built output. The build command comes from the starter manifest
// (pkg/startermanifest.Build) and the output directory from the resolved
// deploy config (pkg/deployconfig.Resolve); the target only ever receives the
// built directory — it never rebuilds.
//
// Two gates guard the path. The first is preview vs production: preview
// deploys may run automatically, while a production deploy is refused with
// ErrProductionNeedsConfirmation unless the caller supplies an explicit
// Confirmation (see confirmation.go). The second is "what was verified is
// what ships": VerificationSnapshot fingerprints the project tree at the
// moment verification passed, and BuildAndDeploy refuses with
// ErrNoPassingVerification (no passing result) or ErrTreeChanged (the tree
// moved since) before anything is built or uploaded. The fingerprint check
// runs again after the build, so a tree that changes while the build runs is
// refused too and nothing is uploaded.
//
// The orchestrator is deliberately pure: the build runner, the tree
// fingerprint, and the target are injected seams, so tests exercise every
// branch without spawning a process or touching a network. The defaults
// (DefaultBuildRunner, DefaultTreeFingerprint) are the real ones.

package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/utils/shellexec"
)

// ErrNoPassingVerification is returned by BuildAndDeploy when the supplied
// snapshot records no passing verification for the tree (a nil verification
// result, or one that failed). Nothing is built or uploaded.
var ErrNoPassingVerification = errors.New("deploy: no passing verification for the current tree")

// ErrTreeChanged is returned by BuildAndDeploy when the project tree's
// fingerprint no longer matches the one recorded at verification time. What
// was verified is what ships: a tree that moved since (or during) the build
// is refused, and nothing is uploaded.
var ErrTreeChanged = errors.New("deploy: tree changed since verification")

// ErrNoBuildCommand is returned by BuildAndDeploy when the request carries no
// build command. The manifest's build command is the single source of the
// build; the orchestrator never guesses one.
var ErrNoBuildCommand = errors.New("deploy: no build command configured")

// ErrBuildFailed is returned by BuildAndDeploy when the workspace build
// exits non-zero (or cannot be started). The failure is wrapped so the
// underlying cause is available through errors.Is/As; nothing is uploaded.
var ErrBuildFailed = errors.New("deploy: build failed")

// ErrNoDeployTarget is returned by BuildAndDeploy when no target is wired to
// the Deployer. A deploy with nowhere to go is a construction mistake, not a
// no-op.
var ErrNoDeployTarget = errors.New("deploy: no deploy target configured")

// VerificationSnapshot is the verification outcome for one tree state: did
// verification pass, and what did the tree look like at that moment.
//
// Fingerprint is the value returned by the TreeFingerprint seam over the
// project root when verification ran; BuildAndDeploy recomputes it and refuses
// when the two differ. Passed is verify.Result.Passed() — the caller builds it
// from the agent's last verification result, so this package stays independent
// of pkg/verify.
type VerificationSnapshot struct {
	// Passed reports whether verification passed on the tree. False means
	// there was no passing result (a nil result, or a failed run).
	Passed bool
	// Fingerprint is the tree fingerprint captured when verification passed.
	Fingerprint string
}

// BuildRequest is one build-and-upload: where to build, what command to run,
// and the built output to ship. Its fields are the resolved deploy config and
// starter manifest flattened to plain values, so this package depends on
// neither pkg/deployconfig nor pkg/starterstore (pkg/deployconfig imports this
// package, and the cycle must not form). The caller resolves the manifest and
// config (deployconfig.Resolve) and fills these in.
type BuildRequest struct {
	// Root is the absolute project root the build runs in. Required.
	Root string
	// Command is the starter manifest's build command (pkg/startermanifest
	// .Build). Required: a deploy with no build step has nothing to upload.
	Command string
	// BuildDir is the absolute directory the build produces its deployable
	// output in, resolved from the deploy config / manifest. Required; it is
	// handed to the target unchanged.
	BuildDir string
	// Project is the project name on the target. Required.
	Project string
	// Kind is preview or production. Empty is treated as preview by adapters.
	Kind DeploymentKind
	// Version identifies the build being shipped. Optional.
	Version string
}

// BuildRunner runs a build command in the project root and reports whether it
// succeeded. It is the injectable seam over the workspace build: the default
// spawns a shell, and tests supply a stub so no process runs. A non-nil error
// means the build failed.
type BuildRunner func(ctx context.Context, root, command string) error

// TreeFingerprint computes a stable fingerprint of the project tree at root.
// Two calls over an unchanged tree return the same value; any file added,
// removed, resized, or retouched changes it. It is the injectable seam over
// "did the tree move since verification": the default hashes the sorted file
// list, and tests supply a stub so no directory walk runs.
//
// skip names paths (relative to root or absolute) to leave out of the
// fingerprint. BuildAndDeploy always passes the request's BuildDir, because a
// build legitimately creates its own output: without excluding it, the
// post-build re-check would read the fresh artifact as a tree change. The gate
// therefore compares the source tree verification saw against the source tree
// the build ran over, not the artifact. A caller computing the snapshot at
// verification time must use the same function and skip set, or an unchanged
// tree can read as changed.
type TreeFingerprint func(root string, skip ...string) (string, error)

// Deployer orchestrates a build-and-upload: gate on confirmation and
// verification, build in the workspace, hand the built output to a target. Its
// three collaborators are fields so a caller (and a test) wires them
// independently; nil Run and Fingerprint fall back to the real defaults, while
// a nil Target is an error (there is nowhere to ship).
type Deployer struct {
	// Target receives the built output. Required.
	Target DeployTarget
	// Run builds the project in the workspace. Nil uses DefaultBuildRunner.
	Run BuildRunner
	// Fingerprint computes the tree fingerprint. Nil uses
	// DefaultTreeFingerprint.
	Fingerprint TreeFingerprint
}

// BuildAndDeploy builds the project in the workspace and uploads the built
// output through the target, gated first on the preview/production rule and
// then on a passing verification of the same tree. confirm is the caller's
// explicit user confirmation; it is required for a production deploy and
// ignored for a preview.
//
// The order is load-bearing — nothing is built or uploaded until every gate
// holds, and nothing is uploaded if the tree moves:
//
//  0. A production deploy without an explicit confirmation is refused with
//     ErrProductionNeedsConfirmation, before anything else runs. Preview
//     deploys proceed with no confirmation.
//  1. snap.Passed must be true, else ErrNoPassingVerification.
//  2. The request's root/command/build directory/project must be set, else a
//     plain error (ErrNoBuildCommand for a missing command).
//  3. The current fingerprint must equal snap.Fingerprint, else
//     ErrTreeChanged. The build has not run yet.
//  4. The build runs in req.Root with req.Command. A failure is ErrBuildFailed
//     wrapped with its cause, and the target is never called.
//  5. The fingerprint is checked again, so a tree that changed while the build
//     ran is ErrTreeChanged and the target is never called.
//  6. Only then is req.BuildDir handed to the target's Deploy, which uploads
//     it. The target is never asked to build.
func (d *Deployer) BuildAndDeploy(ctx context.Context, req BuildRequest, snap VerificationSnapshot, confirm Confirmation) (Deployment, error) {
	if d.Target == nil {
		return Deployment{}, ErrNoDeployTarget
	}
	// Confirmation is the first gate: a production deploy without an explicit
	// user confirmation is refused here, before verification is even read,
	// before a fingerprint, a build, or any call to the target. Preview
	// deploys are unaffected.
	if err := confirmProduction(req.Kind, confirm); err != nil {
		return Deployment{}, err
	}
	if !snap.Passed {
		return Deployment{}, ErrNoPassingVerification
	}

	root := strings.TrimSpace(req.Root)
	if root == "" {
		return Deployment{}, errors.New("deploy: project root must not be empty")
	}
	if strings.TrimSpace(req.Command) == "" {
		return Deployment{}, ErrNoBuildCommand
	}
	if strings.TrimSpace(req.BuildDir) == "" {
		return Deployment{}, errors.New("deploy: build directory must not be empty")
	}
	if strings.TrimSpace(req.Project) == "" {
		return Deployment{}, errors.New("deploy: project must not be empty")
	}

	run := d.Run
	if run == nil {
		run = DefaultBuildRunner
	}
	fingerprint := d.Fingerprint
	if fingerprint == nil {
		fingerprint = DefaultTreeFingerprint
	}

	// Gate before building: the tree must be the one verification passed on.
	// The build output directory is excluded, so it is the source tree that is
	// compared, not the artifact a build may have left behind.
	before, err := fingerprint(root, req.BuildDir)
	if err != nil {
		return Deployment{}, fmt.Errorf("deploy: fingerprint project tree: %w", err)
	}
	if before != snap.Fingerprint {
		return Deployment{}, ErrTreeChanged
	}

	// Build in the workspace, never on the target.
	if err := run(ctx, root, req.Command); err != nil {
		return Deployment{}, fmt.Errorf("%w: %w", ErrBuildFailed, err)
	}

	// Re-check after building: a tree that moved during the build is refused,
	// so what ships is only ever what was verified. The build's own output is
	// excluded, so creating the artifact is not mistaken for a change.
	after, err := fingerprint(root, req.BuildDir)
	if err != nil {
		return Deployment{}, fmt.Errorf("deploy: fingerprint project tree: %w", err)
	}
	if after != snap.Fingerprint {
		return Deployment{}, ErrTreeChanged
	}

	// Upload the already-built output. The target receives the directory; it
	// never rebuilds.
	return d.Target.Deploy(DeployRequest{
		Project:  req.Project,
		Kind:     req.Kind,
		BuildDir: req.BuildDir,
		Version:  req.Version,
	})
}

// DefaultBuildRunner runs a build command through the user's shell in the
// project root, streaming output to the process's stdout/stderr so progress
// is visible. It is the real BuildRunner; tests inject a stub instead.
func DefaultBuildRunner(ctx context.Context, root, command string) error {
	cmd := shellexec.CommandContext(ctx, command)
	cmd.Dir = root
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd.Run()
}

// fingerprintSkipDirs are directory names pruned from the default fingerprint
// walk in addition to the explicit skip paths: version-control, dependency,
// and conventional build-output directories. Build output is excluded on
// purpose — a build legitimately creates it, and the gate is about the source
// tree the developer verified, not the artifact the build just produced. The
// request's BuildDir is passed to DefaultTreeFingerprint explicitly (see
// BuildAndDeploy), so an unconventional output directory is pruned by path
// regardless of its name; this set is the extra safety net for the
// conventional names.
var fingerprintSkipDirs = map[string]bool{
	".git":         true,
	".hg":          true,
	".svn":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	"out":          true,
	"target":       true,
	"coverage":     true,
	".next":        true,
	".nuxt":        true,
	".output":      true,
	".venv":        true,
	"venv":         true,
	"__pycache__":  true,
}

// conventionalSkip reports whether the directory at path (whose base name is
// name, under root) is a conventional prune. The name match is restricted to a
// direct child of root, so a nested source directory that happens to be called
// "build" or "dist" is not silently dropped from the fingerprint everywhere in
// the tree — only the top-level output/state directories are conventional.
func conventionalSkip(path, root, name string) bool {
	if filepath.Dir(filepath.Clean(path)) != filepath.Clean(root) {
		return false
	}
	return fingerprintSkipDirs[name]
}

// DefaultTreeFingerprint computes a stable fingerprint of the project tree at
// root: a SHA-256 over the sorted list of relative file paths with each file's
// size and modification time. It is cheap (no file contents) and changes when
// a file is added, removed, resized, or retouched. It is size+mtime based, so
// a same-size edit inside one filesystem mtime tick is not distinguished; that
// is the deliberate cost of a cheap gate over the whole tree.
//
// A directory is pruned when it is one of the skip paths (tree-relative or
// absolute — BuildAndDeploy passes the build output directory here), or when
// it is a direct child of root whose name is in fingerprintSkipDirs. The
// name-based prune is scoped to direct children on purpose: only the
// top-level output/state directories are conventional, so a nested source
// directory that happens to be called "build" is still fingerprinted. A skip
// path that does not exist is ignored.
//
// It is the real TreeFingerprint; tests inject a stub so no walk runs. A root
// that cannot be read is an error.
func DefaultTreeFingerprint(root string, skip ...string) (string, error) {
	skipDirs := make(map[string]bool, len(skip))
	for _, s := range skip {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !filepath.IsAbs(s) {
			s = filepath.Join(root, s)
		}
		skipDirs[filepath.Clean(s)] = true
	}

	type entry struct {
		path string
		info fs.FileInfo
	}
	var files []entry

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (conventionalSkip(path, root, d.Name()) || skipDirs[filepath.Clean(path)]) {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, entry{path: filepath.ToSlash(rel), info: info})
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })

	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", f.path, f.info.Size(), f.info.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
