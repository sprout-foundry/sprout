package personas

import "testing"

func TestDefaultDefinitionsIncludesWebScraper(t *testing.T) {
	definitions, err := DefaultDefinitions()
	if err != nil {
		t.Fatalf("expected embedded persona configs to load, got error: %v", err)
	}

	webScraper, exists := definitions["web_scraper"]
	if !exists {
		t.Fatalf("expected web_scraper in default persona definitions")
	}
	if !webScraper.Enabled {
		t.Fatalf("expected web_scraper to be enabled")
	}
	if len(webScraper.AllowedTools) == 0 {
		t.Fatalf("expected web_scraper to define allowed tools")
	}
}

func TestDefaultDefinitionsIncludesOrchestratorAndComputerUser(t *testing.T) {
	definitions, err := DefaultDefinitions()
	if err != nil {
		t.Fatalf("expected embedded persona configs to load, got error: %v", err)
	}

	if _, exists := definitions["orchestrator"]; !exists {
		t.Fatalf("expected orchestrator in default persona definitions")
	}
	if _, exists := definitions["computer_user"]; !exists {
		t.Fatalf("expected computer_user in default persona definitions")
	}
}

func TestDefaultDefinitionsCloneIsolation(t *testing.T) {
	first, _ := DefaultDefinitions()
	second, _ := DefaultDefinitions()

	first["general"] = Definition{}
	if second["general"].ID == "" {
		t.Fatalf("expected cloned definitions; mutation should not leak across callers")
	}
}
