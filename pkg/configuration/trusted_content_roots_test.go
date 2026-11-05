package configuration

import (
	"encoding/json"
	"testing"
)

// TestTrustedContentRootsRoundTrip pins the JSON shape of the SP-169 trust
// store: per absolute workspace root, then root-relative path → third-party
// flag. The key is user-visible (the file is hand-editable) so it must stay
// stable, and omitempty must not collapse a populated map.
func TestTrustedContentRootsRoundTrip(t *testing.T) {
	cfg := Config{
		TrustedContentRoots: map[string]map[string]bool{
			"/home/dev/project": {
				".":            false,
				"vendor/acme":  true,
				"third_party":  true,
				"our/internal": false,
			},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !contains(string(data), `"trusted_content_roots"`) {
		t.Fatalf("trusted_content_roots missing from JSON: %s", data)
	}

	var roundTripped Config
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	repo := roundTripped.TrustedContentRoots["/home/dev/project"]
	if len(repo) != 4 {
		t.Fatalf("after round-trip: got %d decisions, want 4: %v", len(repo), repo)
	}
	if repo["vendor/acme"] != true {
		t.Errorf("vendor/acme: got %v, want true", repo["vendor/acme"])
	}
	if repo["our/internal"] != false {
		t.Errorf("our/internal: got %v, want false (explicit first-party)", repo["our/internal"])
	}
	if repo["."] != false {
		t.Errorf("root decision: got %v, want false", repo["."])
	}
}

// TestTrustedContentRootsEmptyOmitted keeps an unclassified machine from
// writing an empty object into every config file.
func TestTrustedContentRootsEmptyOmitted(t *testing.T) {
	cfg := Config{}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if contains(string(data), "trusted_content_roots") {
		t.Errorf("empty trust store should be omitted via omitempty, got: %s", data)
	}
}

// TestTrustedContentRootsNewConfigInitialized guards the constructor: a nil
// map would panic on the first `trust set` write.
func TestTrustedContentRootsNewConfigInitialized(t *testing.T) {
	cfg := NewConfig()
	if cfg.TrustedContentRoots == nil {
		t.Fatal("NewConfig().TrustedContentRoots is nil; first write would panic")
	}
	cfg.TrustedContentRoots["/tmp/repo"] = map[string]bool{"vendor/acme": true}
	if !cfg.TrustedContentRoots["/tmp/repo"]["vendor/acme"] {
		t.Error("write into the initialized map did not stick")
	}
}
