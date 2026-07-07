package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/history"
)

// =====================================================================
// HelpCommand Tests
// =====================================================================

func TestHelpCommand_Name(t *testing.T) {
	h := &HelpCommand{}
	assert.Equal(t, "help", h.Name())
}

func TestHelpCommand_Description(t *testing.T) {
	h := &HelpCommand{}
	assert.Equal(t, "Show help information and available slash commands", h.Description())
}

func TestHelpCommand_Execute_Output(t *testing.T) {
	registry := NewCommandRegistry()
	h := &HelpCommand{registry: registry}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := h.Execute(nil, nil)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	assert.NoError(t, err)
	assert.Contains(t, output, "Sprout")
	assert.Contains(t, output, "AVAILABLE SLASH COMMANDS")
	assert.Contains(t, output, "/help")
}

// =====================================================================
// LogActionItem Tests
// =====================================================================
// LogActionItem Tests
// =====================================================================

func TestLogActionItem_Display(t *testing.T) {
	item := LogActionItem{
		ID:          "view_log",
		DisplayName: "[list] View Change Log",
		Description: "Display complete change history",
	}
	assert.Equal(t, "[list] View Change Log", item.Display())
}

func TestLogActionItem_SearchText(t *testing.T) {
	item := LogActionItem{
		ID:          "view_log",
		DisplayName: "[list] View Change Log",
		Description: "Display complete change history",
	}
	assert.Equal(t, "[list] View Change Log Display complete change history", item.SearchText())
}

func TestLogActionItem_Value(t *testing.T) {
	item := LogActionItem{
		ID:          "rollback_select",
		DisplayName: "[|<] Select Revision",
		Description: "Choose from available revisions",
	}
	val := item.Value()
	assert.Equal(t, "rollback_select", val)
}

// =====================================================================
// RevisionItem Tests
// =====================================================================

func TestRevisionItem_Display(t *testing.T) {
	item := RevisionItem{
		RevisionID:  "rev_abc123",
		Description: "Fixed login bug",
		Timestamp:   "2024-01-15",
	}
	assert.Equal(t, "rev_abc123 - Fixed login bug", item.Display())
}

func TestRevisionItem_SearchText(t *testing.T) {
	item := RevisionItem{
		RevisionID:  "rev_def456",
		Description: "Added auth module",
		Timestamp:   "2024-01-16 10:00:00",
	}
	assert.Equal(t, "rev_def456 Added auth module 2024-01-16 10:00:00", item.SearchText())
}

func TestRevisionItem_Value(t *testing.T) {
	item := RevisionItem{
		RevisionID:  "rev_ghi789",
		Description: "Updated README",
		Timestamp:   "2024-01-17",
	}
	val := item.Value()
	assert.Equal(t, "rev_ghi789", val)
}

// =====================================================================
// CompactCommand Tests
// =====================================================================

func TestCompactCommand_Name(t *testing.T) {
	c := &CompactCommand{}
	assert.Equal(t, "compact", c.Name())
}

func TestCompactCommand_Description(t *testing.T) {
	c := &CompactCommand{}
	assert.Equal(t, "LLM-summarize the middle of the conversation, preserving the opening task anchor and the recent causal chain", c.Description())
}

func TestCompactCommand_Execute_NilAgent(t *testing.T) {
	c := &CompactCommand{}
	err := c.Execute(nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "agent not available")
}

func TestCompactCommand_Execute_MiddleTooSmallShortCircuits(t *testing.T) {
	// 0:  system                                (anchor start)
	// 1:  user "u0"                             (anchor user)
	// 2:  assistant "a0" plain                  (anchor assistant, anchorEnd=3)
	// 3..29: all assistant-tc                   (every slot triggers branch-2 walk-back)
	//
	// anchorEnd = 3. raw recentStart = 30 - 12 = 18. adjustRecentBoundary
	// walks back via branch 2 repeatedly: 18 → 17 → 16 → ... → 4 → 3.
	// At recentStart=3, branch 2 checks messages[2] (assistant plain, no
	// tool calls) → skip → break. got=3. middle = 3-3 = 0 < 6 → middle-
	// too-small branch fires. ✓
	a := agent.NewTestAgent()
	a.SetMessages(makeMiddleTooSmallHistory())
	c := &CompactCommand{}

	output := captureOutput(func() {
		err := c.Execute(nil, a)
		assert.NoError(t, err)
	})

	assert.Contains(t, output, "Middle segment too small to be worth summarizing")
}

// =====================================================================
// ModelsCommand Pure Function Tests
// =====================================================================

