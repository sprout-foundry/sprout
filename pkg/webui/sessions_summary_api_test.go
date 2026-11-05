//go:build !js

package webui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// writeSummaryListSession stores a session with the given state and returns
// its working directory.
func writeSummaryListSession(t *testing.T, sessionID string, state *agent.ConversationState) string {
	t.Helper()
	stateDir := t.TempDir()
	workingDir := t.TempDir()
	restore := agent.SetStateDirFuncForTesting(func() (string, error) { return stateDir, nil })
	t.Cleanup(restore)
	state.WorkingDirectory = workingDir
	if err := agent.WriteTestSessionFile(stateDir, sessionID, workingDir, state); err != nil {
		t.Fatal(err)
	}
	return workingDir
}

func summaryTestServer(t *testing.T) *ReactWebServer {
	t.Helper()
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func sessionListSummary(t *testing.T, server *ReactWebServer, infos []agent.SessionInfo) string {
	t.Helper()
	list := server.buildSessionList(infos)
	if len(list) != 1 {
		t.Fatalf("expected 1 session in list, got %d", len(list))
	}
	summary, _ := list[0]["topic_summary"].(string)
	return summary
}

func TestBuildSessionListCarriesStoredTopicSummary(t *testing.T) {
	server := summaryTestServer(t)
	workingDir := writeSummaryListSession(t, "web-stored", &agent.ConversationState{
		TopicSummary: "Migrated the auth module to JWT",
		Name:         "auth migration",
	})

	got := sessionListSummary(t, server, []agent.SessionInfo{{
		SessionID:        "web-stored",
		WorkingDirectory: workingDir,
		TopicSummary:     "Migrated the auth module to JWT",
	}})
	if got != "Migrated the auth module to JWT" {
		t.Errorf("topic_summary = %q, want the stored summary", got)
	}
}

func TestBuildSessionListLazilyResolvesTemplateForOldSessions(t *testing.T) {
	server := summaryTestServer(t)
	workingDir := writeSummaryListSession(t, "web-old", &agent.ConversationState{
		TaskActions: []agent.TaskAction{{Type: "file_modified", Details: "/p/handler.go"}},
	})

	got := sessionListSummary(t, server, []agent.SessionInfo{{
		SessionID:        "web-old",
		WorkingDirectory: workingDir,
	}})
	if got != "Updated handler.go" {
		t.Errorf("topic_summary = %q, want the lazily-computed template", got)
	}
}

// TestCLIAndWebUIRenderSameSummary pins the cross-surface agreement: the
// picker's detail line and the API's topic_summary both come from the same
// pkg/agent resolver over the same session file.
func TestCLIAndWebUIRenderSameSummary(t *testing.T) {
	server := summaryTestServer(t)
	workingDir := writeSummaryListSession(t, "web-same", &agent.ConversationState{
		TaskActions: []agent.TaskAction{{Type: "file_created", Details: "/p/exporter.go"}},
		Messages:    []api.Message{{Role: "user", Content: "add an exporter"}},
	})

	info := agent.SessionInfo{SessionID: "web-same", WorkingDirectory: workingDir}
	webSummary := sessionListSummary(t, server, []agent.SessionInfo{info})
	cliSummary := agent.SessionTopicSummaryForDisplay(info)

	if webSummary == "" {
		t.Fatal("expected a non-empty web summary")
	}
	if webSummary != cliSummary {
		t.Errorf("web %q != cli %q", webSummary, cliSummary)
	}
}
