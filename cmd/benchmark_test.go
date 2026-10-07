//go:build !js

// benchmark_test.go — the `sprout benchmark` CLI wiring: the command is
// registered on the root command with its flags, invocation mistakes are
// usage errors, and a full run writes report.md + report.json from the
// harness's runs (a run's own failure rides on the report; the command
// still exits zero).
//
// The end-to-end test never touches a provider: its fixture task names a
// starter that no starter catalogue carries, so every run fails at the
// fresh-copy step — before any agent is built — and the report records
// those failed runs. That is the record-and-continue contract the report
// path must demonstrate. The flags-to-runner wiring is pinned through the
// benchmarkRunnerFor seam; the runner's own behavior is pkg/benchmark's
// (see runner_timeout_test.go and runner_test.go).
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/benchmark"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keepSeam holds the test-installed benchmarkRunnerFor across
// resetBenchmarkFlags (which must still clear the flag globals it owns).
var keepSeam func(suiteDir string, models []benchmark.ModelSpec, runs int, timeout time.Duration) *benchmark.Runner

// resetBenchmarkFlags restores the command's flag globals so one test's
// parse cannot leak into the next (the repo's flag-globals convention, as
// in resetAutomateGlobals). It also clears the cobra-generated help and
// suite flags: pflag keeps parsed values on the shared FlagSet across
// Execute calls, so a prior execution's values would leak into this one.
func resetBenchmarkFlags(t *testing.T) {
	t.Helper()
	savedSuite, savedModels, savedOut := benchmarkSuiteDir, benchmarkModels, benchmarkOutDir
	savedRuns, savedTimeout := benchmarkRuns, benchmarkTimeout
	savedRunnerFor := benchmarkRunnerFor
	t.Cleanup(func() {
		benchmarkSuiteDir, benchmarkModels, benchmarkOutDir = savedSuite, savedModels, savedOut
		benchmarkRuns, benchmarkTimeout = savedRuns, savedTimeout
		benchmarkRunnerFor = savedRunnerFor
	})
	benchmarkSuiteDir, benchmarkModels, benchmarkOutDir = "", "", ""
	benchmarkRuns, benchmarkTimeout = 0, 0
	benchmarkRunnerFor = nil
	keepSeam = savedRunnerFor
	if f := benchmarkCmd.Flags().Lookup("help"); f != nil {
		_ = f.Value.Set("false")
	}
	if f := benchmarkCmd.Flags().Lookup("suite"); f != nil {
		_ = f.Value.Set("")
	}
}

// writeBenchmarkFixtureTask writes one valid task fixture into the suite
// layout (<suite>/<starter-id>/<task-id>.json). The starter name is the
// caller's choice: the CLI tests use one that no starter catalogue carries,
// so runs fail at the fresh-copy step without any agent or provider.
func writeBenchmarkFixtureTask(t *testing.T, suiteDir, starterID, taskID string) string {
	t.Helper()
	plan := plancontract.New("Add the badge so the page identifies itself.", time.Now())
	plan.Starter = starterID
	plan.Scope = []plancontract.ScopeItem{{ID: "badge", Title: "Badge"}}
	plan.Steps = []plancontract.Step{{Scope: "badge", Description: "Add the badge."}}
	plan.Acceptance = []plancontract.Acceptance{
		{ID: "build-passes", Scope: "badge", Check: "npm run build", Kind: plancontract.KindBuild},
	}
	task := map[string]any{
		"id":      taskID,
		"request": "Add a small badge under the page heading.",
		"starter": starterID,
		"plan":    plan,
	}
	data, err := json.Marshal(task)
	require.NoError(t, err)
	dir := filepath.Join(suiteDir, starterID)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, taskID+".json")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

// executeBenchmarkCmd runs the registered root command with the benchmark
// subcommand and captures stdout (the summary lines). It resets the flag
// globals and captures stdout around the run, mirroring the
// executeAutomateCmd helper in automate_flags_test.go.
func executeBenchmarkCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetBenchmarkFlags(t)
	return executeBenchmarkRaw(t, args...)
}

