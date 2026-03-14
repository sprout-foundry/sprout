package agent

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestCollapseSystemMessagesToFrontMovesLateSystemMessages(t *testing.T) {
	messages := []api.Message{
		{Role: "system", Content: "base instructions"},
		{Role: "user", Content: "first request"},
		{Role: "assistant", Content: "working"},
		{Role: "system", Content: "[Skill Activated: test]\n\nextra instructions"},
		{Role: "user", Content: "continue"},
	}

	got := collapseSystemMessagesToFront(messages)

	if len(got) != 4 {
		t.Fatalf("expected 4 messages after collapsing systems, got %d", len(got))
	}
	if got[0].Role != "system" {
		t.Fatalf("expected first message to remain system, got %q", got[0].Role)
	}
	wantContent := "base instructions\n\n[Skill Activated: test]\n\nextra instructions"
	if got[0].Content != wantContent {
		t.Fatalf("unexpected merged system content:\nwant: %q\ngot:  %q", wantContent, got[0].Content)
	}
	if got[1].Role != "user" || got[1].Content != "first request" {
		t.Fatalf("expected first non-system message to be preserved, got %#v", got[1])
	}
	if got[2].Role != "assistant" || got[2].Content != "working" {
		t.Fatalf("expected assistant message to be preserved, got %#v", got[2])
	}
	if got[3].Role != "user" || got[3].Content != "continue" {
		t.Fatalf("expected trailing user message to be preserved, got %#v", got[3])
	}
}

func TestCollapseSystemMessagesToFrontLeavesValidMessagesUntouched(t *testing.T) {
	messages := []api.Message{
		{Role: "system", Content: "base instructions"},
		{Role: "user", Content: "request"},
	}

	got := collapseSystemMessagesToFront(messages)

	if len(got) != len(messages) {
		t.Fatalf("expected message count to remain %d, got %d", len(messages), len(got))
	}
	for i := range messages {
		if got[i].Role != messages[i].Role || got[i].Content != messages[i].Content {
			t.Fatalf("message %d changed unexpectedly: want %#v got %#v", i, messages[i], got[i])
		}
	}
}
