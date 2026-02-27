package commands

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/personas"
)

func TestParseCommaList(t *testing.T) {
	items := parseCommaList(" read_file,search_files, read_file ,,TodoRead ")
	if len(items) != 3 {
		t.Fatalf("expected 3 unique items, got %d", len(items))
	}
	if items[0] != "read_file" || items[1] != "search_files" || items[2] != "TodoRead" {
		t.Fatalf("unexpected parsed items: %#v", items)
	}
}

func TestNormalizePersonaKey(t *testing.T) {
	if got := normalizePersonaKey("Web-Scraper "); got != "web_scraper" {
		t.Fatalf("expected web_scraper, got %q", got)
	}
}

func TestBuildCustomPersonaTemplateUsesGeneralDefaults(t *testing.T) {
	template := buildCustomPersonaTemplate("my_custom_persona")
	definitions, err := personas.DefaultDefinitions()
	if err != nil {
		t.Fatalf("failed to load default definitions: %v", err)
	}
	general, ok := definitions["general"]
	if !ok {
		t.Fatalf("expected general definition to exist")
	}

	if template.SystemPrompt != general.SystemPrompt {
		t.Fatalf("expected prompt %q, got %q", general.SystemPrompt, template.SystemPrompt)
	}
	if len(template.AllowedTools) != len(general.AllowedTools) {
		t.Fatalf("expected %d tools, got %d", len(general.AllowedTools), len(template.AllowedTools))
	}
	for i, tool := range general.AllowedTools {
		if template.AllowedTools[i] != tool {
			t.Fatalf("expected tool[%d]=%q, got %q", i, tool, template.AllowedTools[i])
		}
	}
}
