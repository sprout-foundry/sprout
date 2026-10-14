package commands

// /economy command tests: show, on/off toggle, and completion.

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// economyCmdAgent builds an agent with a worker role distinct from the coder
// role so the command's output names a real route.
func economyCmdAgent(t *testing.T) *agent.Agent {
	t.Helper()
	a, err := agent.NewAgentWithModel("")
	if err != nil {
		t.Fatalf("NewAgentWithModel failed: %v", err)
	}
	t.Cleanup(func() { a.Shutdown() })
	if err := a.GetConfigManager().UpdateConfigNoSave(func(c *configuration.Config) error {
		c.Roles = map[string]configuration.RoleConfig{
			configuration.RoleCoder:  {Provider: "openrouter", Model: "coder-model"},
			configuration.RoleWorker: {Provider: "zai", Model: "cheap-model"},
		}
		return nil
	}); err != nil {
		t.Fatalf("set roles: %v", err)
	}
	return a
}

func TestEconomyCommand_Name(t *testing.T) {
	if got := (&EconomyCommand{}).Name(); got != "economy" {
		t.Errorf("Name() = %q, want %q", got, "economy")
	}
}

func TestEconomyCommand_Toggles(t *testing.T) {
	a := economyCmdAgent(t)
	cmd := &EconomyCommand{}

	if a.GetEconomyMode() {
		t.Fatal("economy mode must default off")
	}
	if err := cmd.Execute([]string{"on"}, a); err != nil {
		t.Fatalf("on: %v", err)
	}
	if !a.GetEconomyMode() {
		t.Fatal("/economy on must enable economy mode")
	}
	if err := cmd.Execute([]string{"off"}, a); err != nil {
		t.Fatalf("off: %v", err)
	}
	if a.GetEconomyMode() {
		t.Fatal("/economy off must disable economy mode")
	}
}

func TestEconomyCommand_ShowAndUnknownArg(t *testing.T) {
	a := economyCmdAgent(t)
	cmd := &EconomyCommand{}

	if err := cmd.Execute(nil, a); err != nil {
		t.Fatalf("show: %v", err)
	}
	err := cmd.Execute([]string{"bogus"}, a)
	if err == nil {
		t.Fatal("an unknown argument must be rejected")
	}
	if !strings.Contains(err.Error(), "on, off, show") {
		t.Errorf("error should name the valid arguments, got %q", err.Error())
	}
}

func TestEconomyCommand_Complete(t *testing.T) {
	cmd := &EconomyCommand{}
	got := cmd.Complete(nil, nil)
	for _, want := range []string{"on", "off", "show"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("completion missing %q (got %v)", want, got)
		}
	}
	if matches := cmd.Complete([]string{"o"}, nil); len(matches) != 2 {
		t.Errorf("prefix 'o' should match on/off, got %v", matches)
	}
}
