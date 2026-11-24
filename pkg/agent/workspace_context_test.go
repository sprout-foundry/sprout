package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
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

// TestCrossSessionPromptPrefixStable guards issue #178: the stable prompt
// prefix — the system message and the tools array — must be byte-identical
// across sessions. Two sessions in different workspaces, with a memory created
// and edited between them, must send the same prefix; the per-session variance
// lives in the first user message (workspace + memory block), not the prefix.
// Hashes are sha256 over the serialized JSON so the assertion is exact.
func TestCrossSessionPromptPrefixStable(t *testing.T) {
	// Both sessions share one config/memory dir, like two sessions started
	// from the same ~/.sprout.
	configDir := t.TempDir() + "/.sprout"
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("GIT_EDITOR", "true")
	t.Setenv("GIT_SEQUENCE_EDITOR", "true")
	t.Setenv("GIT_MERGE_AUTOEDIT", "no")
	mgr, err := configuration.NewManagerWithDir(configDir)
	if err != nil {
		t.Fatalf("NewManagerWithDir: %v", err)
	}
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.SkipPrompt = true
		return nil
	}); err != nil {
		t.Fatalf("set SkipPrompt: %v", err)
	}
	newSession := func(folder string) *Agent {
		t.Helper()
		ag, err := NewAgentWithModel("test:test")
		if err != nil {
			t.Fatalf("NewAgentWithModel: %v", err)
		}
		// Replace the internally-built manager with the shared one so both
		// sessions read and write the same memory store.
		ag.configManager = mgr
		ag.SetProjectFolder(folder)
		return ag
	}

	// Build the request the agent would send: the system message the client
	// puts first and the tool roster offered alongside it.
	type requestPrefix struct {
		systemHash string
		toolsHash  string
		firstMsg   string
	}
	buildRequest := func(ag *Agent) requestPrefix {
		t.Helper()
		sysMessage := map[string]string{"role": "system", "content": ag.GetSystemPrompt()}
		sysJSON, err := json.Marshal(sysMessage)
		if err != nil {
			t.Fatalf("marshal system message: %v", err)
		}
		tools := ag.getOptimizedToolDefinitions(nil)
		toolsJSON, err := json.Marshal(tools)
		if err != nil {
			t.Fatalf("marshal tools: %v", err)
		}
		return requestPrefix{
			systemHash: fmt.Sprintf("%x", sha256.Sum256(sysJSON)),
			toolsHash:  fmt.Sprintf("%x", sha256.Sum256(toolsJSON)),
			firstMsg:   ag.withWorkspaceContext("fix the bug"),
		}
	}

	// Session 1: empty memory store, first working directory.
	session1 := newSession(t.TempDir())
	req1 := buildRequest(session1)

	// A memory is created — and then edited — between the two sessions.
	if err := SaveMemory("alpha", "# Alpha\nfirst version."); err != nil {
		t.Fatalf("SaveMemory alpha: %v", err)
	}
	if err := SaveMemory("alpha", "# Alpha\nedited body."); err != nil {
		t.Fatalf("edit SaveMemory alpha: %v", err)
	}

	// Session 2: same config, second working directory, after the edit.
	session2 := newSession(t.TempDir())
	req2 := buildRequest(session2)

	if !strings.Contains(req2.firstMsg, "`alpha`") {
		t.Fatalf("second session's first message lacks the memory created between sessions:\n%s", req2.firstMsg)
	}
	if strings.Contains(req1.firstMsg, "`alpha`") {
		t.Fatalf("session 1's first message leaks a memory created after it started:\n%s", req1.firstMsg)
	}
	if req1.firstMsg == req2.firstMsg {
		t.Fatalf("first user messages are identical across workspaces and memory stores:\n%s", req1.firstMsg)
	}

	// The stable prefix must be byte-identical across sessions.
	if req1.systemHash != req2.systemHash {
		t.Errorf("system message differs across sessions:\nfirst:  %s\nsecond: %s", req1.systemHash, req2.systemHash)
	}
	if req1.toolsHash != req2.toolsHash {
		t.Errorf("tools array differs across sessions:\nfirst:  %s\nsecond: %s", req1.toolsHash, req2.toolsHash)
	}
}
