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

// TestMemoryBlockInFirstUserMessage covers the move of memories out of the system prompt:
// after a memory is created, the system prompt carries no memory text, the first user message
// does, and two sessions started with different memory sets produce byte-identical system
// prompts.
func TestMemoryBlockInFirstUserMessage(t *testing.T) {
	first := newTestAgent(t)
	if err := SaveMemory("alpha", "# Alpha\nPrefer table-driven tests."); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}
	firstFirst := first.withWorkspaceContext("fix the bug")
	if !strings.Contains(firstFirst, "## Memories") || !strings.Contains(firstFirst, "`alpha`") {
		t.Fatalf("first user message lacks the memory block:\n%s", firstFirst)
	}
	second := newTestAgent(t)
	if err := SaveMemory("beta", "# Beta\nUse merge, never rebase."); err != nil {
		t.Fatalf("SaveMemory beta: %v", err)
	}
	secondFirst := second.withWorkspaceContext("fix the bug")
	if !strings.Contains(secondFirst, "`beta`") {
		t.Fatalf("second session's first message lacks beta:\n%s", secondFirst)
	}
	if strings.Contains(secondFirst, "`alpha`") {
		t.Fatalf("second session's first message leaked the first session's memory:\n%s", secondFirst)
	}
	// The memory block is the only session-varying content of the first message.
	if firstFirst == secondFirst {
		t.Fatalf("first messages of two sessions with different memory sets are identical:\n%s", firstFirst)
	}

	// Both system prompts must be byte-identical and memory-free. The memory block is not
	// built for the system prompt at all any more.
	firstPrompt, err := GetEmbeddedSystemPromptForProfile(configuration.ContextProfile{Mode: configuration.ContextModeFull}, "", 200_000, first.currentProjectFolder())
	if err != nil {
		t.Fatalf("GetEmbeddedSystemPromptForProfile (first): %v", err)
	}
	secondPrompt, err := GetEmbeddedSystemPromptForProfile(configuration.ContextProfile{Mode: configuration.ContextModeFull}, "", 200_000, second.currentProjectFolder())
	if err != nil {
		t.Fatalf("GetEmbeddedSystemPromptForProfile (second): %v", err)
	}
	if firstPrompt != secondPrompt {
		t.Fatalf("system prompts differ across sessions with different memory sets:\nfirst:\n%s\nsecond:\n%s", firstPrompt, secondPrompt)
	}
	if strings.Contains(firstPrompt, "## Memories") || strings.Contains(firstPrompt, "alpha") || strings.Contains(firstPrompt, "beta") {
		t.Fatalf("profile system prompt carries memory text:\n%s", firstPrompt)
	}
	prompt, err := GetEmbeddedSystemPrompt()
	if err != nil {
		t.Fatalf("GetEmbeddedSystemPrompt: %v", err)
	}
	if strings.Contains(prompt, "## Memories") || strings.Contains(prompt, "alpha") || strings.Contains(prompt, "beta") {
		t.Fatalf("embedded system prompt carries memory text:\n%s", prompt)
	}
}

// TestMemoryBlockStableAcrossAgentStart: the memory block is stamped once at the first
// message; a mid-session memory write must not change a session's already-stamped message.
func TestMemoryBlockStableAcrossAgentStart(t *testing.T) {
	ag := newTestAgent(t)
	if err := SaveMemory("alpha", "# Alpha\nAlpha body."); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}
	stamped := ag.withWorkspaceContext("fix the bug")
	if err := SaveMemory("beta", "# Beta\nBeta body."); err != nil {
		t.Fatalf("SaveMemory beta: %v", err)
	}
	if strings.Contains(stamped, "beta") {
		t.Fatalf("mid-session memory write leaked into the stamped first message:\n%s", stamped)
	}
	// A later turn must not re-add the block.
	ag.AddMessage(api.Message{Role: "user", Content: stamped})
	if got := ag.withWorkspaceContext("and the next one"); got != "and the next one" {
		t.Fatalf("a later turn re-prefixed the message: %q", got)
	}
}

// TestNoMemoriesNoBlock: an agent with an empty memory store gets no stray
// "## Memories" heading in the first user message.
func TestNoMemoriesNoBlock(t *testing.T) {
	ag := newTestAgent(t)
	first := ag.withWorkspaceContext("fix the bug")
	if strings.Contains(first, "## Memories") {
		t.Fatalf("first user message carries a stray memory block:\n%s", first)
	}
	if !strings.HasSuffix(first, "</workspace-context>\n\nfix the bug") {
		t.Fatalf("workspace block should still be stamped:\n%s", first)
	}
}
