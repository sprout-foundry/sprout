// Package deployconfig owns the schema for the deploy config: the
// machine-readable document stored at .sprout/deploy.json that names where a
// project ships, plus the validator every reader must run.
//
// It is the file-system-facing half of deploys, mirroring
// pkg/startermanifest (+ pkg/starterstore): this package defines the on-disk
// shape, enforces its invariants, and resolves a config against a project
// root into a deploy.DeployRequest. The build output is deliberately NOT a
// required field of the config — the starter manifest's build_output is the
// single source of build output, and the config only overrides
// it when a project's deploy target needs a different directory.
//
// The package is a pure data + validation + resolution contract over the
// filesystem reads it needs: it reaches only for the standard library and
// pkg/startermanifest / pkg/starterstore / pkg/deploy, so the CLI, agent
// tools, and Ship mode all resolve a project's deploy config the same way.
//
// JSON field names are part of the on-disk contract and must stay in sync
// with the spec.
package deployconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/deploy"
	"github.com/sprout-foundry/sprout/pkg/startermanifest"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// On-disk location of the deploy config, under the project root. It lives
// in the same .sprout/ per-project state directory as the starter manifest
// (pkg/startermanifest.SproutDir).
// DeployJSONName is the on-disk file name of the deploy config, relative to
// the .sprout/ state directory (startermanifest.SproutDir).
const DeployJSONName = "deploy.json"

// ErrNoDeployConfig is returned by LoadDeployConfig when the project has no
// .sprout/deploy.json (or no .sprout directory at all). It is an expected,
// distinguishable not-found (not a failure): callers detect it with
// errors.Is(err, ErrNoDeployConfig) and treat it as "deploy is not configured
// for this project", exactly as starterstore.ErrNoManifest marks a missing
// starter manifest. Deploying is opt-in per project, so a
// missing file is the normal state rather than an error callers must
// special-case.
var ErrNoDeployConfig = errors.New("no deploy config found")

// DeployConfig is the in-memory form of .sprout/deploy.json.
// It names where a project ships: the target, the project name on that
// target, and — optionally — a build output directory that overrides the
// starter manifest's build_output.
//
// The config is deliberately small and lenient, matching the starter
// manifest's style: only the target and project name are required, and the
// build output is optional. Unknown JSON fields are tolerated (ignored), so
// a config written against a later schema still loads — the same choice the
// starter manifest validator makes.
type DeployConfig struct {
	// Target identifies the deploy target adapter to use (e.g. "cloudflare"
	// or a test adapter's id). Required.
	Target string `json:"target"`
	// Project is the project name on the target. Required: it is what the
	// target's history and rollback are keyed by.
	Project string `json:"project"`
	// BuildOutput overrides the starter manifest's build_output for this
	// project's deploy. Optional: when absent (or present-but-blank) the
	// manifest's build_output is the source of the build directory.
	// When present it must be a relative path that does not
	// escape the project root.
	BuildOutput string `json:"build_output,omitempty"`
}

// ValidationError describes the structural problems found in a deploy
// config. It carries one entry per problem so callers can surface them all
// at once (instead of fixing-and-revalidating one at a time), mirroring
// startermanifest.ValidationError. Use errors.As to recover the structured
// list from the error returned by Validate.
type ValidationError struct {
	Problems []string
}

// Error implements error. It always lists every problem so the message is
// actionable on its own.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("deployconfig: deploy config is invalid (%d problem(s)): %s",
		len(e.Problems), strings.Join(e.Problems, "; "))
}