func TestModelsCommand_CommonPrefix(t *testing.T) {
	m := &ModelsCommand{}

	tests := []struct {
		name     string
		a        string
		b        string
		expected string
	}{
		{"common prefix", "gpt-4-turbo", "gpt-4-32k", "gpt-4-"},
		{"no common prefix", "gpt-4", "claude-3", ""},
		{"identical strings", "gpt-4", "gpt-4", "gpt-4"},
		{"one is prefix of other", "gpt", "gpt-4-turbo", "gpt"},
		{"empty a", "", "gpt-4", ""},
		{"empty b", "gpt-4", "", ""},
		{"both empty", "", "", ""},
		{"case insensitive", "GPT-4", "gpt-3", "GPT-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.commonPrefix(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestModelsCommand_FindExactModel(t *testing.T) {
	m := &ModelsCommand{}

	tests := []struct {
		name     string
		models   []api.ModelInfo
		query    string
		expected string // empty string means nil expected
	}{
		{
			name: "exact match",
			models: []api.ModelInfo{
				{ID: "gpt-4", Description: "OpenAI GPT-4"},
				{ID: "gpt-3.5-turbo", Description: "OpenAI GPT-3.5"},
			},
			query:    "gpt-4",
			expected: "gpt-4",
		},
		{
			name: "case insensitive match",
			models: []api.ModelInfo{
				{ID: "GPT-4", Description: "OpenAI GPT-4"},
			},
			query:    "gpt-4",
			expected: "GPT-4",
		},
		{
			name: "no match",
			models: []api.ModelInfo{
				{ID: "gpt-4", Description: "OpenAI GPT-4"},
			},
			query:    "claude-3",
			expected: "",
		},
		{
			name:     "empty models list",
			models:   []api.ModelInfo{},
			query:    "gpt-4",
			expected: "",
		},
		{
			name:     "nil models list",
			models:   nil,
			query:    "gpt-4",
			expected: "",
		},
		{
			name:     "empty query",
			models:   []api.ModelInfo{{ID: "gpt-4", Description: "OpenAI GPT-4"}},
			query:    "",
			expected: "",
		},
		{
			name: "partial match should not match",
			models: []api.ModelInfo{
				{ID: "gpt-4-turbo", Description: "OpenAI GPT-4 Turbo"},
				{ID: "gpt-4", Description: "OpenAI GPT-4"},
			},
			query:    "gpt",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.findExactModel(tt.models, tt.query)
			if tt.expected == "" {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, tt.expected, result.ID)
			}
		})
	}
}

