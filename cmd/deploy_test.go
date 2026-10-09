//go:build !js

// deploy_test.go — the `sprout deploy` CLI wiring: the command
// and its subcommands are registered, a missing deploy config is a clear
// actionable error, a preview deploy records a deployment through the fake
// target, an unconfirmed production deploy is refused (typed) without
// touching the target, and status/history/rollback read that recorded
// history back. The build and verification steps are injected through the
// package seams so no process runs and no real verification is required.
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/deploy"
	"github.com/sprout-foundry/sprout/pkg/deployconfig"
	"github.com/sprout-foundry/sprout/pkg/startermanifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetDeploySeams restores the command's flag globals and seams so one test
// cannot leak into the next (the repo's flag-globals convention, as in
// resetBenchmarkFlags).
func resetDeploySeams(t *testing.T) {
	t.Helper()
	savedDir, savedTarget := deployDir, deployTargetOverride
	savedProd, savedYes := deployProduction, deployAssumeYes
	savedTargetFor, savedRun := deployTargetFor, deployBuildRunner
	savedFp, savedSnap := deployFingerprint, deployVerificationSnapshot
	t.Cleanup(func() {
		// nil (not os.Stdout) so later tests that capture stdout see cobra
		// output: SetOut(os.Stdout) pins the *os.File of that moment.
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
		deployDir, deployTargetOverride = savedDir, savedTarget
		deployProduction, deployAssumeYes = savedProd, savedYes
		deployTargetFor, deployBuildRunner = savedTargetFor, savedRun
		deployFingerprint, deployVerificationSnapshot = savedFp, savedSnap
	})
	deployDir, deployTargetOverride = "", ""
	deployProduction, deployAssumeYes = false, false
	deployTargetFor, deployBuildRunner = nil, nil
	deployFingerprint, deployVerificationSnapshot = nil, nil
	if f := deployCmd.Flags().Lookup("help"); f != nil {
		_ = f.Value.Set("false")
	}
	for _, sub := range []*cobra.Command{deployStatusCmd, deployHistoryCmd, deployRollbackCmd} {
		if f := sub.Flags().Lookup("help"); f != nil {
			_ = f.Value.Set("false")
		}
	}
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)
	if f := deployCmd.Flags().Lookup("production"); f != nil {
		_ = f.Value.Set("false")
	}
	if f := deployCmd.Flags().Lookup("yes"); f != nil {
		_ = f.Value.Set("false")
	}
	if f := deployCmd.PersistentFlags().Lookup("target"); f != nil {
		_ = f.Value.Set("")
	}
	if f := deployCmd.PersistentFlags().Lookup("dir"); f != nil {
		_ = f.Value.Set("")
	}
}

// setupDeployProject creates a temp project with a valid deploy config and a
// starter manifest carrying a build command, returns its root, and points the
// command's --dir at it. The build runner, fingerprint and verification
// snapshot seams are stubbed so a deploy neither spawns a process nor needs a
// real verification run.
func setupDeployProject(t *testing.T) string {
	t.Helper()
	resetDeploySeams(t)

	root := t.TempDir()
	sproutDir := filepath.Join(root, startermanifest.SproutDir)
	require.NoError(t, os.MkdirAll(sproutDir, 0o755))

	cfg := deployconfig.DeployConfig{Target: "fake", Project: "fixture-app"}
	writeJSON(t, deployconfig.DeployConfigPath(root), cfg)

	manifest := map[string]any{
		"starter":      map[string]string{"id": "fixture", "version": "1.2.3"},
		"build":        "npm run build",
		"build_output": "dist",
	}
	writeJSON(t, filepath.Join(sproutDir, startermanifest.StarterJSONName), manifest)

	deployDir = root
	deployBuildRunner = func(_ context.Context, _, _ string) error { return nil }
	deployFingerprint = func(_ string, _ ...string) (string, error) { return "test-fingerprint", nil }
	deployVerificationSnapshot = func(_, _ string) (deploy.VerificationSnapshot, error) {
		return deploy.VerificationSnapshot{Passed: true, Fingerprint: "test-fingerprint"}, nil
	}
	return root
}

