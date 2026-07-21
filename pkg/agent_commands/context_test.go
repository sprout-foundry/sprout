package commands

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// newAgentForContextTest builds an agent against an isolated config dir so
// /context's disk writes never reach the real config file. Mirrors the SP-125
// integration test setup. Uses a 128K mock so the default resolved mode is
// "full" (no LCM auto-detection) — the tests then set/clear the field
// explicitly and read it back.
func newAgentForContextTest(t *testing.T) *agent.Agent {
	t.Helper()
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()
	client := agent.NewMockLLMProviderWithLimit(128_000)
	chatAgent, err := agent.NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient failed: %v", err)
	}
	t.Cleanup(func() { chatAgent.Shutdown() })
	return chatAgent
}

func TestContextCommand_NameAndDescription(t *testing.T) {
	cmd := &ContextCommand{}
	if got := cmd.Name(); got != "context" {
		t.Errorf("Name() = %q, want %q", got, "context")
	}
	if got := cmd.Description(); !strings.Contains(got, "context mode") {
		t.Errorf("Description() = %q, want it to mention 'context mode'", got)
	}
	if got := cmd.Usage(); !strings.Contains(got, "/context") {
		t.Errorf("Usage() missing command invocation example: %q", got)
	}
	if got := cmd.Usage(); !strings.Contains(got, "low_context") {
		t.Errorf("Usage() should document the low_context mode, got: %q", got)
	}
}

func TestContextCommand_SafeDuringSteer(t *testing.T) {
	cmd := &ContextCommand{}
	if !cmd.SafeDuringSteer() {
		t.Error("/context is a config change and should be safe during steer")
	}
}

func TestContextCommand_ExecuteNilAgent(t *testing.T) {
	cmd := &ContextCommand{}
	if err := cmd.Execute(nil, nil); err == nil {
		t.Error("expected error for nil agent")
	}
}

func TestContextCommand_ExecuteShowDoesNotError(t *testing.T) {
	chatAgent := newAgentForContextTest(t)
	cmd := &ContextCommand{}

	if err := cmd.Execute(nil, chatAgent); err != nil {
		t.Errorf("show (no args) failed: %v", err)
	}
	if err := cmd.Execute([]string{"show"}, chatAgent); err != nil {
		t.Errorf("show (explicit) failed: %v", err)
	}
}

func TestContextCommand_ExecuteSetFull(t *testing.T) {
	chatAgent := newAgentForContextTest(t)
	cmd := &ContextCommand{}

	if err := cmd.Execute([]string{"full"}, chatAgent); err != nil {
		t.Fatalf("set full failed: %v", err)
	}
	got := chatAgent.GetConfig().ContextMode
	if got != configuration.ContextModeFull {
		t.Errorf("after /context full: config.context_mode = %q, want %q", got, configuration.ContextModeFull)
	}
}

func TestContextCommand_ExecuteSetLowAliases(t *testing.T) {
	aliases := []string{"low", "low_context", "low-context", "lcm"}
	for _, alias := range aliases {
		t.Run(alias, func(t *testing.T) {
			chatAgent := newAgentForContextTest(t)
			cmd := &ContextCommand{}

			if err := cmd.Execute([]string{alias}, chatAgent); err != nil {
				t.Fatalf("set %q failed: %v", alias, err)
			}
			got := chatAgent.GetConfig().ContextMode
			if got != configuration.ContextModeLowContext {
				t.Errorf("after /context %q: config.context_mode = %q, want %q", alias, got, configuration.ContextModeLowContext)
			}
		})
	}
}

func TestContextCommand_ExecuteClear(t *testing.T) {
	clears := []string{"auto", "clear", "default"}
	for _, arg := range clears {
		t.Run(arg, func(t *testing.T) {
			chatAgent := newAgentForContextTest(t)
			cmd := &ContextCommand{}

			// Set first so we know clear has something to clear.
			if err := cmd.Execute([]string{"low"}, chatAgent); err != nil {
				t.Fatalf("precondition set low failed: %v", err)
			}
			if err := cmd.Execute([]string{arg}, chatAgent); err != nil {
				t.Fatalf("clear %q failed: %v", arg, err)
			}
			got := chatAgent.GetConfig().ContextMode
			if got != "" {
				t.Errorf("after /context %q: config.context_mode = %q, want empty", arg, got)
			}
		})
	}
}

func TestContextCommand_ExecuteUnknownMode(t *testing.T) {
	chatAgent := newAgentForContextTest(t)
	cmd := &ContextCommand{}

	err := cmd.Execute([]string{"bogus"}, chatAgent)
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
	if !strings.Contains(err.Error(), "unknown mode") {
		t.Errorf("error should mention 'unknown mode', got: %q", err.Error())
	}
	// Must NOT have mutated config on the error path.
	if got := chatAgent.GetConfig().ContextMode; got != "" {
		t.Errorf("config.context_mode mutated by rejected command: %q", got)
	}
}

func TestContextCommand_Complete(t *testing.T) {
	cmd := &ContextCommand{}

	// Empty prefix returns all subcommands.
	all := cmd.Complete(nil, nil)
	if len(all) == 0 {
		t.Error("Complete() with no args should return subcommands")
	}

	cases := []struct {
		prefix string
		want   string // must appear in results
	}{
		{"fu", "full"},
		{"low", "low"},
		{"low_", "low_context"},
		{"au", "auto"},
		{"cl", "clear"},
		{"xyz", ""}, // no match
	}
	for _, tc := range cases {
		t.Run(tc.prefix, func(t *testing.T) {
			results := cmd.Complete([]string{tc.prefix}, nil)
			if tc.want == "" {
				if len(results) != 0 {
					t.Errorf("prefix %q: expected no matches, got %v", tc.prefix, results)
				}
				return
			}
			found := false
			for _, r := range results {
				if r == tc.want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("prefix %q: expected %q in results, got %v", tc.prefix, tc.want, results)
			}
		})
	}
}