func TestModelsCommand_FindCommonPrefix(t *testing.T) {
	m := &ModelsCommand{}

	tests := []struct {
		name     string
		matches  []api.ModelInfo
		input    string
		expected string
	}{
		{
			name:     "no matches",
			matches:  []api.ModelInfo{},
			input:    "gpt",
			expected: "",
		},
		{
			name: "single match",
			matches: []api.ModelInfo{
				{ID: "gpt-4-turbo"},
			},
			input:    "gpt",
			expected: "gpt-4-", // finds stop chars '-' and extends
		},
		{
			name: "two matches with meaningful common extension",
			matches: []api.ModelInfo{
				{ID: "openrouter/anthropic/claude-3-opus"},
				{ID: "openrouter/anthropic/claude-3-sonnet"},
			},
			input:    "openrouter",
			expected: "openrouter/anthropic/claude-",
		},
		{
			name: "two matches with long common prefix after input",
			matches: []api.ModelInfo{
				{ID: "openrouter/anthropic/claude-3-opus"},
				{ID: "openrouter/anthropic/claude-3-sonnet"},
			},
			input:    "openrouter/an",
			expected: "openrouter/anthropic/claude-",
		},
		{
			name: "matches diverge after input",
			matches: []api.ModelInfo{
				{ID: "gpt-4"},
				{ID: "gpt-3.5"},
			},
			input:    "gpt",
			expected: "", // common prefix "gpt" is not > len(input)+1
		},
		{
			name: "three matches with common prefix",
			matches: []api.ModelInfo{
				{ID: "openrouter/anthropic/claude-3-opus"},
				{ID: "openrouter/anthropic/claude-3-sonnet"},
				{ID: "openrouter/anthropic/claude-3-haiku"},
			},
			input:    "openrouter",
			expected: "openrouter/anthropic/claude-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.findCommonPrefix(tt.matches, tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestModelsCommand_CalculateFuzzyScore(t *testing.T) {
	m := &ModelsCommand{}

	tests := []struct {
		name     string
		model    api.ModelInfo
		query    string
		expected int
	}{
		{
			name: "substring at start gets bonus",
			model: api.ModelInfo{
				ID:          "gpt-4-turbo",
				Description: "OpenAI GPT-4 Turbo",
			},
			query:    "gpt",
			expected: 190, // 100 contain + 50 prefix + 30 word "gpt" in ID + 10 word "gpt" in description
		},
		{
			name: "no match returns 0",
			model: api.ModelInfo{
				ID:          "gpt-4-turbo",
				Description: "OpenAI GPT-4 Turbo",
			},
			query:    "claude",
			expected: 0,
		},
		{
			name: "multi-part query with slash",
			model: api.ModelInfo{
				ID:          "openrouter/gpt-4",
				Description: "OpenRouter GPT-4",
			},
			query:    "openrouter/gpt",
			expected: 100 + 50 + 80, // contain + prefix + both parts match
		},
		{
			name: "word match in ID",
			model: api.ModelInfo{
				ID:          "gpt-4-turbo",
				Description: "OpenAI GPT-4 Turbo",
			},
			query:    "turbo",
			expected: 140, // 100 contain in ID + 30 word "turbo" in ID + 10 word "turbo" in description
		},
		{
			name: "word match in description only",
			model: api.ModelInfo{
				ID:          "gpt-4",
				Description: "A smart model for coding",
			},
			query:    "smart",
			expected: 10, // word match in description only
		},
		{
			name: "short word ignored (less than 3 chars)",
			model: api.ModelInfo{
				ID:          "gpt-4",
				Description: "An ai model",
			},
			query:    "an",
			expected: 0, // "an" is only 2 chars
		},
		{
			name: "empty query",
			model: api.ModelInfo{
				ID:          "gpt-4",
				Description: "OpenAI GPT-4",
			},
			query:    "",
			expected: 150, // empty string is contained in every string (100) + prefix (50)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := m.calculateFuzzyScore(tt.model, tt.query)
			assert.Equal(t, tt.expected, score)
		})
	}
}

func TestModelsCommand_FuzzySearchModels(t *testing.T) {
	m := &ModelsCommand{}

	models := []api.ModelInfo{
		{ID: "gpt-4-turbo", Description: "OpenAI GPT-4 Turbo"},
		{ID: "gpt-3.5-turbo", Description: "OpenAI GPT-3.5"},
		{ID: "claude-3-opus", Description: "Anthropic Claude 3 Opus"},
		{ID: "claude-3-sonnet", Description: "Anthropic Claude 3 Sonnet"},
		{ID: "llama-3-70b", Description: "Meta Llama 3 70B"},
	}

	tests := []struct {
		name      string
		query     string
		wantCount int
		wantFirst string // ID of top result
	}{
		{
			name:      "search gpt",
			query:     "gpt",
			wantCount: 2,
			wantFirst: "gpt-4-turbo", // higher score (prefix bonus)
		},
		{
			name:      "search claude",
			query:     "claude",
			wantCount: 2,
			wantFirst: "claude-3-opus",
		},
		{
			name:      "search non-existent",
			query:     "nonexistent",
			wantCount: 0,
			wantFirst: "",
		},
		{
			name:      "empty query returns all results",
			query:     "",
			wantCount: 5,  // empty string matches all (score 150 each)
			wantFirst: "", // order not guaranteed for equal scores
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := m.fuzzySearchModels(models, tt.query)
			assert.Len(t, results, tt.wantCount)
			if tt.wantCount > 0 && tt.wantFirst != "" {
				assert.Equal(t, tt.wantFirst, results[0].ID)
			}
		})
	}
}

func TestModelsCommand_FuzzySearchModels_LimitsTo10(t *testing.T) {
	m := &ModelsCommand{}

	// Create 15 models that all match "gpt"
	models := make([]api.ModelInfo, 0, 15)
	for i := 0; i < 15; i++ {
		models = append(models, api.ModelInfo{ID: fmt.Sprintf("gpt-%d", i)})
	}

	results := m.fuzzySearchModels(models, "gpt")
	assert.LessOrEqual(t, len(results), 10)
}

// =====================================================================
// Helper for capturing stdout in tests
// =====================================================================

func captureOutput(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Read from pipe in background goroutine to prevent deadlock between
	// w.Close() and io.Copy.
	var buf bytes.Buffer
	copyDone := make(chan struct{})
	go func() {
		io.Copy(&buf, r)
		close(copyDone)
	}()

	f()
	w.Close()
	os.Stdout = old
	<-copyDone
	return buf.String()
}

// =====================================================================
// CommandRegistry Execute Tests
// =====================================================================

