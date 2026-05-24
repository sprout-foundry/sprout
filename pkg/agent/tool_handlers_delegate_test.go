package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestParseDelegateConfig_AllFields(t *testing.T) {
	args := map[string]interface{}{
		"prompt":         "write tests",
		"role":           "tester",
		"provider":       "openai",
		"model":          "gpt-4",
		"tools":          []interface{}{"read_file", "write_file"},
		"context":        "Some background info",
		"max_iterations": float64(50),
		"files":          []interface{}{"pkg/agent/agent.go", "pkg/agent/test.go"},
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)

	assert.Equal(t, "write tests", cfg.Prompt)
	assert.Equal(t, "tester", cfg.Role)
	assert.Equal(t, "openai", cfg.Provider)
	assert.Equal(t, "gpt-4", cfg.Model)
	assert.Equal(t, []string{"read_file", "write_file"}, cfg.Tools)
	assert.Equal(t, "Some background info", cfg.Context)
	assert.Equal(t, 50, cfg.MaxIterations)
	assert.Equal(t, []string{"pkg/agent/agent.go", "pkg/agent/test.go"}, cfg.Files)
}

func TestParseDelegateConfig_MissingFields(t *testing.T) {
	args := map[string]interface{}{
		"prompt": "do something",
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)

	assert.Equal(t, "do something", cfg.Prompt)
	assert.Empty(t, cfg.Role)
	assert.Empty(t, cfg.Provider)
	assert.Empty(t, cfg.Model)
	assert.Nil(t, cfg.Tools)
	assert.Empty(t, cfg.Context)
	assert.Equal(t, 0, cfg.MaxIterations)
	assert.Nil(t, cfg.Files)
}

func TestParseDelegateConfig_EmptyArgs(t *testing.T) {
	cfg, err := parseDelegateConfig(map[string]interface{}{})
	require.NoError(t, err)

	assert.Empty(t, cfg.Prompt)
}

