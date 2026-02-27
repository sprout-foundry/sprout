package cmd

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
)

func TestAvailablePersonaCompletions(t *testing.T) {
	cfg := &configuration.Config{
		SubagentTypes: map[string]configuration.SubagentType{
			"web_scraper": {ID: "web_scraper", Enabled: true},
			"coder":       {ID: "coder", Enabled: true},
			"debugger":    {ID: "debugger", Enabled: false},
		},
	}

	all := availablePersonaCompletions(cfg, "")
	if len(all) != 2 {
		t.Fatalf("expected 2 enabled persona completions, got %d (%v)", len(all), all)
	}
	if all[0] != "coder" || all[1] != "web_scraper" {
		t.Fatalf("unexpected completion order/content: %v", all)
	}

	filtered := availablePersonaCompletions(cfg, "web")
	if len(filtered) != 1 || filtered[0] != "web_scraper" {
		t.Fatalf("unexpected filtered completions: %v", filtered)
	}
}