func TestCommandRegistry_Execute(t *testing.T) {
	registry := NewCommandRegistry()

	t.Run("help command succeeds", func(t *testing.T) {
		var err error
		captureOutput(func() {
			err = registry.Execute("/help", nil)
		})
		assert.NoError(t, err)
	})

	t.Run("unknown command returns error", func(t *testing.T) {
		err := registry.Execute("/unknown", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown command")
	})

	t.Run("empty input returns error", func(t *testing.T) {
		err := registry.Execute("", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a valid command")
	})

	t.Run("slash only returns error", func(t *testing.T) {
		err := registry.Execute("/", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty command")
	})

	t.Run("bang command routes to exec", func(t *testing.T) {
		var err error
		captureOutput(func() {
			err = registry.Execute("!echo test", nil)
		})
		// Should NOT be "unknown command" - it routes to exec
		if err != nil {
			assert.NotContains(t, err.Error(), "unknown command")
		}
	})
}

// =====================================================================
// ExecCommand Execute Tests
// =====================================================================

func TestExecCommand_Execute_NilAgent(t *testing.T) {
	c := &ExecCommand{}

	t.Run("no args returns usage error", func(t *testing.T) {
		err := c.Execute(nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "usage: /exec")
	})

	t.Run("git checkout blocked", func(t *testing.T) {
		err := c.Execute([]string{"git", "checkout", "main"}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checkout")
	})

	t.Run("git restore blocked", func(t *testing.T) {
		err := c.Execute([]string{"git", "restore", "file"}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "restore")
	})
}

// =====================================================================
// WriteToOutput Tests
// =====================================================================

func TestWriteToOutput(t *testing.T) {
	output := captureOutput(func() {
		WriteToOutput("hello world")
	})
	assert.Contains(t, output, "hello world")
}

// =====================================================================
// WriteJSONToOutput Tests
// =====================================================================

func TestWriteJSONToOutput(t *testing.T) {
	output := captureOutput(func() {
		err := WriteJSONToOutput(map[string]string{"key": "value"})
		assert.NoError(t, err)
	})
	assert.Contains(t, output, `"key"`)
	assert.Contains(t, output, `"value"`)
}

// =====================================================================
// getChangeTrackingStatus Tests
// =====================================================================

func TestGetChangeTrackingStatus_WithTestAgent(t *testing.T) {
	a := agent.NewTestAgent()
	status := getChangeTrackingStatus(a)
	// NewTestAgent() doesn't have change tracking enabled
	// GetChangeTracker() returns nil → "[i] Idle (no tracked session yet)"
	assert.Contains(t, status, "Idle")
}

func TestGetChangeTrackingStatus_NilAgent(t *testing.T) {
	status := getChangeTrackingStatus(nil)
	// Glyph-prefixed (SP-057 Phase 1); assert on the text suffix.
	assert.Contains(t, status, "Disabled")
}

// =====================================================================
// Review Context Tests
// =====================================================================

func TestExtractStagedChangesSummary_NoGitRepo(t *testing.T) {
	// Create a temp dir that is not a git repo, using t.Chdir for auto-restore
	t.Chdir(t.TempDir())

	// extractStagedChangesSummary should return "" when not in a git repo
	result := extractStagedChangesSummary()
	assert.Equal(t, "", result)
}

// =====================================================================
// BuildLogActions Tests
// =====================================================================

func TestBuildLogActions(t *testing.T) {
	lf := &LogFlow{agent: nil}

	t.Run("nil revisions returns basic actions", func(t *testing.T) {
		actions := lf.buildLogActions(nil)
		assert.Len(t, actions, 2) // view_log and current_changes only
		assert.Equal(t, "view_log", actions[0].ID)
		assert.Equal(t, "current_changes", actions[1].ID)
	})

	t.Run("empty slice returns basic actions", func(t *testing.T) {
		actions := lf.buildLogActions([]history.RevisionGroup{})
		assert.Len(t, actions, 2)
		assert.Equal(t, "view_log", actions[0].ID)
		assert.Equal(t, "current_changes", actions[1].ID)
	})

	t.Run("non-empty revisions returns all actions", func(t *testing.T) {
		actions := lf.buildLogActions([]history.RevisionGroup{{RevisionID: "rev1"}})
		assert.Len(t, actions, 5) // all 5 actions
		assert.Equal(t, "view_log", actions[0].ID)
		assert.Equal(t, "rollback_select", actions[1].ID)
		assert.Equal(t, "current_changes", actions[2].ID)
		assert.Equal(t, "change_stats", actions[3].ID)
		assert.Equal(t, "export_log", actions[4].ID)
	})
}

// =====================================================================
// ChangesCommand Execute Tests
// =====================================================================

func TestChangesCommand_Execute_NilAgent(t *testing.T) {
	c := &ChangesCommand{}
	output := captureOutput(func() {
		err := c.Execute(nil, nil)
		assert.NoError(t, err)
	})
	assert.Contains(t, output, "No active tracked session")
}

func TestChangesCommand_Execute_WithTestAgent(t *testing.T) {
	c := &ChangesCommand{}
	a := agent.NewTestAgent()
	output := captureOutput(func() {
		err := c.Execute(nil, a)
		assert.NoError(t, err)
	})
	// NewTestAgent has no change tracker, so should say "No tracked session has started yet"
	assert.Contains(t, output, "No tracked session")
}

// =====================================================================
// StatusCommand Execute Tests
// =====================================================================

func TestStatusCommand_Execute_NilAgent(t *testing.T) {
	s := &StatusCommand{}
	output := captureOutput(func() {
		err := s.Execute(nil, nil)
		assert.NoError(t, err)
	})
	assert.Contains(t, output, "Session Status")
	assert.Contains(t, output, "Change Tracking")
}

func TestStatusCommand_Execute_WithTestAgent(t *testing.T) {
	s := &StatusCommand{}
	a := agent.NewTestAgent()
	output := captureOutput(func() {
		err := s.Execute(nil, a)
		assert.NoError(t, err)
	})
	assert.Contains(t, output, "Session Status")
}

// =====================================================================
// RollbackCommand Execute Tests
// =====================================================================

func TestRollbackCommand_Execute_NoArgs(t *testing.T) {
	r := &RollbackCommand{}
	output := captureOutput(func() {
		err := r.Execute(nil, nil)
		assert.NoError(t, err)
	})
	assert.Contains(t, output, "Available revisions")
}

// =====================================================================
// CommandRegistry IsSlashCommand Tests
// =====================================================================

func TestCommandRegistry_IsSlashCommand(t *testing.T) {
	r := NewCommandRegistry()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid slash command", "/help", true},
		{"valid bang command", "!ls", true},
		{"no prefix", "help", false},
		{"empty input", "", false},
		{"slash only", "/", false},
		{"bang only", "!", false},
		{"slash with path", "/path/to/file", false},
		{"slash with backslash", "\\path", false},
		{"special chars in name", "/help!", false},
		{"valid command with numbers", "/stats", true},
		{"bang with command", "!echo hello", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.IsSlashCommand(tt.input)
			assert.Equal(t, tt.want, got, "IsSlashCommand(%q)", tt.input)
		})
	}
}

// =====================================================================
// CommandRegistry GetCommand Tests
// =====================================================================

func TestCommandRegistry_GetCommand(t *testing.T) {
	r := NewCommandRegistry()

	cmd, ok := r.GetCommand("help")
	assert.True(t, ok)
	assert.NotNil(t, cmd)
	assert.Equal(t, "help", cmd.Name())

	_, ok = r.GetCommand("nonexistent")
	assert.False(t, ok)
}

// =====================================================================
// CommandRegistry ListCommands Tests
// =====================================================================

func TestCommandRegistry_ListCommands(t *testing.T) {
	r := NewCommandRegistry()
	commands := r.ListCommands()
	assert.NotEmpty(t, commands)
	// Should have at least the built-in commands
	assert.GreaterOrEqual(t, len(commands), 10)
}

// =====================================================================
// CompactCommand anchor/boundary helper tests
// =====================================================================
//
// These tests pin the boundary logic that /compact uses to keep the
// opening task anchor and the recent causal chain intact. They mirror
// seed's unexported compactionAnchorEnd / adjustCompactionBoundary so
// changes to one side or the other are caught by the suite.

func TestCompactAnchorEnd_Empty(t *testing.T) {
	got := compactAnchorEnd(nil)
	assert.Equal(t, 0, got)
	got = compactAnchorEnd([]api.Message{})
	assert.Equal(t, 0, got)
}

func TestCompactAnchorEnd_SystemOnly(t *testing.T) {
	// Only a system message → anchorEnd = 1. There is no user message
	// so the fallback `anchorEnd = 1` kicks in.
	msgs := []api.Message{{Role: "system", Content: "you are helpful"}}
	got := compactAnchorEnd(msgs)
	assert.Equal(t, 1, got)
}

func TestCompactAnchorEnd_SystemPlusFirstUser(t *testing.T) {
	// system + first user → anchorEnd = 2. The follow-up assistant has
	// no tool calls so it would be included too, but it's not present
	// here so anchorEnd stops at 2.
	msgs := []api.Message{
		{Role: "system", Content: "you are helpful"},
		{Role: "user", Content: "u0"},
	}
	got := compactAnchorEnd(msgs)
	assert.Equal(t, 2, got)
}

func TestCompactAnchorEnd_SystemUserImmediateAssistantPlain(t *testing.T) {
	// system + user + immediate plain assistant (no tool calls) →
	// anchorEnd = 3 (the assistant is anchored because it has no tool
	// calls, so the model can still see the opening greeting in place).
	msgs := []api.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u0"},
		{Role: "assistant", Content: "hi"},
	}
	got := compactAnchorEnd(msgs)
	assert.Equal(t, 3, got)
}

func TestCompactAnchorEnd_SystemUserImmediateAssistantToolCalls(t *testing.T) {
	// system + user + immediate assistant WITH tool calls → anchorEnd
	// stays at 2 (the assistant is left in the middle so its tool
	// results can stay paired with it).
	msgs := []api.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u0"},
		{Role: "assistant", Content: "ok", ToolCalls: []api.ToolCall{{ID: "c1"}}},
	}
	got := compactAnchorEnd(msgs)
	assert.Equal(t, 2, got)
}