// writeJSON marshals v to path.
func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

// executeDeployCmd drives the shared root command with the deploy args and
// captures the command's output. Every deploy subcommand writes through
// cobra's OutOrStdout, which SetOut wires to the buffer.
func executeDeployCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(append([]string{"deploy"}, args...))
	err := rootCmd.Execute()
	return buf.String(), err
}

// TestDeployCmd_RegisteredWithSubcommandsAndGroup pins the reachability
// contract: the command resolves on the root command, carries its flags, has
// its three subcommands, and sits in a help group.
func TestDeployCmd_RegisteredWithSubcommandsAndGroup(t *testing.T) {
	applyCommandGroups(rootCmd)
	c, _, err := rootCmd.Find([]string{"deploy"})
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "deploy", c.Name())
	assert.False(t, c.Hidden)
	assert.Equal(t, "deploy", c.GroupID, "a visible top-level command must carry a help group")

	local := c.Flags()
	persistent := c.PersistentFlags()
	for _, flag := range []string{"production", "yes"} {
		assert.NotNil(t, local.Lookup(flag), "--%s must be registered", flag)
	}
	for _, flag := range []string{"dir", "target"} {
		assert.NotNil(t, persistent.Lookup(flag), "--%s must be registered", flag)
	}
	for _, name := range []string{"status", "history", "rollback"} {
		sub, _, err := rootCmd.Find([]string{"deploy", name})
		require.NoError(t, err)
		require.NotNil(t, sub, "deploy %s must be registered", name)
		assert.Equal(t, name, sub.Name())
	}
}

// TestDeployCmd_HelpResolves pins `sprout deploy --help` through the root.
func TestDeployCmd_HelpResolves(t *testing.T) {
	resetDeploySeams(t)
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"deploy", "--help"})
	require.NoError(t, rootCmd.Execute())

	out := buf.String()
	assert.Contains(t, out, "Build the current project")
	assert.Contains(t, out, "status")
	assert.Contains(t, out, "history")
	assert.Contains(t, out, "rollback")
	assert.Contains(t, out, "--production")
	assert.Contains(t, out, "--yes")
}

// TestDeployCmd_MissingConfigIsClearError pins the no-config path: a project
// without .sprout/deploy.json yields the deployconfig sentinel and an
// actionable hint naming the file to create.
func TestDeployCmd_MissingConfigIsClearError(t *testing.T) {
	resetDeploySeams(t)
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, startermanifest.SproutDir), 0o755))
	deployDir = root

	out, err := executeDeployCmd(t)
	require.Error(t, err)
	assert.True(t, errors.Is(err, deployconfig.ErrNoDeployConfig), "err = %v, want ErrNoDeployConfig", err)
	assert.Contains(t, err.Error(), "no deploy config found")

	he, ok := errors.AsType[*hintedError](err)
	require.True(t, ok, "the not-found error must carry an actionable hint")
	assert.Contains(t, he.hint, "deploy.json")
	_ = out
}

// TestDeployCmd_PreviewDeploysAgainstFake pins the happy path: a preview
// deploy builds and ships through the fake target, prints the preview URL,
// and records the deployment in the project's history for a later command to
// read back.
func TestDeployCmd_PreviewDeploysAgainstFake(t *testing.T) {
	root := setupDeployProject(t)

	out, err := executeDeployCmd(t)
	require.NoError(t, err)
	assert.Contains(t, out, "Deployed")
	assert.Contains(t, out, "preview")

	// The deployment is visible to a second invocation (persisted under the
	// project root), which is what makes status/history/rollback meaningful.
	target, err := newPersistentFakeTarget(root)
	require.NoError(t, err)
	history, err := target.List("fixture-app")
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, "fixture-app-1", history[0].ID)
	assert.Equal(t, deploy.KindPreview, history[0].Kind)
	assert.Equal(t, "1.2.3", history[0].Version)
	assert.Equal(t, deploy.StatusReady, history[0].Status)
}