// executeBenchmarkCmdKeepSeam is executeBenchmarkCmd for tests that install
// the benchmarkRunnerFor seam first and need it to survive the call.
func executeBenchmarkCmdKeepSeam(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetBenchmarkFlags(t)
	benchmarkRunnerFor = keepSeam
	defer func() { benchmarkRunnerFor = nil }()
	return executeBenchmarkRaw(t, args...)
}

// executeBenchmarkRaw drives the shared root command with captured stdout.
func executeBenchmarkRaw(t *testing.T, args ...string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	cap := captureAutomateStdout(buf)
	rootCmd.SetArgs(append([]string{"benchmark"}, args...))
	err := rootCmd.Execute()
	cap.Restore()
	return buf.String(), err
}

// executeBenchmarkRawContext is executeBenchmarkRaw for the stopped-early
// path: the command runs on the caller's context, so cancelling it stops
// the suite between pairs exactly as an interrupt would.
func executeBenchmarkRawContext(t *testing.T, ctx context.Context, args ...string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	cap := captureAutomateStdout(buf)
	rootCmd.SetArgs(append([]string{"benchmark"}, args...))
	err := rootCmd.ExecuteContext(ctx)
	cap.Restore()
	return buf.String(), err
}

// TestBenchmarkCmd_RegisteredWithFlagsAndGroup pins the reachability
// contract: the command resolves on the root command, carries its flags,
// and sits in a help group (a visible top-level command without one would
// regrow the help junk drawer).
func TestBenchmarkCmd_RegisteredWithFlagsAndGroup(t *testing.T) {
	applyCommandGroups(rootCmd)
	c, _, err := rootCmd.Find([]string{"benchmark"})
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "benchmark", c.Name())
	assert.False(t, c.Hidden, "benchmark is a person-typed command, not plumbing")
	assert.Equal(t, "benchmark", c.GroupID, "a visible top-level command must carry a help group")
	for _, flag := range []string{"suite", "models", "output", "runs", "timeout"} {
		assert.NotNil(t, c.Flags().Lookup(flag), "--%s must be registered", flag)
	}
}

// TestBenchmarkCmd_HelpResolvesViaRoot pins the CLI wiring the way a user
// first meets it: `sprout benchmark --help` resolves through the root
// command and describes the command's cost warning and its flags.
func TestBenchmarkCmd_HelpResolvesViaRoot(t *testing.T) {
	resetBenchmarkFlags(t)
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"benchmark", "--help"})
	require.NoError(t, rootCmd.Execute())

	out := buf.String()
	assert.Contains(t, out, "Run the agent task benchmark")
	assert.Contains(t, out, "network and money", "the help must be explicit that real providers are called")
	for _, flag := range []string{"--suite", "--models", "--output", "--runs", "--timeout"} {
		assert.Contains(t, out, flag)
	}
}

