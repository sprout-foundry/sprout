package commands

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
)

func TestPersonaCommandSmoke(t *testing.T) {
	chatAgent, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	cmd := &PersonaCommand{}

	if err := cmd.Execute([]string{"list"}, chatAgent); err != nil {
		t.Fatalf("persona list failed: %v", err)
	}
	if err := cmd.Execute([]string{"web-scraper", "show"}, chatAgent); err != nil {
		t.Fatalf("persona show failed: %v", err)
	}
	if err := cmd.Execute([]string{"web-scraper"}, chatAgent); err != nil {
		t.Fatalf("persona apply failed: %v", err)
	}
	if got := chatAgent.GetActivePersona(); got != "web_scraper" {
		t.Fatalf("expected active persona web_scraper, got %q", got)
	}
}