// TestDeployCmd_ProductionWithoutConfirmationRefused pins the production
// rule at the CLI: --production without --yes is refused with the package's
// typed error and nothing is deployed.
func TestDeployCmd_ProductionWithoutConfirmationRefused(t *testing.T) {
	root := setupDeployProject(t)

	out, err := executeDeployCmd(t, "--production")
	require.Error(t, err)
	assert.True(t, errors.Is(err, deploy.ErrProductionNeedsConfirmation),
		"err = %v, want ErrProductionNeedsConfirmation", err)
	he, ok := errors.AsType[*hintedError](err)
	require.True(t, ok, "the refusal must tell the user how to confirm")
	assert.Contains(t, he.hint, "--yes")
	_ = out

	target, err := newPersistentFakeTarget(root)
	require.NoError(t, err)
	history, err := target.List("fixture-app")
	require.NoError(t, err)
	assert.Empty(t, history, "a refused production deploy must not touch the target")
}

// TestDeployCmd_ProductionWithYesDeploys pins the confirmation channel:
// --production --yes proceeds and records a production deployment.
func TestDeployCmd_ProductionWithYesDeploys(t *testing.T) {
	root := setupDeployProject(t)

	out, err := executeDeployCmd(t, "--production", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "production")

	target, err := newPersistentFakeTarget(root)
	require.NoError(t, err)
	history, err := target.List("fixture-app")
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, deploy.KindProduction, history[0].Kind)
}

// TestDeployCmd_StatusPrintsLatest pins `deploy status`: after a deploy it
// reports the latest deployment for the project.
func TestDeployCmd_StatusPrintsLatest(t *testing.T) {
	setupDeployProject(t)
	_, err := executeDeployCmd(t)
	require.NoError(t, err)

	out, err := executeDeployCmd(t, "status")
	require.NoError(t, err)
	assert.Contains(t, out, "Latest deployment")
	assert.Contains(t, out, "fixture-app-1")
	assert.Contains(t, out, "preview")
	assert.Contains(t, out, "ready")
}

// TestDeployCmd_StatusEmptyStatesCase pins the no-history path: status on a
// project with no deployments says so instead of failing.
func TestDeployCmd_StatusEmptyStatesCase(t *testing.T) {
	setupDeployProject(t)

	out, err := executeDeployCmd(t, "status")
	require.NoError(t, err)
	assert.Contains(t, out, "No deployments recorded")
}

// TestDeployCmd_HistoryListsDeployments pins `deploy history`: it lists every
// recorded deployment, oldest first.
func TestDeployCmd_HistoryListsDeployments(t *testing.T) {
	setupDeployProject(t)
	_, err := executeDeployCmd(t)
	require.NoError(t, err)
	_, err = executeDeployCmd(t)
	require.NoError(t, err)

	out, err := executeDeployCmd(t, "history")
	require.NoError(t, err)
	assert.Contains(t, out, "2 deployment(s)")
	assert.Contains(t, out, "fixture-app-1")
	assert.Contains(t, out, "fixture-app-2")
}

// TestDeployCmd_RollbackRestoresPrevious pins `deploy rollback <id>`: it
// rolls back to the preceding deployment and reports what is live again.
func TestDeployCmd_RollbackRestoresPrevious(t *testing.T) {
	setupDeployProject(t)
	_, err := executeDeployCmd(t)
	require.NoError(t, err)
	_, err = executeDeployCmd(t)
	require.NoError(t, err)

	out, err := executeDeployCmd(t, "rollback", "fixture-app-2")
	require.NoError(t, err)
	assert.Contains(t, out, "Rolled back")
	assert.Contains(t, out, "fixture-app-1")

	status, err := executeDeployCmd(t, "status")
	require.NoError(t, err)
	assert.Contains(t, status, "fixture-app-2")
	assert.Contains(t, status, "rolled_back")
}