// TestBenchmarkCmd_MissingSuiteDirIsUsageError pins the suite-level error
// path: a suite dir that cannot be read exits non-zero as a usage error,
// with the directory named.
func TestBenchmarkCmd_MissingSuiteDirIsUsageError(t *testing.T) {
	_, err := executeBenchmarkCmd(t, "--suite", filepath.Join(t.TempDir(), "no-such-suite"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load benchmark suite")
	assert.Contains(t, err.Error(), "no-such-suite")
	assert.Equal(t, exitUsage, exitCodeFor(err))
}

// TestBenchmarkCmd_EmptySuiteIsUsageError pins the no-tasks path: a suite
// dir that exists but holds no task fixtures is an invocation error that
// says where tasks live.
func TestBenchmarkCmd_EmptySuiteIsUsageError(t *testing.T) {
	_, err := executeBenchmarkCmd(t, "--suite", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no benchmark tasks found")
	assert.Equal(t, exitUsage, exitCodeFor(err))
}

// TestBenchmarkCmd_BadModelsIsUsageError pins --models parsing: malformed
// entries (a provider with no model) are rejected as an invocation error.
func TestBenchmarkCmd_BadModelsIsUsageError(t *testing.T) {
	_, err := executeBenchmarkCmd(t, "--suite", t.TempDir(), "--models", "openai/,prov-x/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad --models")
	assert.Equal(t, exitUsage, exitCodeFor(err))
}

// TestBenchmarkCmd_WritesReportsFromFailedRuns runs the real command end to
// end against a fixture suite whose starter does not exist: every run fails
// at the fresh-copy step (no agent, no provider, no network), the command
// still exits zero, and report.md + report.json land in --output recording
// the failed runs.
func TestBenchmarkCmd_WritesReportsFromFailedRuns(t *testing.T) {
	suiteDir := filepath.Join(t.TempDir(), "suite")
	writeBenchmarkFixtureTask(t, suiteDir, "mystery-starter", "smoke-task")
	outDir := filepath.Join(t.TempDir(), "results")

	stdout, err := executeBenchmarkCmd(t,
		"--suite", suiteDir, "--output", outDir,
		"--models", "prov-x/model-x", "--runs", "2")
	require.NoError(t, err, "a run's own failure rides on the report — the command exits zero")

	md, err := os.ReadFile(filepath.Join(outDir, "report.md"))
	require.NoError(t, err)
	assert.Contains(t, string(md), "# Agent benchmark —")
	assert.Contains(t, string(md), "smoke-task")
	assert.Contains(t, string(md), "prov-x/model-x")
	assert.Contains(t, string(md), "error", "the failed runs land in the failure categories")

	data, err := os.ReadFile(filepath.Join(outDir, "report.json"))
	require.NoError(t, err)
	var rep map[string]any
	require.NoError(t, json.Unmarshal(data, &rep))

	models, ok := rep["models"].([]any)
	require.True(t, ok, "models = %v, want the one --models entry", rep["models"])
	require.Len(t, models, 1)
	m0 := models[0].(map[string]any)
	assert.Equal(t, "prov-x", m0["Provider"])
	assert.Equal(t, "model-x", m0["Model"])

	tasks, ok := rep["tasks"].([]any)
	require.True(t, ok)
	require.Len(t, tasks, 1)
	task := tasks[0].(map[string]any)
	assert.Equal(t, "smoke-task", task["TaskID"])
	runs, ok := task["Runs"].([]any)
	require.True(t, ok)
	require.Len(t, runs, 2, "--runs 2 must reach the runner")
	for _, r := range runs {
		run := r.(map[string]any)
		assert.Equal(t, false, run["Passed"], "a run that never instantiated its starter is never passed")
		assert.NotNil(t, run["Err"], "the fresh-copy failure is recorded on the run")
	}

	cats, ok := rep["failure_categories"].(map[string]any)
	require.True(t, ok, "failure_categories = %v, want the error category for the failed runs", rep["failure_categories"])
	assert.Equal(t, float64(2), cats["error"])

	// The stdout summary names the written files and the per-starter rate.
	assert.Contains(t, stdout, filepath.Join(outDir, "report.md"))
	assert.Contains(t, stdout, filepath.Join(outDir, "report.json"))
	assert.Contains(t, stdout, "Pass rate per starter per model")
}

// TestBenchmarkCmd_FlagsReachRunner pins the flag-to-runner wiring through
// the benchmarkRunnerFor seam: the positional suite dir (winning over
// --suite), the parsed model list, --runs, and --timeout all reach the
// runner the command builds, and the report is written from that runner's
// own model list.
func TestBenchmarkCmd_FlagsReachRunner(t *testing.T) {
	resetBenchmarkFlags(t)
	suiteDir := filepath.Join(t.TempDir(), "suite")
	writeBenchmarkFixtureTask(t, suiteDir, "mystery-starter", "wiring-task")
	outDir := filepath.Join(t.TempDir(), "results")

	var gotSuiteDir string
	var gotModels []benchmark.ModelSpec
	var gotRuns int
	var gotTimeout time.Duration
	benchmarkRunnerFor = func(seenSuiteDir string, models []benchmark.ModelSpec, runs int, timeout time.Duration) *benchmark.Runner {
		gotSuiteDir, gotModels, gotRuns, gotTimeout = seenSuiteDir, models, runs, timeout
		// A runner whose model list is distinctive: the report must come
		// from THIS runner (the command records the runner's own resolved
		// list), and its runs fail at the fresh-copy step — no provider.
		return &benchmark.Runner{
			Models:      []benchmark.ModelSpec{{Model: "stub-model", Provider: "stub"}},
			RunsPerTask: 1,
		}
	}

	stdout, err := executeBenchmarkCmdKeepSeam(t,
		suiteDir, "--suite", filepath.Join(t.TempDir(), "ignored"),
		"--models", "prov-x/model-x", "--runs", "2", "--timeout", "90s", "--output", outDir)
	require.NoError(t, err)

	assert.Equal(t, suiteDir, gotSuiteDir, "the positional suite dir wins over --suite")
	require.Len(t, gotModels, 1)
	assert.Equal(t, benchmark.ModelSpec{Model: "model-x", Provider: "prov-x"}, gotModels[0])
	assert.Equal(t, 2, gotRuns, "--runs must reach the runner construction")
	assert.Equal(t, 90*time.Second, gotTimeout, "--timeout must reach the runner construction")

	data, err := os.ReadFile(filepath.Join(outDir, "report.json"))
	require.NoError(t, err)
	var rep map[string]any
	require.NoError(t, json.Unmarshal(data, &rep))
	models, ok := rep["models"].([]any)
	require.True(t, ok)
	require.Len(t, models, 1)
	assert.Equal(t, "stub-model", models[0].(map[string]any)["Model"],
		"the report must come from the command's runner, not a re-derived list")
	assert.Contains(t, stdout, "stub")
}

// TestBenchmarkCmd_SuiteStoppedEarlyWritesPartialReportAndFails pins the
// cancelled-suite path: when the harness stops the suite early (the ctx is
// cancelled between pairs), the command still writes the partial report
// from the runs accumulated so far, says so on stdout, and exits non-zero.
func TestBenchmarkCmd_SuiteStoppedEarlyWritesPartialReportAndFails(t *testing.T) {
	resetBenchmarkFlags(t)
	suiteDir := filepath.Join(t.TempDir(), "suite")
	// A real (test-only) starter, so each run gets past the fresh-copy step
	// and reaches the AgentFactory — which is where this test cancels the
	// command's context to stop the suite between pairs.
	writeBenchmarkFixtureTask(t, suiteDir, "fixture", "stopped-task")
	outDir := filepath.Join(t.TempDir(), "results")

	ctx, cancelCmd := context.WithCancel(context.Background())
	defer cancelCmd()
	benchmarkRunnerFor = func(seenSuiteDir string, models []benchmark.ModelSpec, runs int, timeout time.Duration) *benchmark.Runner {
		return &benchmark.Runner{
			AgentFactory: func(runDir string, spec benchmark.ModelSpec) (*agent.Agent, error) {
				// Stop the suite during the first pair's run: the ctx
				// error fires in the SECOND pair's RunTask (before its
				// first run), which is the harness's stopped-early path
				// the command's partial-report branch consumes.
				cancelCmd()
				return nil, errors.New("harness: no agent in this test")
			},
			RunsPerTask: 1,
			Models: []benchmark.ModelSpec{
				{Model: "stub-model", Provider: "stub-a"},
				{Model: "stub-model", Provider: "stub-b"},
			},
		}
	}

	stdout, err := executeBenchmarkRawContext(t, ctx, "--suite", suiteDir, "--output", outDir)
	require.Error(t, err, "a suite that stopped early exits non-zero")
	assert.True(t, errors.Is(err, context.Canceled), "err = %v, want the wrapped context cancellation", err)

	// The partial report is still written: the runs completed before the
	// stop are the evidence an interrupted benchmark exists for.
	data, err := os.ReadFile(filepath.Join(outDir, "report.json"))
	require.NoError(t, err)
	var rep map[string]any
	require.NoError(t, json.Unmarshal(data, &rep))
	tasks, ok := rep["tasks"].([]any)
	require.True(t, ok)
	assert.NotEmpty(t, tasks, "the partial report records the runs that completed")

	assert.Contains(t, stdout, filepath.Join(outDir, "report.json"))
}
