package deployconfig

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/deploy"
	"github.com/sprout-foundry/sprout/pkg/startermanifest"
)

// fixtureConfig is a fully-populated, valid .sprout/deploy.json document.
const fixtureConfig = `{
  "target": "cloudflare",
  "project": "my-site",
  "build_output": "dist"
}`

// fixtureManifest is a starter manifest that names a build output, so the
// "build output taken from the starter manifest" fallback can be exercised.
const fixtureManifest = `{
  "starter": {"id": "static-site", "version": "1.0.0"},
  "build": "npm run build",
  "build_output": "build"
}`

// writeFile creates path (and its parents) with content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// writeConfig writes .sprout/deploy.json under root.
func writeConfig(t *testing.T, root, content string) {
	t.Helper()
	writeFile(t, DeployConfigPath(root), content)
}

// writeManifest writes .sprout/starter.json under root.
func writeManifest(t *testing.T, root, content string) {
	t.Helper()
	writeFile(t, filepath.Join(root, startermanifest.SproutDir, startermanifest.StarterJSONName), content)
}

// ---------------------------------------------------------------------------
// Loading
// ---------------------------------------------------------------------------

func TestLoadMissingDeployConfig(t *testing.T) {
	t.Run("no .sprout directory at all", func(t *testing.T) {
		root := t.TempDir()
		c, err := LoadDeployConfig(root)
		require.ErrorIs(t, err, ErrNoDeployConfig, "a missing .sprout dir must surface the ErrNoDeployConfig sentinel")
		assert.Nil(t, c, "no config may be returned when the file is absent")
	})

	t.Run(".sprout exists but deploy.json does not", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, startermanifest.SproutDir), 0o755))

		c, err := LoadDeployConfig(root)
		require.ErrorIs(t, err, ErrNoDeployConfig, "a missing file must surface the ErrNoDeployConfig sentinel")
		assert.Nil(t, c)
	})
}

func TestLoadValidDeployConfig(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, fixtureConfig)

	c, err := LoadDeployConfig(root)
	require.NoError(t, err, "a valid config on disk must load")
	require.NotNil(t, c)

	assert.Equal(t, "cloudflare", c.Target)
	assert.Equal(t, "my-site", c.Project)
	assert.Equal(t, "dist", c.BuildOutput)
}

// TestLoadDeployConfigToleratesUnknownFields pins the lenient decoder
// choice (matching the starter manifest validator): a config written against
// a later schema still loads, with the unknown fields ignored.
func TestLoadDeployConfigToleratesUnknownFields(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{"target": "cloudflare", "project": "my-site", "future_field": {"a": 1}}`)

	c, err := LoadDeployConfig(root)
	require.NoError(t, err, "unknown fields must be tolerated")
	require.NotNil(t, c)
	assert.Equal(t, "cloudflare", c.Target)
	assert.Equal(t, "my-site", c.Project)
}

func TestLoadCorruptJSON(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "{ not valid json ")

	c, err := LoadDeployConfig(root)
	require.Error(t, err, "corrupt JSON must not load")
	assert.Nil(t, c, "a corrupt file must not return a config")
	assert.False(t, errors.Is(err, ErrNoDeployConfig), "corrupt is a different error than missing")
	assert.Contains(t, err.Error(), "invalid deploy JSON", "the decode failure must be surfaced: %v", err)
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func TestValidateRequiresTarget(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{"project": "my-site"}`)

	c, err := LoadDeployConfig(root)
	require.Error(t, err, "a config without a target must not load")
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "target is required")

	var ve *ValidationError
	require.ErrorAs(t, err, &ve, "the structured ValidationError must be recoverable")
}

func TestValidateRequiresProject(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{"target": "cloudflare"}`)

	c, err := LoadDeployConfig(root)
	require.Error(t, err, "a config without a project must not load")
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "project is required")
}

func TestValidateReportsAllProblems(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{"target": " ", "project": ""}`)

	_, err := LoadDeployConfig(root)
	require.Error(t, err)

	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Len(t, ve.Problems, 2, "both problems are reported at once: %v", ve.Problems)
}

func TestValidateRejectsUnsafeBuildOutput(t *testing.T) {
	cases := []struct {
		name string
		out  string
	}{
		{"absolute path", filepath.Join(string(filepath.Separator), "tmp", "dist")},
		{"parent escape", "../dist"},
		{"nested parent escape", "a/../../dist"},
		{"bare parent", ".."},
		{"whitespace-only", "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeConfig(t, root, `{"target": "cf", "project": "p", "build_output": `+jsonString(tc.out)+`}`)

			c, err := LoadDeployConfig(root)
			require.Error(t, err, "an unsafe build_output must be rejected")
			assert.Nil(t, c)
			assert.Contains(t, err.Error(), "build_output")
		})
	}
}