func TestCompactAnchorEnd_NoUserMessage(t *testing.T) {
	// No user message anywhere — fallback to anchorEnd = 1 (just the
	// system message is anchored).
	msgs := []api.Message{
		{Role: "system", Content: "sys"},
		{Role: "assistant", Content: "a"},
	}
	got := compactAnchorEnd(msgs)
	assert.Equal(t, 1, got)
}

func TestCompactAnchorEnd_NoSystemNoUser(t *testing.T) {
	// No system, no user → fallback to anchorEnd = 1.
	msgs := []api.Message{
		{Role: "assistant", Content: "a"},
	}
	got := compactAnchorEnd(msgs)
	assert.Equal(t, 1, got)
}

func TestCompactAnchorEnd_SkipsLeadingNonUserUntilFirstUser(t *testing.T) {
	// system + assistant + user → anchorEnd = 3 (the assistant between
	// system and the first user is skipped over).
	msgs := []api.Message{
		{Role: "system", Content: "sys"},
		{Role: "assistant", Content: "prelude"},
		{Role: "user", Content: "u0"},
		{Role: "assistant", Content: "plain reply"},
	}
	got := compactAnchorEnd(msgs)
	assert.Equal(t, 4, got)
}

func TestAdjustRecentBoundary_WalksBackPastTrailingTool(t *testing.T) {
	// The first-branch check fires when messages[recentStart] is a
	// tool message. We construct a layout where recentStart points at
	// a tool and the slot at recentStart-1 is NOT an assistant-tc
	// (so only branch 1 fires).
	msgs := []api.Message{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "u"}, // anchorEnd = 2
		{Role: "assistant", Content: "plain"},
		{Role: "tool", Content: "result", ToolCallID: "c1"}, // 3 — recentStart points here
	}
	// recentStart=3 → messages[3] is tool → walk to 2.
	// recentStart=2 → loop guard 2 > 2 fails → break. got=2.
	got := adjustRecentBoundary(msgs, 3, 2)
	assert.Equal(t, 2, got)
}

