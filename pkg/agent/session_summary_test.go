package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// fakeSummaryClient is a topicSummaryModelClient whose behavior each test
// controls: a canned reply, an error, or a record of the prompt it received.
type fakeSummaryClient struct {
	reply  string
	err    error
	prompt string
}

func (f *fakeSummaryClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	f.prompt = userPrompt
	if f.err != nil {
		return "", f.err
	}
	return f.reply, nil
}

// summaryTestAgent returns a bare agent with messages and task actions set
// and its workspace root pointed at a plan-free temp dir, so the plan-goal
// signal is deterministically absent.
func summaryTestAgent(t *testing.T, messages []api.Message, actions []TaskAction) *Agent {
	t.Helper()
	a := &Agent{state: NewAgentStateManager(false)}
	a.state.SetMessages(messages)
	a.replaceTaskActions(actions)
	a.SetWorkspaceRoot(t.TempDir())
	return a
}

// withTopicSummaryClient swaps the client-creation seam for the duration of
// the test.
func withTopicSummaryClient(t *testing.T, fn func(*Agent) topicSummaryModelClient) {
	t.Helper()
	original := newTopicSummaryClientForTesting
	newTopicSummaryClientForTesting = fn
	t.Cleanup(func() { newTopicSummaryClientForTesting = original })
}

func TestTopicSummaryDescribesTopicNotFirstMessage(t *testing.T) {
	a := summaryTestAgent(t,
		[]api.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "On it.", ToolCalls: []api.ToolCall{
				{Function: api.ToolCallFunction{Name: "write_file"}},
			}},
			{Role: "tool", Content: "Wrote /tmp/project/parser.go"},
			{Role: "assistant", Content: "Done."},
		},
		[]TaskAction{
			{Type: "file_created", Description: "Created/updated file: /tmp/project/parser.go", Details: "/tmp/project/parser.go"},
			{Type: "file_modified", Description: "Modified file: /tmp/project/main.go", Details: "/tmp/project/main.go"},
		},
	)

	summary := a.generateTopicSummary(context.Background())

	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
	if strings.Contains(strings.ToLower(summary), "hi") {
		t.Errorf("summary %q should not be the first message", summary)
	}
	if !strings.Contains(summary, "parser.go") {
		t.Errorf("summary %q should describe the file work", summary)
	}
}

func TestTopicSummaryTemplateFromSessionSignals(t *testing.T) {
	a := summaryTestAgent(t,
		[]api.Message{
			{Role: "user", Content: "please refactor the parser"},
			{Role: "assistant", Content: "Working.", ToolCalls: []api.ToolCall{
				{Function: api.ToolCallFunction{Name: "read_file"}},
				{Function: api.ToolCallFunction{Name: "write_file"}},
			}},
		},
		[]TaskAction{
			{Type: "file_modified", Description: "Modified file: /p/parser.go", Details: "/p/parser.go"},
			{Type: "command_executed", Description: "Executed: go test ./...", Details: "go test ./..."},
		},
	)
	withTopicSummaryClient(t, func(*Agent) topicSummaryModelClient { return nil })

	summary := a.generateTopicSummary(context.Background())

	want := "Updated parser.go — Ran go test ./..."
	if summary != want {
		t.Errorf("summary = %q, want %q", summary, want)
	}
}

func TestTopicSummaryModelFallbackToTemplateOnModelError(t *testing.T) {
	a := summaryTestAgent(t,
		[]api.Message{{Role: "user", Content: "fix the flaky test"}},
		[]TaskAction{
			{Type: "file_modified", Description: "Modified file: /p/flaky_test.go", Details: "/p/flaky_test.go"},
		},
	)
	withTopicSummaryClient(t, func(*Agent) topicSummaryModelClient {
		return &fakeSummaryClient{err: errors.New("provider down")}
	})

	summary := a.generateTopicSummary(context.Background())

	if summary != "Updated flaky_test.go" {
		t.Errorf("summary = %q, want the template fallback", summary)
	}
}

func TestTopicSummaryModelWrittenWhenSummarizerAnswers(t *testing.T) {
	a := summaryTestAgent(t,
		[]api.Message{
			{Role: "user", Content: "migrate the auth module to jwt"},
			{Role: "assistant", Content: "Starting the migration."},
		},
		nil,
	)
	withTopicSummaryClient(t, func(*Agent) topicSummaryModelClient {
		return &fakeSummaryClient{reply: "Migrated the auth module to JWT authentication"}
	})

	summary := a.generateTopicSummary(context.Background())

	if summary != "Migrated the auth module to JWT authentication" {
		t.Errorf("summary = %q, want the model-written line", summary)
	}
}

func TestTopicSummaryModelEmptyReplyFallsBackToTemplate(t *testing.T) {
	a := summaryTestAgent(t,
		[]api.Message{{Role: "user", Content: "do the thing"}},
		[]TaskAction{
			{Type: "file_created", Description: "Created file: /p/thing.go", Details: "/p/thing.go"},
		},
	)
	withTopicSummaryClient(t, func(*Agent) topicSummaryModelClient {
		return &fakeSummaryClient{reply: "   "}
	})

	summary := a.generateTopicSummary(context.Background())

	if summary != "Created thing.go" {
		t.Errorf("summary = %q, want the template fallback", summary)
	}
}

