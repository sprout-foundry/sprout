package agent

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestWithWorkspaceContext(t *testing.T) {
	ag := newTestAgent(t)
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

// A prompt that replaced the embedded one never carried the workspace context, so its
// conversations get no block either.
func TestWithWorkspaceContextReplacedPrompt(t *testing.T) {
	ag := newTestAgent(t)
	if err := ag.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.SystemPromptText = "You are a custom agent."
		return nil
	}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if got := ag.withWorkspaceContext("fix the bug"); got != "fix the bug" {
		t.Fatalf("system_prompt_text override got the block: %q", got)
	}

	persona := newTestAgent(t)
	persona.personaReplacedPrompt = true
	if got := persona.withWorkspaceContext("fix the bug"); got != "fix the bug" {
		t.Fatalf("a persona's own prompt got the block: %q", got)
	}
	persona.ClearActivePersona()
	if got := persona.withWorkspaceContext("fix the bug"); !strings.HasPrefix(got, "<workspace-context>") {
		t.Fatalf("clearing the persona did not restore the block: %q", got)
	}
}

func TestSystemPromptOmitsWorkspaceContext(t *testing.T) {
	setupMemoryDirForTest(t)
	root := t.TempDir()
	builders := map[string]func() (string, error){
		"embedded": GetEmbeddedSystemPrompt,
		"full": func() (string, error) {
			return GetEmbeddedSystemPromptForProfile(configuration.ContextProfile{Mode: configuration.ContextModeFull}, "", 200_000, root)
		},
		"lite": func() (string, error) {
			return GetEmbeddedSystemPromptForProfile(configuration.ContextProfile{Mode: configuration.ContextModeLowContext}, "", 32_000, root)
		},
	}
	for name, build := range builders {
		prompt, err := build()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(prompt, "## Current Working Directory") || strings.Contains(prompt, root) {
			t.Errorf("%s: system prompt carries the working directory", name)
		}
	}
}

func TestStripUserMessageEnvelope(t *testing.T) {
	stamped := InjectUserMessageTimestamp("<workspace-context>\n## Current Working Directory\n\n`/w`\n</workspace-context>\n\nhola, arregla el error")
	if got := StripUserMessageEnvelope(stamped); got != "hola, arregla el error" {
		t.Fatalf("StripUserMessageEnvelope = %q", got)
	}
	if got := StripUserMessageTimestamp(stamped); !strings.HasPrefix(got, "<workspace-context>") {
		t.Fatalf("StripUserMessageTimestamp dropped the block persisted state keeps: %q", got)
	}
	if got := StripUserMessageEnvelope(InjectUserMessageTimestamp("plain")); got != "plain" {
		t.Fatalf("a message without a block = %q", got)
	}
}