func TestParseDelegateConfig_MaxIterationsAsFloat64(t *testing.T) {
	// JSON numbers come through as float64 from the JSON unmarshaler
	args := map[string]interface{}{
		"prompt":         "test",
		"max_iterations": float64(100),
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.Equal(t, 100, cfg.MaxIterations)
}

func TestParseDelegateConfig_MaxIterationsAsInt(t *testing.T) {
	// Direct Go map with int value
	args := map[string]interface{}{
		"prompt":         "test",
		"max_iterations": 100,
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.Equal(t, 100, cfg.MaxIterations)
}

func TestParseDelegateConfig_MaxIterationsZero(t *testing.T) {
	args := map[string]interface{}{
		"prompt":         "test",
		"max_iterations": float64(0),
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.Equal(t, 0, cfg.MaxIterations)
}

func TestParseDelegateConfig_ToolsAsSliceOfStrings(t *testing.T) {
	args := map[string]interface{}{
		"prompt": "test",
		"tools":  []interface{}{"read_file", "write_file", "shell_command"},
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.Equal(t, []string{"read_file", "write_file", "shell_command"}, cfg.Tools)
}

func TestParseDelegateConfig_ToolsEmpty(t *testing.T) {
	args := map[string]interface{}{
		"prompt": "test",
		"tools":  []interface{}{},
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.Empty(t, cfg.Tools)
}

func TestParseDelegateConfig_ToolsMissing(t *testing.T) {
	args := map[string]interface{}{
		"prompt": "test",
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.Nil(t, cfg.Tools)
}

func TestParseDelegateConfig_FilesAsSliceOfStrings(t *testing.T) {
	args := map[string]interface{}{
		"prompt": "test",
		"files":  []interface{}{"pkg/a/a.go", "pkg/b/b.go"},
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.Equal(t, []string{"pkg/a/a.go", "pkg/b/b.go"}, cfg.Files)
}

func TestParseDelegateConfig_FilesEmpty(t *testing.T) {
	args := map[string]interface{}{
		"prompt": "test",
		"files":  []interface{}{},
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.Empty(t, cfg.Files)
}

func TestParseDelegateConfig_FilesMissing(t *testing.T) {
	args := map[string]interface{}{
		"prompt": "test",
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.Nil(t, cfg.Files)
}

func TestParseDelegateConfig_MissingToolsInSlice(t *testing.T) {
	// When tools contains non-string elements, they are silently skipped
	args := map[string]interface{}{
		"prompt": "test",
		"tools":  []interface{}{"read_file", 123, "write_file"},
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	// Only the string elements are extracted
	assert.Equal(t, []string{"read_file", "write_file"}, cfg.Tools)
}

func TestParseDelegateConfig_MissingFilesInSlice(t *testing.T) {
	// When files contains non-string elements, they are silently skipped
	args := map[string]interface{}{
		"prompt": "test",
		"files":  []interface{}{"pkg/a.go", 456},
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	// Only the string elements are extracted
	assert.Equal(t, []string{"pkg/a.go"}, cfg.Files)
}

func TestParseDelegateConfig_WrongTypeForTools(t *testing.T) {
	// When tools is not a []interface{}, it's silently ignored
	args := map[string]interface{}{
		"prompt": "test",
		"tools":  "not a slice",
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.Nil(t, cfg.Tools)
}

func TestParseDelegateConfig_WrongTypeForFiles(t *testing.T) {
	// When files is not a []interface{}, it's silently ignored
	args := map[string]interface{}{
		"prompt": "test",
		"files":  "not a slice",
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.Nil(t, cfg.Files)
}

func TestParseDelegateConfig_WrongTypeForMaxIterations(t *testing.T) {
	// When max_iterations is not float64 or int, it's silently ignored
	args := map[string]interface{}{
		"prompt":         "test",
		"max_iterations": "not a number",
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.Equal(t, 0, cfg.MaxIterations)
}

func TestParseDelegateConfig_WrongTypeForPrompt(t *testing.T) {
	// When prompt is not a string, it's silently ignored (zero value)
	args := map[string]interface{}{
		"prompt": 12345,
	}

	cfg, err := parseDelegateConfig(args)
	require.NoError(t, err)
	assert.Equal(t, "", cfg.Prompt)
}

func TestParseDelegateConfig_TableDriven(t *testing.T) {
	tests := []struct {
		name        string
		args        map[string]interface{}
		wantErr     bool
		wantPrompt  string
		wantRole    string
		wantTools   []string
		wantFiles   []string
		wantContext string
	}{
		{
			name: "minimal valid config",
			args: map[string]interface{}{
				"prompt": "do something",
			},
			wantErr:    false,
			wantPrompt: "do something",
		},
		{
			name: "with role only",
			args: map[string]interface{}{
				"prompt": "code review",
				"role":   "code_reviewer",
			},
			wantErr:    false,
			wantPrompt: "code review",
			wantRole:   "code_reviewer",
		},
		{
			name: "with all optional fields",
			args: map[string]interface{}{
				"prompt":         "write tests",
				"role":           "tester",
				"context":        "Use Go testing",
				"tools":          []interface{}{"read_file"},
				"files":          []interface{}{"pkg/a.go"},
				"max_iterations": float64(25),
			},
			wantErr:     false,
			wantPrompt:  "write tests",
			wantRole:    "tester",
			wantContext: "Use Go testing",
			wantTools:   []string{"read_file"},
			wantFiles:   []string{"pkg/a.go"},
		},
		{
			name: "provider and model",
			args: map[string]interface{}{
				"prompt":   "test",
				"provider": "anthropic",
				"model":    "claude-3",
			},
			wantErr:    false,
			wantPrompt: "test",
		},
		{
			name: "empty prompt",
			args: map[string]interface{}{
				"prompt": "",
			},
			wantErr:    false,
			wantPrompt: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseDelegateConfig(tt.args)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantPrompt, cfg.Prompt)
			assert.Equal(t, tt.wantRole, cfg.Role)
			assert.Equal(t, tt.wantContext, cfg.Context)
			assert.Equal(t, tt.wantTools, cfg.Tools)
			assert.Equal(t, tt.wantFiles, cfg.Files)
		})
	}
}

func TestTruncateSummary_UnderMaxLen(t *testing.T) {
	input := "Short summary"
	result := truncateSummary(input, 100)
	assert.Equal(t, "Short summary", result)
}

func TestTruncateSummary_ExactLength(t *testing.T) {
	input := "12345678901234567890" // exactly 20 chars
	result := truncateSummary(input, 20)
	assert.Equal(t, "12345678901234567890", result)
}

func TestTruncateSummary_OverMaxLen(t *testing.T) {
	input := "This is a very long summary that should be truncated"
	result := truncateSummary(input, 30)
	// truncateSummary does s[:maxLen] + "...", so result is maxLen + 3 chars
	assert.Equal(t, "This is a very long summary th...", result)
	assert.Equal(t, 33, len(result))
}

func TestTruncateSummary_EmptyString(t *testing.T) {
	result := truncateSummary("", 50)
	assert.Equal(t, "", result)
}

func TestTruncateSummary_MaxLenTwo(t *testing.T) {
	input := "Long string"
	result := truncateSummary(input, 2)
	assert.Equal(t, "Lo...", result)
	assert.Equal(t, 5, len(result))
}

func TestTruncateSummary_MaxLenThree(t *testing.T) {
	input := "Long string"
	result := truncateSummary(input, 3)
	assert.Equal(t, "Lon...", result)
	assert.Equal(t, 6, len(result))
}

func TestTruncateSummary_MaxLenFour(t *testing.T) {
	input := "Hello World"
	result := truncateSummary(input, 4)
	assert.Equal(t, "Hell...", result)
	assert.Equal(t, 7, len(result))
}

func TestTruncateSummary_MaxLenZero(t *testing.T) {
	input := "test"
	result := truncateSummary(input, 0)
	assert.Equal(t, "...", result)
}

func TestHandleDelegate_EmptyPrompt(t *testing.T) {
	args := map[string]interface{}{
		"prompt": "",
	}

	_, err := handleDelegate(context.Background(), nil, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt")
}

func TestHandleDelegate_NilAgent(t *testing.T) {
	args := map[string]interface{}{
		"prompt": "do something",
	}

	_, err := handleDelegate(context.Background(), nil, args)
	require.Error(t, err)
}

func TestHandleDelegate_NestingDepthExceeded(t *testing.T) {
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	require.NoError(t, err)

	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	require.NoError(t, err)
	agent.configManager = mgr
	agent.delegateDepth = 10 // beyond max depth

	args := map[string]interface{}{
		"prompt": "do something",
	}

	_, err = handleDelegate(context.Background(), agent, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestHandleDelegate_NestingDepth_AtMax(t *testing.T) {
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	require.NoError(t, err)

	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	require.NoError(t, err)
	agent.configManager = mgr
	agent.delegateDepth = 3 // at max depth (3), new depth would be 4

	args := map[string]interface{}{
		"prompt": "do something",
	}

	_, err = handleDelegate(context.Background(), agent, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestHandleDelegate_WhitespaceOnlyPrompt(t *testing.T) {
	// Whitespace-only prompt passes the empty check in handleDelegate
	// (only "" fails), but would fail Validate(). handleDelegate uses
	// cfg.Prompt == "", so whitespace-only is technically accepted here.
	args := map[string]interface{}{
		"prompt": "   ",
	}

	// This should NOT error from handleDelegate since it only checks ""
	// The nil agent check runs first and returns "agent is required"
	_, err := handleDelegate(context.Background(), nil, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent is required")
	assert.NotContains(t, err.Error(), "prompt is required")
}

func TestHandleDelegate_CanCreateDelegateAgent(t *testing.T) {
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	require.NoError(t, err)

	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	require.NoError(t, err)
	agent.configManager = mgr
	agent.delegateDepth = 0 // top level, can delegate

	args := map[string]interface{}{
		"prompt":   "do something",
		"provider": "test",
		"model":    "test",
	}

	// This will fail at the agent.Run step (no proper setup) but should
	// get past the delegate creation and nesting checks
	_, err = handleDelegate(context.Background(), agent, args)
	// We expect either an error from Run or a result - the key is it doesn't
	// fail at the nesting/config level
	// If there's an error, it should NOT be about nesting depth
	if err != nil {
		assert.NotContains(t, err.Error(), "exceeds maximum",
			"should not fail on nesting depth when at depth 0")
	}
}