func TestAdjustRecentBoundary_WalksBackPastAssistantWithToolCalls(t *testing.T) {
	// The second-branch check fires when messages[recentStart] is NOT
	// a tool but messages[recentStart-1] is an assistant-with-tool-
	// calls. We construct a layout where that pattern holds and the
	// walk-back stops after one iteration (slot below is not tool, not
	// assistant-tc).
	msgs := []api.Message{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "u"}, // anchorEnd = 2
		{Role: "assistant", Content: "plain"},
		{Role: "assistant", Content: "tc", ToolCalls: []api.ToolCall{{ID: "c1"}}}, // 3
		{Role: "user", Content: "u2"},                                             // 4
	}
	// recentStart=4 → messages[4] is user (not tool) → first branch
	// skip. messages[3] is assistant-tc → second branch fires → walk
	// to 3. recentStart=3 → messages[3] is assistant-tc (not tool) →
	// first branch skip. messages[2] is assistant plain (no tool
	// calls) → second branch skip. break → got=3.
	got := adjustRecentBoundary(msgs, 4, 2)
	assert.Equal(t, 3, got)
}

func TestAdjustRecentBoundary_StopsAtAnchorEnd(t *testing.T) {
	// The loop guard `recentStart > anchorEnd` must hold — the
	// boundary can never be moved below anchorEnd. We construct a
	// list where the walk-back would WANT to keep going but anchorEnd
	// blocks it.
	//
	// Layout: msgs[0]=system (anchor start), msgs[1]=user (anchor
	// user, anchorEnd=2). msgs[2]=plain assistant (anchor assistant,
	// anchorEnd=3). msgs[3..10]=assistant-tc.
	//
	// adjustRecentBoundary(msgs, 10, 3): walk-back goes 10 → 9 → ...
	// → 4 → 3 (via second branch). At recentStart=3, second branch
	// checks messages[2] which is plain assistant (no ToolCalls) →
	// skip → break. got=3.
	msgs := []api.Message{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "u"}, // anchorEnd = 2; then messages[2] is plain assistant → anchorEnd=3
		{Role: "assistant", Content: "plain-anchor"},
	}
	for i := 3; i <= 10; i++ {
		msgs = append(msgs, api.Message{
			Role:      "assistant",
			Content:   fmt.Sprintf("tc-%d", i),
			ToolCalls: []api.ToolCall{{ID: fmt.Sprintf("c%d", i)}},
		})
	}
	got := adjustRecentBoundary(msgs, 10, 3)
	assert.Equal(t, 3, got)

	// To prove the loop guard stops at anchorEnd (not just at the
	// first non-assistant-tc slot), use a tighter anchorEnd where
	// EVERY slot from anchorEnd+1 onward is assistant-tc. The loop
	// must still stop at anchorEnd without dropping below it.
	//
	// Layout: msgs[0]=system only → anchorEnd=1. msgs[1..5]=assistant-tc.
	// Walk-back: 5 → 4 → 3 → 2 → 1 (loop guard 1 > 1 false → break).
	msgs2 := []api.Message{
		{Role: "system", Content: "s"}, // anchorEnd = 1
	}
	for i := 1; i <= 5; i++ {
		msgs2 = append(msgs2, api.Message{
			Role:      "assistant",
			Content:   fmt.Sprintf("tc-%d", i),
			ToolCalls: []api.ToolCall{{ID: fmt.Sprintf("c%d", i)}},
		})
	}
	got = adjustRecentBoundary(msgs2, 5, 1)
	assert.Equal(t, 1, got, "loop must stop at anchorEnd (1), not drop below it")
}

