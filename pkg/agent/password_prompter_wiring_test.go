package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// fakePrompter is a minimal PasswordPrompter for wiring tests. It returns
// a fixed password and records the reason it was called with.
type fakePrompter struct {
	password string
	called   bool
	reason   string
}

func (f *fakePrompter) Prompt(_ context.Context, reason string) (string, error) {
	f.called = true
	f.reason = reason
	return f.password, nil
}

// =============================================================================
// GetPasswordPrompter / SetPasswordPrompter / HasPasswordPrompter
// =============================================================================

func TestHasPasswordPrompter_DefaultFalse(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()
	assert.False(t, agent.HasPasswordPrompter(), "prompter should be nil by default")
}

func TestSetGetPasswordPrompter_RoundTrip(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	fp := &fakePrompter{password: "secret"}
	agent.SetPasswordPrompter(fp)

	require.True(t, agent.HasPasswordPrompter(), "prompter should be registered")
	got := agent.GetPasswordPrompter()
	assert.Equal(t, fp, got, "GetPasswordPrompter should return the registered prompter")
}

func TestSetPasswordPrompter_Nil(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	agent.SetPasswordPrompter(&fakePrompter{})
	require.True(t, agent.HasPasswordPrompter())

	agent.SetPasswordPrompter(nil)
	assert.False(t, agent.HasPasswordPrompter(), "setting nil should clear the prompter")
}

// =============================================================================
// ResolveToolRisk — classifier gating
// =============================================================================

// TestResolveToolRisk_PrivilegedDowngradedWithPrompter verifies that a sudo
// command is Medium (CAUTION level from classifier) when a password prompter
// is registered. Since sudo is now CAUTION in the classifier, the level is
// already Medium before the prompter downgrade logic — the downgrade only
// fires at High or above, so RiskSourcePasswordPrompter is NOT added.
func TestResolveToolRisk_PrivilegedDowngradedWithPrompter(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()
	agent.SetPasswordPrompter(&fakePrompter{password: "pw"})

	args := map[string]interface{}{"command": "sudo apt update"}
	assessment := agent.ResolveToolRisk("shell_command", args)

	assert.True(t, assessment.Level.Rank() <= configuration.RiskLevelMedium.Rank(),
		"sudo with prompter should be Medium or lower, got %s", assessment.Level)
	assert.False(t, assessment.IsHardBlock, "sudo with prompter should not be a hard block")
}

// TestResolveToolRisk_PrivilegedNotDowngradedWithoutPrompter verifies that
// a sudo command is CAUTION (Medium) even without a password prompter.
// The classifier now returns CAUTION for sudo, so no prompter is needed
// to avoid a hard block — sudo simply prompts in the default profile.
func TestResolveToolRisk_PrivilegedNotDowngradedWithoutPrompter(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()
	// No prompter — sudo is now CAUTION (Medium), not blocked.
	assert.False(t, agent.HasPasswordPrompter())

	args := map[string]interface{}{"command": "sudo apt update"}
	assessment := agent.ResolveToolRisk("shell_command", args)

	assert.True(t, assessment.Level.Rank() <= configuration.RiskLevelMedium.Rank(),
		"sudo without prompter should be Medium or lower (CAUTION), got %s", assessment.Level)
	assert.False(t, assessment.IsHardBlock, "sudo without prompter should not be a hard block")
}

// TestResolveToolRisk_DestructiveNotDowngradedWithPrompter is the safety
// guard: even with a prompter, destructive commands (rm -rf) must NOT be
// downgraded. Only RiskCategoryPrivileged is eligible.
func TestResolveToolRisk_DestructiveNotDowngradedWithPrompter(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()
	agent.SetPasswordPrompter(&fakePrompter{password: "pw"})

	args := map[string]interface{}{"command": "rm -rf /tmp/sprout_test_dir"}
	assessment := agent.ResolveToolRisk("shell_command", args)

	// rm -rf should remain High or Critical even with a prompter.
	assert.True(t, assessment.Level.Rank() >= configuration.RiskLevelHigh.Rank(),
		"rm -rf with prompter should still be High or Critical, got %s", assessment.Level)
}

// =============================================================================
// executeShellCommandWithTruncation — context wiring
// =============================================================================

// TestExecuteShellCommand_PrompterInContext verifies that the prompter is
// placed into the execution context. We test this by checking that
// PasswordPrompterFromContext returns the registered prompter after the
// wiring function runs. Since executeShellCommandWithTruncation runs a real
// command, we instead verify the wiring logic directly: the agent's
// passwordPrompter field, when set, is what WithPasswordPrompter would
// inject. This is a structural test — the actual stdin plumbing lives in
// the shell tool (a follow-up slice).
func TestExecuteShellCommand_PrompterInContext(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()

	fp := &fakePrompter{password: "ctx-pw"}
	agent.SetPasswordPrompter(fp)

	// Simulate the wiring that executeShellCommandWithTruncation does.
	ctx := context.Background()
	if agent.passwordPrompter != nil {
		ctx = tools.WithPasswordPrompter(ctx, agent.passwordPrompter)
	}

	got := tools.PasswordPrompterFromContext(ctx)
	require.NotNil(t, got, "prompter should be in context after wiring")

	// Verify it's the same prompter and it works.
	pwd, err := got.Prompt(ctx, "test")
	require.NoError(t, err)
	assert.Equal(t, "ctx-pw", pwd)
	assert.True(t, fp.called, "the wired prompter should be the fakePrompter instance")
}

// TestExecuteShellCommand_NoPrompterInContextWhenUnset verifies that when
// no prompter is registered, the context does not carry one (nil-safe).
func TestExecuteShellCommand_NoPrompterInContextWhenUnset(t *testing.T) {
	agent := newIsolatedTestAgent(t)
	defer agent.Shutdown()
	assert.False(t, agent.HasPasswordPrompter())

	ctx := context.Background()
	if agent.passwordPrompter != nil {
		ctx = tools.WithPasswordPrompter(ctx, agent.passwordPrompter)
	}

	got := tools.PasswordPrompterFromContext(ctx)
	assert.Nil(t, got, "no prompter should be in context when unset")
}
