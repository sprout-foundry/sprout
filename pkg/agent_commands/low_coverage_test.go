package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"
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
// StatsCommand Tests
// =====================================================================

func TestStatsCommand_Name(t *testing.T) {
	s := &StatsCommand{}
	assert.Equal(t, "stats", s.Name())
}

func TestStatsCommand_Description(t *testing.T) {
	s := &StatsCommand{}
	assert.Equal(t, "Show detailed conversation summary and token usage", s.Description())
}

func TestStatsCommand_Execute_WithTestAgent(t *testing.T) {
	a := agent.NewTestAgent()
	s := &StatsCommand{}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := s.Execute(nil, a)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	assert.NoError(t, err)
	assert.Contains(t, output, "Detailed Conversation Summary")
}

func TestStatsCommand_Execute_NilAgent(t *testing.T) {
	s := &StatsCommand{}

	// NOTE: Documents a known issue — StatsCommand.Execute panics with nil agent
	// because it calls chatAgent.PrintConversationSummary without a nil check.
	assert.Panics(t, func() {
		s.Execute(nil, nil)
	})
}

// =====================================================================
// IsShellCommand Tests
// =====================================================================

func TestIsShellCommand(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		want   bool
	}{
		{"ls -la", "ls -la", true},
		{"git status", "git status", true},
		{"go test ./...", "go test ./...", true},
		{"make build", "make build", true},
		{"docker ps", "docker ps", true},
		{"hello world", "hello world", false},
		{"empty string", "", false},
		{"single ls", "ls", true},
		{"single git", "git", true},
		{"cd /tmp", "cd /tmp", true},
		{"cat file.txt", "cat file.txt", true},
		{"echo hello", "echo hello", true},
		{"npm install", "npm install", true},
		{"python script.py", "python script.py", true},
		{"cargo build", "cargo build", true},
		{"node app.js", "node app.js", true},
		{"curl http://example.com", "curl http://example.com", true},
		{"vim file.go", "vim file.go", true},
		{"rm -rf temp", "rm -rf temp", true},
		{"touch file.txt", "touch file.txt", true},
		{"chmod 755 script.sh", "chmod 755 script.sh", true},
		{"pwd", "pwd", true},
		{"whoami", "whoami", true},
		{"date", "date", true},
		{"sleep 1", "sleep 1", true},
		{"watch ls", "watch ls", true},
		{"tar -czf archive.tar.gz dir", "tar -czf archive.tar.gz dir", true},
		{"gzip file.txt", "gzip file.txt", true},
		{"find . -name *.go", "find . -name *.go", true},
		{"grep -r func", "grep -r func", true},
		{"awk print", "awk '{print $1}'", true},
		{"sed -i replace", "sed -i 's/old/new/'", true},
		{"diff file1 file2", "diff file1 file2", true},
		{"test -f file", "test -f file", true},
		{"true", "true", true},
		{"false", "false", true},
		{"yes", "yes", true},
		{"env", "env", true},
		{"export VAR=value", "export VAR=value", true},
		{"source .env", "source .env", true},
		{"alias ll=ls", "alias ll=ls", true},
		{"systemctl status nginx", "systemctl status nginx", true},
		{"apt-get update", "apt-get update", true},
		{"brew install go", "brew install go", true},
		{"yarn build", "yarn build", true},
		{"pip install flask", "pip install flask", true},
		{"pip3 install flask", "pip3 install flask", true},
		{"kubectl get pods", "kubectl get pods", true},
		{"gcc main.c", "gcc main.c", true},
		{"rustc main.rs", "rustc main.rs", true},
		{"node", "node", true},
		{"deno run main.ts", "deno run main.ts", true},
		{"ssh user@host", "ssh user@host", true},
		{"scp file.txt user@host:~/", "scp file.txt user@host:~/", true},
		{"rsync -avz dir/ dest/", "rsync -avz dir/ dest/", true},
		{"sqlite3 db.sqlite", "sqlite3 db.sqlite", true},
		{"redis-cli ping", "redis-cli ping", true},
		{"jq . data.json", "jq . data.json", true},
		{"rg pattern", "rg pattern", true},
		{"fd . src", "fd . src", true},
		{"lsusb", "lsusb", true},
		{"lscpu", "lscpu", true},
		{"df -h", "df -h", true},
		{"du -sh .", "du -sh .", true},
		{"free -m", "free -m", true},
		{"uptime", "uptime", true},
		{"cal", "cal", true},
		{"ps aux", "ps aux", true},
		{"top", "top", true},
		{"htop", "htop", true},
		{"head -n 10 file.txt", "head -n 10 file.txt", true},
		{"tail -f /var/log/syslog", "tail -f /var/log/syslog", true},
		{"less file.txt", "less file.txt", true},
		{"more file.txt", "more file.txt", true},
		{"man ls", "man ls", true},
		{"which ls", "which ls", true},
		{"id", "id", true},
		{"groups", "groups", true},
		{"sudo ls", "sudo ls", true},
		{"ping 8.8.8.8", "ping 8.8.8.8", true},
		{"curl -v http://example.com", "curl -v http://example.com", true},
		{"wget http://example.com/file", "wget http://example.com/file", true},
		{"file myfile", "file myfile", true},
		{"stat myfile", "stat myfile", true},
		{"ln -s target link", "ln -s target link", true},
		{"crontab -l", "crontab -l", true},
		{"dmesg", "dmesg", true},
		{"lsof", "lsof", true},
		{"strace ls", "strace ls", true},
		{"gcc", "gcc", true},
		{"go", "go", true},
		{"non-matching text", "this is just some text", false},
		{"sentence with ls in middle", "there is a ls in this sentence", false},
		{"whitespace only", "   ", false},
		{"starts with prefix but not command", "list items", false},
		{"go command prefix match", "go build", true},
		{"golang is not a prefix", "golang build", false},
		{"docker with space", "docker run nginx", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsShellCommand(tt.prompt)
			assert.Equal(t, tt.want, got, "IsShellCommand(%q)", tt.prompt)
		})
	}
}

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
	assert.Equal(t, "Summarize prior conversation via the LLM and replace it with the recap, preserving the most recent user turn", c.Description())
}

func TestCompactCommand_Execute_NilAgent(t *testing.T) {
	c := &CompactCommand{}
	err := c.Execute(nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "agent not available")
}

func TestCompactCommand_Execute_NotEnoughHistory(t *testing.T) {
	// A fresh test agent has no conversation history. /compact should
	// short-circuit with an informational message rather than calling
	// the LLM summarizer with nothing to summarize.
	a := agent.NewTestAgent()
	c := &CompactCommand{}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := c.Execute(nil, a)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	assert.NoError(t, err)
	assert.Contains(t, output, "Not enough conversation history to compact")
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
// IsShellCommand Additional Edge Cases
// =====================================================================

func TestIsShellCommand_Additional(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		want   bool
	}{
		{"npm run build", "npm run build", true},
		{"python3 script.py", "python3 script.py", true},
		{"whitespace trimmed ls", "  ls  ", true},
		{"unknown binary no match", "unknownbinary", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsShellCommand(tt.prompt)
			assert.Equal(t, tt.want, got, "IsShellCommand(%q)", tt.prompt)
		})
	}
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