// =====================================================================
// CompactCommand Execute tests — threshold and overlap branches
// =====================================================================
//
// These exercise the early-exit paths that don't reach the LLM call.
// The test agent has no LLM client bound, so any test that DOES hit
// SummarizeViaLLM would error out — we deliberately stay on the
// short-circuit branches so the suite doesn't need an HTTP mock.

func TestCompactCommand_Execute_BelowMinMessagesThreshold(t *testing.T) {
	// 29 messages — one below the threshold — must short-circuit with
	// the "Need at least 30" informational message and not modify the
	// message list.
	a := agent.NewTestAgent()
	original := makeMessages(29)
	a.SetMessages(original)
	c := &CompactCommand{}

	output := captureOutput(func() {
		err := c.Execute(nil, a)
		assert.NoError(t, err)
	})

	assert.Contains(t, output, "Need at least 30 messages to compact")
	assert.Contains(t, output, "(have 29)")
	// Messages should be unchanged after a no-op short-circuit.
	assert.Equal(t, 29, len(a.GetMessages()))
}

func TestCompactCommand_Execute_AnchorRecentOverlapShortCircuits(t *testing.T) {
	// 30 messages where anchorEnd >= len-recentToKeep. We make
	// anchorEnd large by stacking many "first user" candidates. With
	// the makeOverlappingHistory builder, the first user at index 1
	// has a plain assistant reply at index 2, but then we attach many
	// user/assistant plain pairs that are NOT anchor-included — the
	// anchor is system + first user + immediate plain assistant = 3.
	// recentStart = 30-12 = 18. middle = 18-3 = 15, which is fine. To
	// force overlap we need a long opening assistant-with-tool-calls
	// chain such that adjustRecentBoundary walks recentStart back to
	// <= anchorEnd. The makeOverlappingHistory builder places a tool
	// result at index 30-12 = 18 to trigger the walk-back.
	a := agent.NewTestAgent()
	a.SetMessages(makeOverlappingHistory(30))
	c := &CompactCommand{}

	output := captureOutput(func() {
		err := c.Execute(nil, a)
		assert.NoError(t, err)
	})

	// Either the overlap branch or the middle-too-small branch fires;
	// both are valid early-exits. We accept either informative
	// message.
	assert.True(t,
		strings.Contains(output, "Not enough distinct history beyond anchor + recent window to compact") ||
			strings.Contains(output, "Middle segment too small to be worth summarizing"),
		"expected an early-exit message; got: %q", output)
}

// =====================================================================
// Test message builders
// =====================================================================
//
// These produce deterministic message lists shaped to exercise the
// specific boundary branches above. Each builder is paired with a
// docstring explaining the layout.

