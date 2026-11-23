package agent

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestWithWorkspaceContext(t *testing.T) {
	ag := newTestAgent(t)
	if got := ag.withWorkspaceContext("fix the bug"); got != "fix the bug" {
		t.Fatalf("default placement changed the query: %q", got)
	}

	if err := ag.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.WorkspaceContextPlacement = "user"
		return nil
	}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	first := ag.withWorkspaceContext("fix the bug")
	if !strings.HasPrefix(first, "<workspace-context>\n") || !strings.HasSuffix(first, "</workspace-context>\n\nfix the bug") {
		t.Fatalf("first message lacks the workspace block:\n%s", first)
	}
	if !strings.Contains(first, "## Current Working Directory") {
		t.Fatalf("workspace block lacks the working directory:\n%s", first)
	}

	ag.AddMessage(api.Message{Role: "user", Content: first})
	if got := ag.withWorkspaceContext("and the next one"); got != "and the next one" {
		t.Fatalf("a later turn got the block again: %q", got)
	}
	if got := ag.withWorkspaceContext("   "); got != "   " {
		t.Fatalf("an empty query changed: %q", got)
	}
}

func TestSystemPromptWithoutWorkspaceContext(t *testing.T) {
	setupMemoryDirForTest(t)
	profile, err := configuration.ResolveContextProfile(&configuration.Config{
		ContextMode:               configuration.ContextModeFull,
		WorkspaceContextPlacement: "user",
	}, 200_000, false)
	if err != nil {
		t.Fatalf("ResolveContextProfile: %v", err)
	}
	if !profile.WorkspaceContextInUserMessage {
		t.Fatal("placement \"user\" did not reach the profile")
	}
	root := t.TempDir()
	moved, err := GetEmbeddedSystemPromptForProfile(profile, "", 200_000, root)
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if strings.Contains(moved, "## Current Working Directory") || strings.Contains(moved, root) {
		t.Fatalf("system prompt still carries the working directory")
	}
	profile.WorkspaceContextInUserMessage = false
	kept, err := GetEmbeddedSystemPromptForProfile(profile, "", 200_000, root)
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if !strings.Contains(kept, "## Current Working Directory") || !strings.HasPrefix(kept, moved[:len(moved)/2]) {
		t.Fatalf("default placement changed the system prompt")
	}
}