func TestTopicSummaryCappedAtTargetLength(t *testing.T) {
	long := strings.Repeat("word ", 60)
	a := summaryTestAgent(t,
		[]api.Message{{Role: "user", Content: long}},
		nil,
	)
	withTopicSummaryClient(t, func(*Agent) topicSummaryModelClient {
		return &fakeSummaryClient{reply: long}
	})

	summary := a.generateTopicSummary(context.Background())

	if got := len([]rune(summary)); got > topicSummaryMaxRunes {
		t.Errorf("summary length = %d runes, want <= %d", got, topicSummaryMaxRunes)
	}
}

func TestGenerateTopicSummaryPromptCarriesConversation(t *testing.T) {
	a := summaryTestAgent(t,
		[]api.Message{
			{Role: "user", Content: "<current-time>2026-01-01T00:00:00Z</current-time>\n\nfix the login bug"},
			{Role: "assistant", Content: "Looking.", ToolCalls: []api.ToolCall{
				{Function: api.ToolCallFunction{Name: "read_file"}},
			}},
			{Role: "tool", Content: "read login.go, 200 lines"},
		},
		nil,
	)
	client := &fakeSummaryClient{reply: "whatever"}
	withTopicSummaryClient(t, func(*Agent) topicSummaryModelClient { return client })

	_ = a.generateTopicSummary(context.Background())

	for _, want := range []string{"fix the login bug", "[assistant/tools] read_file", "read login.go"} {
		if !strings.Contains(client.prompt, want) {
			t.Errorf("prompt %q missing %q", client.prompt, want)
		}
	}
	if strings.Contains(client.prompt, "<current-time>") {
		t.Errorf("prompt should strip the timestamp envelope: %q", client.prompt)
	}
}

func TestWriteTopicSummaryTimeoutFallsBack(t *testing.T) {
	originalTimeout := topicSummaryTimeout
	topicSummaryTimeout = 20 * time.Millisecond
	t.Cleanup(func() { topicSummaryTimeout = originalTimeout })

	slow := slowSummaryClient{delay: 200 * time.Millisecond}
	if got := writeTopicSummary(context.Background(), slow, []api.Message{{Role: "user", Content: "hello"}}); got != "" {
		t.Errorf("writeTopicSummary = %q, want empty on timeout", got)
	}
}

type slowSummaryClient struct{ delay time.Duration }

func (s slowSummaryClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(s.delay):
		return "too late", nil
	}
}

func TestTopicSummaryNewClientNilWithoutSummarizerRole(t *testing.T) {
	a := newTestAgent(t)
	if got := a.newTopicSummaryClient(); got != nil {
		t.Errorf("newTopicSummaryClient() = %v, want nil with no summarizer role configured", got)
	}
}

// === storage + resolution ===

func writeSummaryTestSession(t *testing.T, sessionID string, state *ConversationState) (stateDir, workingDir string) {
	t.Helper()
	stateDir = t.TempDir()
	workingDir = t.TempDir()
	restore := SetStateDirFuncForTesting(func() (string, error) { return stateDir, nil })
	t.Cleanup(restore)
	state.WorkingDirectory = workingDir
	if err := WriteTestSessionFile(stateDir, sessionID, workingDir, state); err != nil {
		t.Fatal(err)
	}
	return stateDir, workingDir
}

func TestResolveSessionTopicSummaryPrefersStored(t *testing.T) {
	got := ResolveSessionTopicSummary(&ConversationState{
		TopicSummary: "Refactored the persistence layer",
		Messages:     []api.Message{{Role: "user", Content: "refactor persistence"}},
	})
	if got != "Refactored the persistence layer" {
		t.Errorf("ResolveSessionTopicSummary() = %q, want the stored summary", got)
	}
}

func TestResolveSessionTopicSummaryTemplateWhenAbsent(t *testing.T) {
	workingDir := t.TempDir()
	got := ResolveSessionTopicSummary(&ConversationState{
		Messages:         []api.Message{{Role: "user", Content: "hello"}},
		TaskActions:      []TaskAction{{Type: "file_created", Details: "/p/app.ts"}},
		WorkingDirectory: workingDir,
	})
	if got != "Created app.ts" {
		t.Errorf("ResolveSessionTopicSummary() = %q, want the template", got)
	}
}

func TestResolveSessionTopicSummaryFallsBackToPreviewForLegacySession(t *testing.T) {
	// A session saved before the summary field existed: no TopicSummary, no
	// work signals — resolution must degrade to the first-message preview.
	_, workingDir := writeSummaryTestSession(t, "legacy", &ConversationState{
		Messages: []api.Message{{Role: "user", Content: "just a question about fonts"}},
	})

	got := SessionTopicSummaryForDisplayScoped("legacy", workingDir)
	if got != "just a question about fonts" {
		t.Errorf("SessionTopicSummaryForDisplayScoped() = %q, want the first-message fallback", got)
	}
}