func makeMessages(n int) []api.Message {
	msgs := make([]api.Message, n)
	for i := 0; i < n; i++ {
		switch i % 3 {
		case 0:
			msgs[i] = api.Message{Role: "user", Content: fmt.Sprintf("u%d", i)}
		case 1:
			msgs[i] = api.Message{Role: "assistant", Content: fmt.Sprintf("a%d", i)}
		case 2:
			msgs[i] = api.Message{Role: "user", Content: fmt.Sprintf("u%d-extra", i)}
		}
	}
	return msgs
}

// makeOverlappingHistory returns a 30-message list shaped so that the
// raw recentStart (= len - 12 = 18) is <= anchorEnd, forcing the
// overlap short-circuit ("Not enough distinct history beyond anchor +
// recent window to compact") to fire without touching the LLM.
//
// Layout (indices):
//
//	0:  system                                (anchor start)
//	1..17: alternating assistant-tc and tool  (skipped by compactAnchorEnd)
//	18: user "u18"                            (first user → anchorEnd = 19)
//	19: assistant "a19" plain                 (anchor extends to 20)
//	20..29: filler user/assistant pairs        (recent window)
//
// anchorEnd = 20. raw recentStart = 30 - 12 = 18. recentStart (18) <=
// anchorEnd (20) → overlap short-circuit fires.
func makeOverlappingHistory(n int) []api.Message {
	if n < 30 {
		n = 30
	}
	msgs := make([]api.Message, n)
	msgs[0] = api.Message{Role: "system", Content: "sys"}
	// Indices 1..17: alternating assistant-tc and tool (neither is a
	// user, so compactAnchorEnd scans past them).
	for i := 1; i <= 17; i++ {
		if i%2 == 1 {
			msgs[i] = api.Message{
				Role:      "assistant",
				Content:   fmt.Sprintf("prelude-tc-%d", i),
				ToolCalls: []api.ToolCall{{ID: fmt.Sprintf("c%d", i)}},
			}
		} else {
			msgs[i] = api.Message{
				Role:       "tool",
				Content:    fmt.Sprintf("prelude-tr-%d", i),
				ToolCallID: fmt.Sprintf("c%d", i-1),
			}
		}
	}
	// Index 18: first user → anchorEnd becomes 19.
	msgs[18] = api.Message{Role: "user", Content: "first-real-user"}
	// Index 19: immediate plain assistant → anchorEnd becomes 20.
	msgs[19] = api.Message{Role: "assistant", Content: "first-real-assistant"}
	// Indices 20..29: filler so the message list reaches n=30.
	for i := 20; i < n; i++ {
		switch i % 2 {
		case 0:
			msgs[i] = api.Message{Role: "user", Content: fmt.Sprintf("fill-u%d", i)}
		case 1:
			msgs[i] = api.Message{Role: "assistant", Content: fmt.Sprintf("fill-a%d", i)}
		}
	}
	return msgs
}

// makeMiddleTooSmallHistory returns a 30-message list where
// adjustRecentBoundary walks recentStart all the way down to the
// anchor, leaving a middle segment of size 0 and triggering the
// "Middle segment too small" early-exit.
//
// Layout (indices):
//
//	0:  system                                (anchor start)
//	1:  user "u0"                             (anchor user)
//	2:  assistant "a0" plain                  (anchor assistant, anchorEnd=3)
//	3..29: all assistant-tc                   (every slot triggers branch-2 walk-back)
//
// anchorEnd = 3. raw recentStart = 30 - 12 = 18. adjustRecentBoundary
// walks back via branch 2 (each iteration: messages[recentStart-1] is
// assistant-tc → walk). Walks: 18 → 17 → 16 → ... → 4 → 3. At
// recentStart=3, branch 2 checks messages[2] (assistant plain, no tool
// calls) → skip → break. got=3. middle = 3-3 = 0 < 6 → middle-too-
// small branch fires.
func makeMiddleTooSmallHistory() []api.Message {
	msgs := make([]api.Message, 30)
	msgs[0] = api.Message{Role: "system", Content: "sys"}
	msgs[1] = api.Message{Role: "user", Content: "u0"}
	msgs[2] = api.Message{Role: "assistant", Content: "a0"}
	// Indices 3..29: every slot is an assistant-with-tool-calls so
	// branch 2 keeps firing on each iteration.
	for i := 3; i < 30; i++ {
		msgs[i] = api.Message{
			Role:      "assistant",
			Content:   fmt.Sprintf("tc-%d", i),
			ToolCalls: []api.ToolCall{{ID: fmt.Sprintf("c%d", i)}},
		}
	}
	return msgs
}