// Validate reports every structural problem with c and returns a
// *ValidationError when the config is invalid, or nil when it is valid.
//
// It is a pure function over the in-memory struct and a project root, so it
// can be used both when building a new config and on every read of
// .sprout/deploy.json. root is the project root that build_output must stay
// under; pass the real root (an empty root skips the resolve-against-root
// re-check and should be avoided). What is enforced:
//
//   - target is required (non-blank);
//   - project is required (non-blank);
//   - build_output, if present, is not whitespace-only and is a relative
//     path that stays under root (no absolute path, no ".." segment that
//     escapes the project root).
//
// Unknown fields are tolerated rather than rejected: the decoder ignores
// them, matching the starter manifest validator's choice.
func Validate(c *DeployConfig, root string) error {
	if c == nil {
		return &ValidationError{Problems: []string{"deploy config is nil"}}
	}

	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if strings.TrimSpace(c.Target) == "" {
		add("target is required (the deploy target adapter to use)")
	}
	if strings.TrimSpace(c.Project) == "" {
		add("project is required (the project name on the target)")
	}

	if presentButBlank(c.BuildOutput) {
		add("build_output: must not be whitespace-only (omit the field to take the starter manifest's build_output)")
	} else if c.BuildOutput != "" {
		if err := validateBuildOutput(c.BuildOutput, root); err != nil {
			add("build_output: %s", err)
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: problems}
}

// validateBuildOutput checks that out is a relative path that stays under
// root: an absolute path, or a relative path whose cleaned form escapes root
// (e.g. "../dist" or "a/../../b"), is rejected. It reports a human-readable
// reason, or nil when the path is acceptable.
func validateBuildOutput(out, root string) error {
	if filepath.IsAbs(out) {
		return fmt.Errorf("%q must be a relative path (it is absolute)", out)
	}
	// Clean resolves "a/../b" and collapses redundant separators, so the
	// escape check sees the effective path. A cleaned path that is ".." or
	// starts with "../" climbs above the project root.
	cleaned := filepath.Clean(out)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%q must stay under the project root (it escapes with %q)", out, cleaned)
	}
	if root != "" {
		// Belt and braces: resolve both against root and confirm the result
		// is still root or below it. This catches platform-specific forms
		// Clean alone might miss.
		absRoot, err := filepath.Abs(root)
		if err == nil {
			absOut := filepath.Join(absRoot, cleaned)
			rel, relErr := filepath.Rel(absRoot, absOut)
			if relErr == nil && (rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
				return fmt.Errorf("%q must stay under the project root (it escapes with %q)", out, cleaned)
			}
		}
	}
	return nil
}

// validateJSON decodes a deploy config JSON document and validates the
// result against root. It returns the decoded config (non-nil when err is
// nil) and the first error encountered: either a JSON decode error or a
// *ValidationError.
func validateJSON(data []byte, root string) (*DeployConfig, error) {
	c := &DeployConfig{}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("deployconfig: invalid deploy JSON: %w", err)
	}
	if err := Validate(c, root); err != nil {
		return c, err
	}
	return c, nil
}

// DeployConfigPath returns the path to the project's .sprout/deploy.json for
// the given project root. It joins through startermanifest.SproutDir so the
// state directory name has a single home.
func DeployConfigPath(root string) string {
	return filepath.Join(root, startermanifest.SproutDir, DeployJSONName)
}

// LoadDeployConfig reads and validates the deploy config from
// .sprout/deploy.json under projectRoot.
//
//   - When the file does not exist (or .sprout/ does not exist),
//     LoadDeployConfig returns (nil, ErrNoDeployConfig) — the normal "deploy
//     is not configured" case.
//   - When the file is corrupt JSON or fails validation, LoadDeployConfig
//     returns (nil, err), where err wraps a JSON decode error or a
//     *ValidationError. An invalid config is a hard error, never a guessed
//     one.
//   - When the file is valid, LoadDeployConfig returns the validated config
//     and a nil error.
//
// On any error the returned config is nil, so a caller can never observe an
// invalid config through the loader.
func LoadDeployConfig(projectRoot string) (*DeployConfig, error) {
	path := DeployConfigPath(projectRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoDeployConfig
		}
		return nil, fmt.Errorf("read deploy config %s: %w", path, err)
	}

	c, err := validateJSON(data, projectRoot)
	if err != nil {
		return nil, fmt.Errorf("load deploy config %s: %w", path, err)
	}
	return c, nil
}