// TestDeployCmd_RollbackNoPreviousIsClearError pins the nothing-to-revert
// path: rolling back the only deployment is refused with the typed error.
func TestDeployCmd_RollbackNoPreviousIsClearError(t *testing.T) {
	setupDeployProject(t)
	_, err := executeDeployCmd(t)
	require.NoError(t, err)

	_, err = executeDeployCmd(t, "rollback", "fixture-app-1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, deploy.ErrNoPreviousDeployment),
		"err = %v, want ErrNoPreviousDeployment", err)

	// The refused rollback leaves the single deployment live, not rolled back.
	status, err := executeDeployCmd(t, "status")
	require.NoError(t, err)
	assert.Contains(t, status, "ready")

	// An unknown id is likewise a typed, clear error.
	_, err = executeDeployCmd(t, "rollback", "no-such-deploy")
	require.Error(t, err)
	assert.True(t, errors.Is(err, deploy.ErrUnknownDeployment), "err = %v, want ErrUnknownDeployment", err)
}

// TestDeployCmd_StatusWorksWithoutBuildOutput pins that the read-only
// subcommands need only the deploy config: a project with no starter manifest
// still reports its (empty) history rather than failing on build resolution,
// which only the deploy path needs.
func TestDeployCmd_StatusWorksWithoutBuildOutput(t *testing.T) {
	resetDeploySeams(t)
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, startermanifest.SproutDir), 0o755))
	writeJSON(t, deployconfig.DeployConfigPath(root), deployconfig.DeployConfig{Target: "fake", Project: "fixture-app"})
	deployDir = root

	out, err := executeDeployCmd(t, "status")
	require.NoError(t, err)
	assert.Contains(t, out, "No deployments recorded")
}

