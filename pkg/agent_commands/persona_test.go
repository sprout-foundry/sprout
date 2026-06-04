package commands

import "testing"

func TestNormalizePersonaKey(t *testing.T) {
	if got := normalizePersonaKey("Web-Scraper "); got != "web_scraper" {
		t.Fatalf("expected web_scraper, got %q", got)
	}
}