func TestValidateAcceptsNestedRelativeBuildOutput(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{"target": "cf", "project": "p", "build_output": "packages/app/dist"}`)

	c, err := LoadDeployConfig(root)
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "packages/app/dist", c.BuildOutput)
}

// ---------------------------------------------------------------------------
// Resolve: config + starter manifest → deploy-ready shape
// ---------------------------------------------------------------------------

// TestResolveFallsBackToManifestBuildOutput pins the contract that, with no
// build_output override in the deploy config, the
// build directory comes from the starter manifest.
func TestResolveFallsBackToManifestBuildOutput(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{"target": "cloudflare", "project": "my-site"}`)
	writeManifest(t, root, fixtureManifest)

	c, err := LoadDeployConfig(root)
	require.NoError(t, err)

	r, err := Resolve(c, root)
	require.NoError(t, err, "a manifest-supplied build output resolves")

	assert.Equal(t, "cloudflare", r.Target)
	assert.Equal(t, "my-site", r.Project)
	assert.Equal(t, deploy.KindPreview, r.Kind, "the kind defaults to preview")
	assert.Equal(t, filepath.Join(root, "build"), r.BuildDir, "the build dir is the manifest's build_output, made absolute")
}

// TestResolveConfigOverridesManifest pins that an explicit build_output in
// the deploy config wins over the starter manifest.
func TestResolveConfigOverridesManifest(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, fixtureConfig) // build_output: dist
	writeManifest(t, root, fixtureManifest)

	c, err := LoadDeployConfig(root)
	require.NoError(t, err)

	r, err := Resolve(c, root)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "dist"), r.BuildDir, "the config's build_output overrides the manifest's")
}

// TestResolveRequestMatchesResolved checks the Resolved → DeployRequest
// handoff keeps project, kind, and the resolved absolute build dir.
func TestResolveRequestMatchesResolved(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{"target": "cloudflare", "project": "my-site", "build_output": "dist"}`)

	c, err := LoadDeployConfig(root)
	require.NoError(t, err)
	r, err := Resolve(c, root)
	require.NoError(t, err)

	req := r.Request()
	assert.Equal(t, "my-site", req.Project)
	assert.Equal(t, deploy.KindPreview, req.Kind)
	assert.Equal(t, filepath.Join(root, "dist"), req.BuildDir)
}

// TestResolveNoBuildOutputAnywhere pins that a deploy with no build output
// in either source is a hard error, not an empty deploy.
func TestResolveNoBuildOutputAnywhere(t *testing.T) {
	t.Run("manifest present but without build_output", func(t *testing.T) {
		root := t.TempDir()
		writeConfig(t, root, `{"target": "cloudflare", "project": "my-site"}`)
		writeManifest(t, root, `{"starter": {"id": "static-site", "version": "1.0.0"}}`)

		c, err := LoadDeployConfig(root)
		require.NoError(t, err)

		_, err = Resolve(c, root)
		require.Error(t, err, "no build output in either source must fail")
		assert.Contains(t, err.Error(), "no build output configured")
	})

	t.Run("no manifest at all", func(t *testing.T) {
		root := t.TempDir()
		writeConfig(t, root, `{"target": "cloudflare", "project": "my-site"}`)

		c, err := LoadDeployConfig(root)
		require.NoError(t, err)

		_, err = Resolve(c, root)
		require.Error(t, err, "a missing manifest with no override must fail")
		assert.Contains(t, err.Error(), "no build output configured")
	})
}

// TestResolveRejectsInvalidConfig pins that Resolve re-validates, so a nil
// or invalid config never slips through into a request.
func TestResolveRejectsInvalidConfig(t *testing.T) {
	root := t.TempDir()

	_, err := Resolve(nil, root)
	require.Error(t, err)

	_, err = Resolve(&DeployConfig{Project: "p"}, root) // no target
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target is required")
}

// TestDeployConfigCarriesNoCredential pins that the on-disk config and the
// resolved shape never name or carry a credential. A token belongs in the
// credential store (or the embedding environment), not in .sprout/deploy.json
// or anything derived from it, so the marshalled document must not grow a
// credential field.
func TestDeployConfigCarriesNoCredential(t *testing.T) {
	c := DeployConfig{Target: "cloudflare", Project: "my-site", BuildOutput: "dist"}
	raw, err := json.Marshal(c)
	require.NoError(t, err)

	lower := strings.ToLower(string(raw))
	for _, forbidden := range []string{"token", "api_key", "apikey", "secret", "password", "credential"} {
		assert.NotContains(t, lower, forbidden, "DeployConfig JSON must not name a credential field (%q)", forbidden)
	}
}

// jsonString quotes s as a JSON string, so a case table can build a document
// with arbitrary path characters (e.g. a backslash separator on Windows).
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err) // a plain string always marshals
	}
	return string(b)
}