// TestDeployAdapterID_Direct pins the resolution helper directly (no seam),
// so the real path is exercised: vendor selection, the Cloudflare shape from
// the manifest, the pages default, the override, and trimming of the vendor.
func TestDeployAdapterID_Direct(t *testing.T) {
	root := t.TempDir()
	sproutDir := filepath.Join(root, startermanifest.SproutDir)
	require.NoError(t, os.MkdirAll(sproutDir, 0o755))

	writeManifest := func(target string) {
		m := map[string]any{"starter": map[string]string{"id": "static-site", "version": "1.0.0"}}
		if target != "" {
			m["deploy_target"] = target
		}
		writeJSON(t, filepath.Join(sproutDir, startermanifest.StarterJSONName), m)
	}

	cases := []struct {
		name     string
		override string
		cfgTgt   string
		manifest string
		want     string
	}{
		{name: "cloudflare + workers manifest", cfgTgt: "cloudflare", manifest: "workers", want: "cloudflare/workers"},
		{name: "cloudflare + pages manifest", cfgTgt: "cloudflare", manifest: "pages", want: "cloudflare/pages"},
		{name: "cloudflare + absent manifest defaults pages", cfgTgt: "cloudflare", manifest: "", want: "cloudflare/pages"},
		{name: "cloudflare vendor is trimmed", cfgTgt: "  cloudflare  ", manifest: "workers", want: "cloudflare/workers"},
		{name: "non-cloudflare vendor passes through", cfgTgt: "fake", manifest: "workers", want: "fake"},
		{name: "override wins for a non-cloudflare vendor", override: "fake", cfgTgt: "cloudflare", manifest: "workers", want: "fake"},
		{name: "override of the vendor still uses the manifest shape", override: "cloudflare", cfgTgt: "fake", manifest: "workers", want: "cloudflare/workers"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetDeploySeams(t)
			writeManifest(tc.manifest)
			deployTargetOverride = tc.override
			got, err := deployAdapterID(root, &deployconfig.DeployConfig{Target: tc.cfgTgt, Project: "p"})
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestDeployTargetConstantsMatchPackages guards the two independent definitions
// of the deploy-target strings (the stdlib-only startermanifest cannot import
// pkg/deploy) against silent drift.
func TestDeployTargetConstantsMatchPackages(t *testing.T) {
	assert.Equal(t, startermanifest.DeployTargetPages, deploy.DeployTargetPages)
	assert.Equal(t, startermanifest.DeployTargetWorkers, deploy.DeployTargetWorkers)
}

// TestDeployCmd_CloudflareTargetResolvesAdapter pins the adapter seam: a
// config naming cloudflare resolves to the real adapter (not the old
// "not available yet" error). With no account id in the environment the
// adapter construction fails actionably, naming the variable to set.
func TestDeployCmd_CloudflareTargetResolvesAdapter(t *testing.T) {
	root := setupDeployProject(t)
	writeJSON(t, deployconfig.DeployConfigPath(root), deployconfig.DeployConfig{
		Target: "cloudflare", Project: "fixture-app",
	})
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "")

	_, err := executeDeployCmd(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CLOUDFLARE_ACCOUNT_ID")
	assert.NotContains(t, err.Error(), "not available yet")
}

// TestDeployCmd_UnknownTargetIsClearError pins the adapter seam: a config
// naming a target that has no adapter fails actionably, pointing at the fake.
func TestDeployCmd_UnknownTargetIsClearError(t *testing.T) {
	root := setupDeployProject(t)
	writeJSON(t, deployconfig.DeployConfigPath(root), deployconfig.DeployConfig{
		Target: "some-unknown-vendor", Project: "fixture-app",
	})

	_, err := executeDeployCmd(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
	assert.Contains(t, err.Error(), "--target fake")
}

// TestDeployCmd_ProductionRefusalIsTypedBeforeBuild pins that the
// confirmation gate runs before the build: a stubbed runner records whether
// it was invoked.
func TestDeployCmd_ProductionRefusalIsTypedBeforeBuild(t *testing.T) {
	setupDeployProject(t)
	built := false
	deployBuildRunner = func(_ context.Context, _, _ string) error {
		built = true
		return nil
	}

	_, err := executeDeployCmd(t, "--production")
	require.Error(t, err)
	assert.True(t, errors.Is(err, deploy.ErrProductionNeedsConfirmation))
	assert.False(t, built, "the build must not run for a refused production deploy")
}

// captureDeployAdapterID swaps the target seam to record the adapter id the
// command resolves, returning any error so the test can assert on routing
// without a real adapter.
func captureDeployAdapterID(t *testing.T) *string {
	t.Helper()
	var got string
	deployTargetFor = func(root, targetID string) (deploy.DeployTarget, error) {
		got = targetID
		return deploy.NewFake(), nil
	}
	return &got
}

// TestDeployCmd_ManifestDeployTargetSelectsAdapter pins the rule: when the
// deploy config's vendor is cloudflare, the starter manifest's deploy_target
// picks the Pages or Workers shape, and an absent value defaults to pages.
func TestDeployCmd_ManifestDeployTargetSelectsAdapter(t *testing.T) {
	cases := []struct {
		name       string
		manifest   map[string]any
		wantTarget string
	}{
		{
			name: "workers selects the workers adapter",
			manifest: map[string]any{
				"starter": map[string]string{"id": "web-app-data", "version": "1.2.3"},
				"build":   "npm run build", "build_output": "dist",
				"deploy_target": "workers",
			},
			wantTarget: "cloudflare/workers",
		},
		{
			name: "pages selects the pages adapter",
			manifest: map[string]any{
				"starter": map[string]string{"id": "static-site", "version": "1.2.3"},
				"build":   "npm run build", "build_output": "dist",
				"deploy_target": "pages",
			},
			wantTarget: "cloudflare/pages",
		},
		{
			name: "absent deploy_target defaults to pages",
			manifest: map[string]any{
				"starter": map[string]string{"id": "static-site", "version": "1.2.3"},
				"build":   "npm run build", "build_output": "dist",
			},
			wantTarget: "cloudflare/pages",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := setupDeployProject(t)
			writeJSON(t, deployconfig.DeployConfigPath(root), deployconfig.DeployConfig{
				Target: "cloudflare", Project: "fixture-app",
			})
			writeJSON(t, filepath.Join(root, startermanifest.SproutDir, startermanifest.StarterJSONName), tc.manifest)

			got := captureDeployAdapterID(t)
			out, err := executeDeployCmd(t)
			require.NoError(t, err, "output: %s", out)
			assert.Equal(t, tc.wantTarget, *got)
		})
	}
}

// TestDeployCmd_TargetOverrideWins pins the override: an explicit --target on
// the CLI wins over the config's vendor and the manifest's shape.
func TestDeployCmd_TargetOverrideWins(t *testing.T) {
	root := setupDeployProject(t)
	// The config names cloudflare and the manifest names workers; the
	// explicit --target fake must win outright.
	writeJSON(t, deployconfig.DeployConfigPath(root), deployconfig.DeployConfig{
		Target: "cloudflare", Project: "fixture-app",
	})
	writeJSON(t, filepath.Join(root, startermanifest.SproutDir, startermanifest.StarterJSONName), map[string]any{
		"starter": map[string]string{"id": "web-app-data", "version": "1.2.3"},
		"build":   "npm run build", "build_output": "dist",
		"deploy_target": "workers",
	})

	got := captureDeployAdapterID(t)
	out, err := executeDeployCmd(t, "--target", "fake")
	require.NoError(t, err, "output: %s", out)
	assert.Equal(t, "fake", *got, "--target must override the config and the manifest shape")
}

// TestDeployCmd_TargetOverrideOfVendorKeepsManifestShape pins the finer rule:
// --target cloudflare overrides the vendor but the manifest still picks the
// Pages-vs-Workers shape within it.
func TestDeployCmd_TargetOverrideOfVendorKeepsManifestShape(t *testing.T) {
	root := setupDeployProject(t)
	// The config names the fake target, but --target cloudflare overrides the
	// vendor; the manifest's workers shape then applies within cloudflare.
	writeJSON(t, filepath.Join(root, startermanifest.SproutDir, startermanifest.StarterJSONName), map[string]any{
		"starter": map[string]string{"id": "web-app-data", "version": "1.2.3"},
		"build":   "npm run build", "build_output": "dist",
		"deploy_target": "workers",
	})

	got := captureDeployAdapterID(t)
	out, err := executeDeployCmd(t, "--target", "cloudflare")
	require.NoError(t, err, "output: %s", out)
	assert.Equal(t, "cloudflare/workers", *got)
}

// TestDefaultDeployTargetFor_CloudflareConstructsAdapter pins the adapter
// construction: with an account id and a resolved token in the environment,
// the seam builds a Workers adapter without the token ever being an argument.
func TestDefaultDeployTargetFor_CloudflareConstructsAdapter(t *testing.T) {
	root := setupDeployProject(t)
	writeJSON(t, deployconfig.DeployConfigPath(root), deployconfig.DeployConfig{
		Target: "cloudflare", Project: "fixture-app",
	})
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct-1")
	t.Setenv(deploy.CloudflareAPITokenEnvVar, "tok-not-logged")

	target, err := defaultDeployTargetFor(root, "cloudflare/workers")
	require.NoError(t, err)
	require.NotNil(t, target)

	// The token was never printed through the target's string form.
	assert.NotContains(t, fmt.Sprintf("%v", target), "tok-not-logged")
}