// Resolved is a deploy config merged with the starter manifest, ready to
// build a deploy.DeployRequest: the project name, the deployment kind
// (defaulting to preview), and the build directory resolved to an absolute
// path under the project root.
type Resolved struct {
	// Target is the deploy target adapter id from the config.
	Target string
	// Project is the project name on the target.
	Project string
	// Kind is the deployment kind. It defaults to deploy.KindPreview; a
	// caller promoting to production sets deploy.KindProduction explicitly.
	Kind deploy.DeploymentKind
	// BuildDir is the absolute build output directory to upload. It is the
	// resolved build output: the config's build_output when it overrides,
	// otherwise the starter manifest's build_output.
	BuildDir string
}

// Request returns the deploy.DeployRequest this resolved config describes,
// ready to hand to a deploy.DeployTarget. Version is left empty — the
// caller fills it from the starter version or plan revision it is shipping.
func (r Resolved) Request() deploy.DeployRequest {
	return deploy.DeployRequest{
		Project:  r.Project,
		Kind:     r.Kind,
		BuildDir: r.BuildDir,
	}
}

// Resolve merges a deploy config with the starter manifest under projectRoot
// and returns the resolved deploy shape. It is the "build output taken from
// the starter manifest" step: the build directory is the
// config's build_output when it overrides, and the starter manifest's
// build_output otherwise (the manifest is the single source of build output).
//
// It is a hard error when neither source names a build output: a deploy with
// nothing to upload is a misconfiguration, not an empty deploy. The config
// must already be valid (Validate); Resolve re-checks so a nil config or a
// blank target/project is never silently accepted.
//
// The returned BuildDir is absolute (resolved against projectRoot), so the
// caller passes the same directory the verification/build steps produced
// regardless of its own working directory.
//
// The returned Resolved always starts as a preview deployment; a caller
// promoting to production sets Resolved.Kind = deploy.KindProduction before
// calling Request() (production always needs explicit user confirmation).
func Resolve(c *DeployConfig, projectRoot string) (Resolved, error) {
	if err := Validate(c, projectRoot); err != nil {
		return Resolved{}, err
	}

	buildOutput := c.BuildOutput
	if strings.TrimSpace(buildOutput) == "" {
		// No override: the starter manifest is the source of build output.
		// A missing manifest is not fatal here — a project
		// may hand-author its build output in the deploy config instead —
		// but a manifest with no build_output cannot supply one.
		m, err := starterstore.LoadStarterManifest(projectRoot)
		if err != nil && !errors.Is(err, starterstore.ErrNoManifest) {
			return Resolved{}, fmt.Errorf("resolve deploy config: %w", err)
		}
		if err != nil {
			return Resolved{}, fmt.Errorf("resolve deploy config: no build output configured " +
				"(set build_output in .sprout/deploy.json or build_output in .sprout/starter.json)")
		}
		buildOutput = m.BuildOutput
		if strings.TrimSpace(buildOutput) == "" {
			return Resolved{}, fmt.Errorf("resolve deploy config: no build output configured "+
				"(the starter manifest %s has no build_output)", starterstore.StarterManifestPath(projectRoot))
		}
		if err := validateBuildOutput(buildOutput, projectRoot); err != nil {
			return Resolved{}, fmt.Errorf("resolve deploy config: starter manifest build_output: %w", err)
		}
	}

	absBuildDir := buildOutput
	if !filepath.IsAbs(absBuildDir) {
		absBuildDir = filepath.Join(projectRoot, absBuildDir)
	}

	return Resolved{
		Target:   c.Target,
		Project:  c.Project,
		Kind:     deploy.KindPreview,
		BuildDir: filepath.Clean(absBuildDir),
	}, nil
}

// presentButBlank reports whether s is present (non-empty) but carries no
// non-whitespace content. A zero-length string means "absent" and is allowed
// for the optional build_output override; only a present-but-blank value is a
// problem.
func presentButBlank(s string) bool {
	return s != "" && strings.TrimSpace(s) == ""
}
