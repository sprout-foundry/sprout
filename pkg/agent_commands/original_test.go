package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// metaKeyOriginal is the Meta key the language guard writes the mismatched
// reply under. It is unexported in package agent; the test stamps the same
// literal so the fixture mirrors what the guard actually stores.
const metaKeyOriginal = "language_guard_original"

func TestOriginalCommand_Name(t *testing.T) {
	cmd := &OriginalCommand{}
	if got := cmd.Name(); got != "original" {
		t.Errorf("OriginalCommand.Name() = %q, want \"original\"", got)
	}
}

func TestOriginalCommand_Description(t *testing.T) {
	cmd := &OriginalCommand{}
	if strings.TrimSpace(cmd.Description()) == "" {
		t.Error("OriginalCommand.Description() returned an empty string")
	}
}

func TestOriginalCommand_Usage(t *testing.T) {
	cmd := &OriginalCommand{}
	usage := cmd.Usage()
	if !strings.Contains(usage, "/original") {
		t.Errorf("OriginalCommand.Usage() = %q; expected to mention /original", usage)
	}
}

func TestOriginalCommand_SafeDuringSteer(t *testing.T) {
	cmd := &OriginalCommand{}
	if !cmd.SafeDuringSteer() {
		t.Error("OriginalCommand is read-only and must be safe during steer")
	}
}

func TestOriginalCommand_ExecuteWithNilAgent(t *testing.T) {
	cmd := &OriginalCommand{}
	if err := cmd.Execute(nil, nil); err == nil {
		t.Error("OriginalCommand.Execute() with nil agent should return error")
	}
}

// TestOriginalCommand_PrintsMostRecentOriginal builds a fixture agent whose
// transcript carries the Meta key the guard writes, plus an older and a
// newer assistant message without it, and pins that the command prints the
// ORIGINAL text (not the replacement content) and the newest one at that.
func TestOriginalCommand_PrintsMostRecentOriginal(t *testing.T) {
	chatAgent := agent.NewTestAgent()
	older := api.Message{Role: "assistant", Content: "repaired reply"}
	older.SetMeta(metaKeyOriginal, "older wrong-language reply")
	newest := api.Message{Role: "assistant", Content: "notice"}
	newest.SetMeta(metaKeyOriginal, "newest wrong-language reply")
	chatAgent.SetMessages([]api.Message{
		{Role: "user", Content: "hola"},
		older,
		{Role: "user", Content: "otra pregunta"},
		newest,
	})

	var out bytes.Buffer
	cmd := &OriginalCommand{}
	cmd.SetOutput(&out)
	if err := cmd.Execute(nil, chatAgent); err != nil {
		t.Fatalf("OriginalCommand.Execute() error = %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "newest wrong-language reply") {
		t.Errorf("output %q does not carry the most recent original", got)
	}
	if strings.Contains(got, "older wrong-language reply") {
		t.Errorf("output %q carries a stale original", got)
	}
	if strings.Contains(got, "notice") {
		t.Errorf("output %q must not print the replacement content", got)
	}
}

// TestOriginalCommand_NoOriginalInSession pins the empty case: a plain
// conversation (and an assistant message without the Meta key) prints the
// nothing-to-show notice and returns nil.
func TestOriginalCommand_NoOriginalInSession(t *testing.T) {
	chatAgent := agent.NewTestAgent()
	chatAgent.SetMessages([]api.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "a normal reply"},
	})

	var out bytes.Buffer
	cmd := &OriginalCommand{}
	cmd.SetOutput(&out)
	if err := cmd.Execute(nil, chatAgent); err != nil {
		t.Fatalf("OriginalCommand.Execute() error = %v", err)
	}
	got := out.String()
	if strings.Contains(got, "a normal reply") {
		t.Errorf("output %q must not print ordinary assistant content", got)
	}
	if !strings.Contains(strings.ToLower(got), "no language-corrected reply") {
		t.Errorf("output %q does not carry the nothing-to-show notice", got)
	}
}

// TestOriginalCommand_RegisteredWithAlias pins the registry wiring: /original
// resolves and its `orig` alias resolves to the same command.
func TestOriginalCommand_RegisteredWithAlias(t *testing.T) {
	registry := NewCommandRegistry()
	cmd, ok := registry.GetCommand("original")
	if !ok {
		t.Fatal("/original is not registered")
	}
	if _, ok := cmd.(*OriginalCommand); !ok {
		t.Errorf("GetCommand(\"original\") returned %T, want *OriginalCommand", cmd)
	}
	aliased, ok := registry.GetCommand("orig")
	if !ok {
		t.Fatal("alias /orig is not registered")
	}
	if _, ok := aliased.(*OriginalCommand); !ok {
		t.Errorf("GetCommand(\"orig\") returned %T, want *OriginalCommand", aliased)
	}
}