func TestSessionTopicSummaryForDisplayUsesStoredFromList(t *testing.T) {
	_, workingDir := writeSummaryTestSession(t, "stored", &ConversationState{
		TopicSummary: "Fixed the resize handle",
		Messages:     []api.Message{{Role: "user", Content: "fix the resize handle"}},
	})

	got := SessionTopicSummaryForDisplay(SessionInfo{SessionID: "stored", WorkingDirectory: workingDir, TopicSummary: "Fixed the resize handle"})
	if got != "Fixed the resize handle" {
		t.Errorf("SessionTopicSummaryForDisplay() = %q, want the stored summary", got)
	}
}

func TestSessionTopicSummaryForDisplayLoadsTemplateForOldSessions(t *testing.T) {
	_, workingDir := writeSummaryTestSession(t, "old", &ConversationState{
		Messages:    []api.Message{{Role: "user", Content: "kick off"}},
		TaskActions: []TaskAction{{Type: "file_modified", Details: "/p/README.md"}},
	})

	got := SessionTopicSummaryForDisplay(SessionInfo{SessionID: "old", WorkingDirectory: workingDir})
	if got != "Updated README.md" {
		t.Errorf("SessionTopicSummaryForDisplay() = %q, want the lazily-computed template", got)
	}
}

func TestSessionInfoCarriesTopicSummary(t *testing.T) {
	_, workingDir := writeSummaryTestSession(t, "listed", &ConversationState{
		TopicSummary:     "Migrated config to TOML",
		SessionID:        "listed",
		Messages:         []api.Message{{Role: "user", Content: "migrate config"}},
		LastUpdated:      time.Now(),
		WorkingDirectory: "",
	})

	sessions, err := ListSessionsWithTimestampsScoped(workingDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].TopicSummary != "Migrated config to TOML" {
		t.Errorf("SessionInfo.TopicSummary = %q, want the stored summary", sessions[0].TopicSummary)
	}
}

func TestGetSessionLabelScopedPrefersName(t *testing.T) {
	_, workingDir := writeSummaryTestSession(t, "named", &ConversationState{
		Name:     "My Session",
		Messages: []api.Message{{Role: "user", Content: strings.Repeat("a", 120)}},
	})

	if got := GetSessionLabelScoped("named", workingDir); got != "My Session" {
		t.Errorf("GetSessionLabelScoped() = %q, want the stored name", got)
	}
}

func TestGetSessionLabelScopedUncappedPreview(t *testing.T) {
	long := strings.Repeat("b", 120)
	_, workingDir := writeSummaryTestSession(t, "unrenamed", &ConversationState{
		Messages: []api.Message{{Role: "user", Content: long}},
	})

	got := GetSessionLabelScoped("unrenamed", workingDir)
	if len(got) != 120 {
		t.Errorf("GetSessionLabelScoped() length = %d, want the uncapped 120-rune message", len(got))
	}
}

func TestGetSessionPreviewScopedStillCapsAt50(t *testing.T) {
	long := strings.Repeat("c", 120)
	_, workingDir := writeSummaryTestSession(t, "capped", &ConversationState{
		Messages: []api.Message{{Role: "user", Content: long}},
	})

	got := GetSessionPreviewScoped("capped", workingDir)
	if !strings.HasSuffix(got, "...") || len(got) != 53 {
		t.Errorf("GetSessionPreviewScoped() = %d chars, want the 50-rune cap with ellipsis", len(got))
	}
}

func TestTopicSummaryPlanGoalSignal(t *testing.T) {
	root := t.TempDir()
	sproutDir := filepath.Join(root, ".sprout")
	if err := os.MkdirAll(sproutDir, 0o755); err != nil {
		t.Fatal(err)
	}
	planJSON := `{"version":1,"revision":1,"created":"2026-01-01T00:00:00Z","updated":"2026-01-01T00:00:00Z","goal":"Ship the export pipeline","scope":[{"id":"s1","title":"Export"}],"steps":[{"scope":"s1","description":"Do it"}],"acceptance":[{"id":"a1","scope":"s1","check":"go test ./...","kind":"test"}]}`
	if err := os.WriteFile(filepath.Join(sproutDir, "plan.json"), []byte(planJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	a := summaryTestAgent(t, []api.Message{{Role: "user", Content: "continue"}}, nil)
	a.SetWorkspaceRoot(root)
	withTopicSummaryClient(t, func(*Agent) topicSummaryModelClient { return nil })

	got := a.generateTopicSummary(context.Background())
	if !strings.Contains(got, "Ship the export pipeline") {
		t.Errorf("summary = %q, want the plan goal", got)
	}
}

func TestTemplateToolsOnlyFallback(t *testing.T) {
	s := topicSignalsFromConversation(
		[]api.Message{{Role: "assistant", ToolCalls: []api.ToolCall{
			{Function: api.ToolCallFunction{Name: "web_search"}},
			{Function: api.ToolCallFunction{Name: "web_search"}},
			{Function: api.ToolCallFunction{Name: "read_file"}},
		}}},
		nil, "")
	got := s.template()
	if got != "Used web_search, read_file" {
		t.Errorf("template() = %q, want the tools-only phrasing", got)
	}
}
