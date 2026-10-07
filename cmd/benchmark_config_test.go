package cmd

import (
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolateBenchmarkConfig gives the test an empty configuration with no
// provider credentials, so the result does not depend on the developer's
// own config, saved keys or provider API-key environment variables.
func isolateBenchmarkConfig(t *testing.T) {
	t.Helper()
	_, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	t.Setenv("SPROUT_CREDENTIAL_BACKEND", "file")
	for _, name := range configuration.KnownProviderNames() {
		meta, err := configuration.GetProviderAuthMetadata(name)
		if err == nil && meta.EnvVar != "" {
			t.Setenv(meta.EnvVar, "")
		}
	}
}

// With --models every entry names its provider, so a run needs no
// configured default provider: the failed runs still land in the report.
func TestBenchmarkCmd_ExplicitModelsNeedNoDefaultProvider(t *testing.T) {
	isolateBenchmarkConfig(t)
	suiteDir := filepath.Join(t.TempDir(), "suite")
	writeBenchmarkFixtureTask(t, suiteDir, "mystery-starter", "smoke-task")
	outDir := filepath.Join(t.TempDir(), "results")

	_, err := executeBenchmarkCmd(t,
		"--suite", suiteDir, "--output", outDir,
		"--models", "prov-x/model-x", "--runs", "1")
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(outDir, "report.md"))
}

// Without --models the suite runs the default model list, which needs a
// configured provider: the command refuses before any run starts.
func TestBenchmarkCmd_DefaultModelsWithoutProviderIsRefused(t *testing.T) {
	isolateBenchmarkConfig(t)
	suiteDir := filepath.Join(t.TempDir(), "suite")
	writeBenchmarkFixtureTask(t, suiteDir, "mystery-starter", "smoke-task")
	outDir := filepath.Join(t.TempDir(), "results")

	_, err := executeBenchmarkCmd(t, "--suite", suiteDir, "--output", outDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no provider configured for the benchmark")
	assert.NoFileExists(t, filepath.Join(outDir, "report.md"))
}
