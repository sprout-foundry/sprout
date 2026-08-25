//go:build !js

package cmd

import (
	"context"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/daemon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// saveAgentFlagVars snapshots the package-level agent flag variables and
// restores them at test end, so tests can exercise tryDaemonOneShot and the
// routing gate with flag values set without leaking state into other tests.
func saveAgentFlagVars(t *testing.T) {
	t.Helper()
	persona, provider, model, risk := agentPersona, agentProvider, agentModel, agentRiskProfile
	iter, workflow, session, sysPrompt := maxIterations, agentWorkflowConfig, agentSessionID, agentSystemPrompt
	sysPromptFile, resDir, traceDir := agentSystemPromptFile, agentResourceDirectory, agentTraceDatasetDir
	lastSess, dryRun, unsafe := agentLastSession, agentDryRun, agentUnsafe
	unsafeShell, noSub := agentUnsafeShell, agentNoSubagents
	subModel, subProvider, budget := agentSubagentModel, agentSubagentProvider, agentBudgetUSD
	mockLLM, noProjSkills := agentMockLLM, noProjectSkills
	t.Cleanup(func() {
		agentPersona, agentProvider, agentModel, agentRiskProfile = persona, provider, model, risk
		maxIterations, agentWorkflowConfig, agentSessionID, agentSystemPrompt = iter, workflow, session, sysPrompt
		agentSystemPromptFile, agentResourceDirectory, agentTraceDatasetDir = sysPromptFile, resDir, traceDir
		agentLastSession, agentDryRun, agentUnsafe = lastSess, dryRun, unsafe
		agentUnsafeShell, agentNoSubagents = unsafeShell, noSub
		agentSubagentModel, agentSubagentProvider, agentBudgetUSD = subModel, subProvider, budget
		agentMockLLM, noProjectSkills = mockLLM, noProjSkills
	})
}

// TestDaemonModelSelector pins the provider/model composition rules: both →
// "provider:model", provider only → provider, model only → model, neither →
// "" (daemon defaults).
func TestDaemonModelSelector(t *testing.T) {
	assert.Equal(t, "openai:gpt-4o", daemonModelSelector("openai", "gpt-4o"))
	assert.Equal(t, "openai", daemonModelSelector("openai", ""))
	assert.Equal(t, "gpt-4o", daemonModelSelector("", "gpt-4o"))
	assert.Equal(t, "", daemonModelSelector("", ""))
}

// TestTryDaemonOneShot_SendsFlagOptions verifies the CLI's persona/provider/
// model/risk-profile/max-iterations flags travel the wire protocol inside
// QueryOptions instead of being silently dropped.
func TestTryDaemonOneShot_SendsFlagOptions(t *testing.T) {
	saveAgentFlagVars(t)
	agentPersona = "coder"
	agentProvider = "openai"
	agentModel = "gpt-4o"
	agentRiskProfile = "cautious"
	maxIterations = 7

	stub := &stubAgentForCmd{}
	sockPath := startCmdAgentServer(t, stub)
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
	t.Setenv("SPROUT_DAEMON_AGENT", "1")

	handled, err := tryDaemonOneShot(context.Background(), "hello", false)
	require.NoError(t, err)
	assert.True(t, handled, "one-shot query must be handled by the daemon")

	got, ok := stub.lastOpts.Load().(daemon.QueryOptions)
	require.True(t, ok, "the daemon must receive QueryOptions")
	assert.Equal(t, "coder", got.Persona)
	assert.Equal(t, "openai", got.Provider)
	assert.Equal(t, "gpt-4o", got.Model)
	assert.Equal(t, "cautious", got.RiskProfile)
	assert.Equal(t, 7, got.MaxIterations)
}

// TestNewEphemeralDaemonAgent_AppliesOptions verifies QueryOptions are
// applied to the ephemeral agent exactly like createChatAgent applies them
// in-process: risk-profile override takes effect, and invalid persona /
// risk-profile values error (with the agent shut down, not leaked).
func TestNewEphemeralDaemonAgent_AppliesOptions(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir())

	a, err := newEphemeralDaemonAgent(t.TempDir(), daemon.QueryOptions{RiskProfile: "cautious"})
	require.NoError(t, err)
	assert.Equal(t, configuration.RiskProfileCautious, a.GetActiveRiskProfile())
	a.Shutdown()

	_, err = newEphemeralDaemonAgent(t.TempDir(), daemon.QueryOptions{RiskProfile: "bogus-profile"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "risk_profile")

	_, err = newEphemeralDaemonAgent(t.TempDir(), daemon.QueryOptions{Persona: "definitely-not-a-persona"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persona")
}

// TestTryDaemonOneShot_SkipsWhenUntransmittableFlagsSet verifies a
// behavior-changing flag the wire protocol can't carry (e.g. --system-prompt)
// forces in-process execution even with a healthy daemon socket — the flag
// is honored, never silently dropped.
func TestTryDaemonOneShot_SkipsWhenUntransmittableFlagsSet(t *testing.T) {
	saveAgentFlagVars(t)
	agentSystemPrompt = "custom system prompt"

	stub := &stubAgentForCmd{}
	sockPath := startCmdAgentServer(t, stub)
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
	t.Setenv("SPROUT_DAEMON_AGENT", "1")

	assert.True(t, agentSkipDaemonRouting(), "untransmittable flag must trip the routing gate")

	handled, err := tryDaemonOneShot(context.Background(), "hello", false)
	require.NoError(t, err)
	assert.False(t, handled, "the daemon must not serve queries when an untransmittable flag is set")
	assert.Zero(t, stub.queries, "no query may reach the daemon")
}

// TestAgentSkipDaemonRouting_FlagMatrix pins the routing gate: each
// untransmittable flag forces in-process execution; the transmissible set
// (persona/provider/model/risk-profile/max-iterations) does not.
func TestAgentSkipDaemonRouting_FlagMatrix(t *testing.T) {
	saveAgentFlagVars(t)

	assert.False(t, agentSkipDaemonRouting(), "no flags → routing allowed")

	agentPersona = "coder"
	agentProvider = "openai"
	agentModel = "gpt-4o"
	agentRiskProfile = "cautious"
	maxIterations = 7
	assert.False(t, agentSkipDaemonRouting(), "transmissible flags → routing allowed")

	agentPersona, agentProvider, agentModel = "", "", ""
	agentRiskProfile, maxIterations = "", 0

	cases := map[string]func(t *testing.T){
		"workflow-config":   func(t *testing.T) { agentWorkflowConfig = "wf.json" },
		"session-id":        func(t *testing.T) { agentSessionID = "s1" },
		"last-session":      func(t *testing.T) { agentLastSession = true },
		"system-prompt":     func(t *testing.T) { agentSystemPrompt = "x" },
		"system-prompt-f":   func(t *testing.T) { agentSystemPromptFile = "f.txt" },
		"dry-run":           func(t *testing.T) { agentDryRun = true },
		"unsafe":            func(t *testing.T) { agentUnsafe = true },
		"unsafe-shell":      func(t *testing.T) { agentUnsafeShell = true },
		"no-subagents":      func(t *testing.T) { agentNoSubagents = true },
		"subagent-model":    func(t *testing.T) { agentSubagentModel = "m" },
		"subagent-provider": func(t *testing.T) { agentSubagentProvider = "p" },
		"resource-dir":      func(t *testing.T) { agentResourceDirectory = "d" },
		"budget-usd":        func(t *testing.T) { agentBudgetUSD = 5 },
		"trace-dataset":     func(t *testing.T) { agentTraceDatasetDir = "d" },
		"mock-llm":          func(t *testing.T) { agentMockLLM = true },
		"no-project-skills": func(t *testing.T) { noProjectSkills = true },
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			// Reset the untransmittable set so each case is the sole trip
			// wire — flags would otherwise accumulate across subtests and
			// hide a regression where one of them stops working.
			resetUntransmittableFlags()
			set(t)
			assert.True(t, agentSkipDaemonRouting(), "flag must force in-process")
		})
	}
}

func resetUntransmittableFlags() {
	agentWorkflowConfig, agentSessionID, agentSystemPrompt = "", "", ""
	agentSystemPromptFile, agentResourceDirectory, agentTraceDatasetDir = "", "", ""
	agentLastSession, agentDryRun, agentUnsafe = false, false, false
	agentUnsafeShell, agentNoSubagents = false, false
	agentSubagentModel, agentSubagentProvider, agentBudgetUSD = "", "", 0
	agentMockLLM, noProjectSkills = false, false
}
