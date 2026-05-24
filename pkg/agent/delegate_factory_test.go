package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// newTestAgentForDelegate creates a fully initialized Agent suitable for
// CreateDelegateAgent tests, including a real configManager.
func newTestAgentForDelegate(t *testing.T) *Agent {
	t.Helper()

	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	require.NoError(t, err)

	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	require.NoError(t, err)
	agent.configManager = mgr
	return agent
}

func TestCreateDelegateAgent_NilParent(t *testing.T) {
	cfg := DelegateConfig{Prompt: "do something"}
	_, err := CreateDelegateAgent(nil, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parent agent is required")
}

func TestCreateDelegateAgent_NilConfigManager(t *testing.T) {
	parent := &Agent{}
	cfg := DelegateConfig{Prompt: "do something"}
	_, err := CreateDelegateAgent(parent, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config manager is required")
}

func TestCreateDelegateAgent_NestingDepthExceeded(t *testing.T) {
	parent := newTestAgentForDelegate(t)
	parent.delegateDepth = 10 // well beyond max

	cfg := DelegateConfig{Prompt: "do something"}
	_, err := CreateDelegateAgent(parent, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestCreateDelegateAgent_DelegateDepth(t *testing.T) {
	parent := newTestAgentForDelegate(t)
	parent.delegateDepth = 1

	cfg := DelegateConfig{Prompt: "do something"}
	child, err := CreateDelegateAgent(parent, cfg)
	require.NoError(t, err)
	assert.Equal(t, 2, child.delegateDepth)
}

func TestCreateDelegateAgent_DelegateDepthZero(t *testing.T) {
	parent := newTestAgentForDelegate(t)
	parent.delegateDepth = 0 // top-level agent

	cfg := DelegateConfig{Prompt: "do something"}
	child, err := CreateDelegateAgent(parent, cfg)
	require.NoError(t, err)
	assert.Equal(t, 1, child.delegateDepth)
}

func TestCreateDelegateAgent_PropagatesRootPersonaID(t *testing.T) {
	parent := newTestAgentForDelegate(t)
	parent.rootPersonaID = "orchestrator"

	cfg := DelegateConfig{Prompt: "do something"}
	child, err := CreateDelegateAgent(parent, cfg)
	require.NoError(t, err)
	assert.Equal(t, "orchestrator", child.rootPersonaID)
}

func TestCreateDelegateAgent_DoesNotSetRootPersonaIDWhenEmpty(t *testing.T) {
	parent := newTestAgentForDelegate(t)
	parent.rootPersonaID = ""

	cfg := DelegateConfig{Prompt: "do something"}
	child, err := CreateDelegateAgent(parent, cfg)
	require.NoError(t, err)
	assert.Empty(t, child.rootPersonaID)
}

func TestCreateDelegateAgent_SetsAllowedTools(t *testing.T) {
	parent := newTestAgentForDelegate(t)

	cfg := DelegateConfig{
		Prompt: "do something",
		Tools:  []string{"read_file", "write_file", "Shell"},
	}
	child, err := CreateDelegateAgent(parent, cfg)
	require.NoError(t, err)
	require.NotNil(t, child.allowedTools)
	assert.True(t, child.allowedTools["read_file"])
	assert.True(t, child.allowedTools["write_file"])
	assert.True(t, child.allowedTools["shell"]) // lowercased
}

func TestCreateDelegateAgent_NoAllowedToolsWhenEmpty(t *testing.T) {
	parent := newTestAgentForDelegate(t)

	cfg := DelegateConfig{Prompt: "do something"}
	child, err := CreateDelegateAgent(parent, cfg)
	require.NoError(t, err)
	assert.Nil(t, child.allowedTools)
}

func TestCreateDelegateAgent_InheritsDebugMode(t *testing.T) {
	parent := newTestAgentForDelegate(t)
	parent.debug = true

	cfg := DelegateConfig{Prompt: "do something"}
	child, err := CreateDelegateAgent(parent, cfg)
	require.NoError(t, err)
	assert.True(t, child.debug)
}

func TestCreateDelegateAgent_InheritsDebugModeFalse(t *testing.T) {
	parent := newTestAgentForDelegate(t)
	parent.debug = false

	cfg := DelegateConfig{Prompt: "do something"}
	child, err := CreateDelegateAgent(parent, cfg)
	require.NoError(t, err)
	assert.False(t, child.debug)
}

func TestCreateDelegateAgent_SharedTodoManager(t *testing.T) {
	parent := newTestAgentForDelegate(t)
	parent.todoMgr = tools.NewTodoManager()

	cfg := DelegateConfig{Prompt: "do something"}
	child, err := CreateDelegateAgent(parent, cfg)
	require.NoError(t, err)
	assert.Same(t, parent.todoMgr, child.todoMgr)
}

func TestCreateDelegateAgent_SharedEventBus(t *testing.T) {
	parent := newTestAgentForDelegate(t)
	// parent.eventBus will be nil from NewAgentWithModel, verify child is also nil
	cfg := DelegateConfig{Prompt: "do something"}
	child, err := CreateDelegateAgent(parent, cfg)
	require.NoError(t, err)
	assert.Same(t, parent.eventBus, child.eventBus)
}

func TestCreateDelegateAgent_SharedEmbeddingManager(t *testing.T) {
	parent := newTestAgentForDelegate(t)
	// parent.embeddingMgr will be nil from NewAgentWithModel, verify child is also nil
	cfg := DelegateConfig{Prompt: "do something"}
	child, err := CreateDelegateAgent(parent, cfg)
	require.NoError(t, err)
	assert.Same(t, parent.embeddingMgr, child.embeddingMgr)
}

func TestCreateDelegateAgent_SetMaxIterations(t *testing.T) {
	parent := newTestAgentForDelegate(t)

	cfg := DelegateConfig{
		Prompt:        "do something",
		MaxIterations: 50,
	}
	child, err := CreateDelegateAgent(parent, cfg)
	require.NoError(t, err)
	assert.Equal(t, 50, child.maxIterations)
}

func TestCreateDelegateAgent_InheritsWorkspaceRoot(t *testing.T) {
	parent := newTestAgentForDelegate(t)
	parent.workspaceRoot = "/some/workspace"

	cfg := DelegateConfig{Prompt: "do something"}
	child, err := CreateDelegateAgent(parent, cfg)
	require.NoError(t, err)
	assert.Equal(t, "/some/workspace", child.workspaceRoot)
}

func TestCreateDelegateAgent_BuildsSystemPrompt(t *testing.T) {
	parent := newTestAgentForDelegate(t)

	cfg := DelegateConfig{
		Prompt: "write tests",
		Role:   "tester",
	}
	child, err := CreateDelegateAgent(parent, cfg)
	require.NoError(t, err)
	assert.Contains(t, child.systemPrompt, "tester")
	assert.Contains(t, child.systemPrompt, "write tests")
}

func TestBuildDelegateSystemPrompt_WithRole(t *testing.T) {
	cfg := DelegateConfig{
		Prompt: "write tests",
		Role:   "tester",
	}
	prompt := buildDelegateSystemPrompt(cfg)
	assert.Contains(t, prompt, "You are a delegated agent with the role: tester")
	assert.Contains(t, prompt, "Task: write tests")
	assert.Contains(t, prompt, "Complete the task and provide a clear summary of your work")
}

func TestBuildDelegateSystemPrompt_WithoutRole(t *testing.T) {
	cfg := DelegateConfig{
		Prompt: "write tests",
	}
	prompt := buildDelegateSystemPrompt(cfg)
	assert.Contains(t, prompt, "You are a delegated agent assisting with a specific task")
	assert.Contains(t, prompt, "Task: write tests")
}

func TestBuildDelegateSystemPrompt_WithContext(t *testing.T) {
	cfg := DelegateConfig{
		Prompt:  "write tests",
		Role:    "coder",
		Context: "The project uses Go and testify.",
	}
	prompt := buildDelegateSystemPrompt(cfg)
	assert.Contains(t, prompt, "Context from parent agent:")
	assert.Contains(t, prompt, "The project uses Go and testify")
}

func TestBuildDelegateSystemPrompt_WithFiles(t *testing.T) {
	cfg := DelegateConfig{
		Prompt: "write tests",
		Files:  []string{"pkg/agent/agent.go", "pkg/agent/delegate.go"},
	}
	prompt := buildDelegateSystemPrompt(cfg)
	assert.Contains(t, prompt, "Relevant files: pkg/agent/agent.go, pkg/agent/delegate.go")
}

func TestBuildDelegateSystemPrompt_WithEmptyConfig(t *testing.T) {
	cfg := DelegateConfig{}
	prompt := buildDelegateSystemPrompt(cfg)
	assert.Contains(t, prompt, "You are a delegated agent assisting with a specific task")
	assert.Contains(t, prompt, "Complete the task and provide a clear summary of your work")
	assert.NotContains(t, prompt, "Task: ")
	assert.NotContains(t, prompt, "Context from parent agent")
	assert.NotContains(t, prompt, "Relevant files")
}

func TestBuildDelegateSystemPrompt_OnlyRole(t *testing.T) {
	cfg := DelegateConfig{Role: "debugger"}
	prompt := buildDelegateSystemPrompt(cfg)
	assert.Contains(t, prompt, "You are a delegated agent with the role: debugger")
	assert.NotContains(t, prompt, "Task: ")
}

func TestRestrictTools(t *testing.T) {
	child := &Agent{}
	restrictTools(child, []string{"read_file", "Write_File", "shell"})
	require.NotNil(t, child.allowedTools)
	assert.True(t, child.allowedTools["read_file"])
	assert.True(t, child.allowedTools["write_file"])
	assert.True(t, child.allowedTools["shell"])
	assert.False(t, child.allowedTools["git"])
}

func TestRestrictTools_EmptyList(t *testing.T) {
	child := &Agent{}
	restrictTools(child, []string{})
	require.NotNil(t, child.allowedTools)
	assert.Empty(t, child.allowedTools)
}

func TestRestrictTools_NilList(t *testing.T) {
	child := &Agent{}
	restrictTools(child, nil)
	require.NotNil(t, child.allowedTools)
	assert.Empty(t, child.allowedTools)
}

func TestCreateDelegateAgent_UsesConfigProviderModel(t *testing.T) {
	parent := newTestAgentForDelegate(t)

	cfg := DelegateConfig{
		Prompt:   "do something",
		Provider: "test",
		Model:    "test",
	}
	child, err := CreateDelegateAgent(parent, cfg)
	require.NoError(t, err)
	assert.NotNil(t, child)
}

func TestCreateDelegateAgent_CustomEnvMaxDepth(t *testing.T) {
	t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", "2")
	defer t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", "")

	parent := newTestAgentForDelegate(t)
	parent.delegateDepth = 2 // at max, so newDepth would be 3 > 2

	cfg := DelegateConfig{Prompt: "do something"}
	_, err := CreateDelegateAgent(parent, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestCreateDelegateAgent_InvalidProviderModel(t *testing.T) {
	parent := newTestAgentForDelegate(t)

	cfg := DelegateConfig{
		Prompt:   "do something",
		Provider: "nonexistent-provider",
		Model:    "nonexistent-model",
	}
	_, err := CreateDelegateAgent(parent, cfg)
	require.Error(t, err)
	// Could fail at resolve or at client creation
	assert.True(t, strings.Contains(err.Error(), "resolve provider/model") ||
		strings.Contains(err.Error(), "create client"),
		"expected resolve or client creation error, got: %v", err)
}
