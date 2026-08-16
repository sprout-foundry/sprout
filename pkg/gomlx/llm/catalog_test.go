package llm

import (
	"os"
	"path/filepath"
	"testing"
)

const testGB = 1024 * 1024 * 1024

// TestSelectModelForRAM checks auto-selection picks the largest installed,
// fitting SUGGESTED model — never a risky stretch pick. Stretch models are
// only reachable via explicit user selection (SelectableForRAM), not
// automatic RAM-based resolution.
func TestSelectModelForRAM(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"gemma-4-e2b-it-4bit", "qwen3.5-4b-4bit", "qwen3.5-9b-4bit"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name string
		ram  uint64
		want string // expected Dir basename, or "" for error
	}{
		{"1gb", 1 * testGB, "gemma-4-e2b-it-4bit"},
		{"4gb", 4 * testGB, "gemma-4-e2b-it-4bit"},
		// 8GB: qwen3.5-4b is only a STRETCH pick here (MinRAMSelect=8GB,
		// MinRAMSuggested=16GB) — auto-selection must not pick it.
		{"8gb", 8 * testGB, "gemma-4-e2b-it-4bit"},
		{"16gb", 16 * testGB, "qwen3.5-4b-4bit"},
		{"24gb", 24 * testGB, "qwen3.5-9b-4bit"},
		{"32gb", 32 * testGB, "qwen3.5-9b-4bit"}, // 35b-a3b never auto-selected
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := SelectModelForRAM(root, tc.ram)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if filepath.Base(m.Dir) != tc.want {
				t.Fatalf("got %s, want %s", filepath.Base(m.Dir), tc.want)
			}
		})
	}
}

// TestCatalogThresholdsMonotonic ensures each catalog entry's MinRAMSelect
// and MinRAMSuggested are both >= the previous entry's, when sorted by
// MinRAMSuggested — the tier walk in TieredCatalogForRAM depends on this.
func TestCatalogThresholdsMonotonic(t *testing.T) {
	sorted := sortedCatalog()
	for i := 1; i < len(sorted); i++ {
		if sorted[i].MinRAMSelect < sorted[i-1].MinRAMSelect {
			t.Fatalf("MinRAMSelect not monotonic: %s (%d) after %s (%d)",
				sorted[i].Name, sorted[i].MinRAMSelect, sorted[i-1].Name, sorted[i-1].MinRAMSelect)
		}
	}
}

// TestRecommendModelForRAM checks the pure-RAM recommendation (the safe
// "suggested" tier only).
func TestRecommendModelForRAM(t *testing.T) {
	cases := []struct {
		ram  uint64
		want string
	}{
		{1 * testGB, "gemma4-e2b"},
		{4 * testGB, "gemma4-e2b"},
		{8 * testGB, "gemma4-e2b"},
		{16 * testGB, "qwen3.5-4b"},
		{24 * testGB, "qwen3.5-9b"},
		{32 * testGB, "qwen3.5-9b"},
		{128 * testGB, "qwen3.5-9b"}, // 35b-a3b is never the unwarned default
	}
	for _, tc := range cases {
		m := RecommendModelForRAM(tc.ram)
		if m == nil || m.Name != tc.want {
			t.Fatalf("RecommendModelForRAM(%d) = %+v, want %s", tc.ram, m, tc.want)
		}
		if m.HFRepo == "" {
			t.Fatalf("catalog entry %s missing HFRepo", m.Name)
		}
	}
}

// TestTieredCatalogForRAM locks in the exact suggested/eligible/stretch/
// blocked matrix requested: <8GB suggested=gemma4-e2b no stretch; 8-16GB
// suggested=gemma4-e2b stretch=qwen3.5-4b; 16-24GB suggested=qwen3.5-4b
// (gemma4-e2b now a safe downgrade, not blocked) stretch=qwen3.5-9b;
// 24-32GB suggested=qwen3.5-9b no stretch; 32GB+ suggested=qwen3.5-9b
// stretch=qwen3.6-35b-a3b. Anything smaller than suggested is always a
// selectable (eligible) downgrade, never blocked — only tiers beyond the
// one-up stretch are genuinely blocked.
func TestTieredCatalogForRAM(t *testing.T) {
	cases := []struct {
		name   string
		ram    uint64
		expect map[string]TierStatus
	}{
		{"4gb", 4 * testGB, map[string]TierStatus{
			"gemma4-e2b": TierSuggested, "qwen3.5-4b": TierBlocked, "qwen3.5-9b": TierBlocked, "qwen3.6-35b-a3b": TierBlocked,
		}},
		{"8gb", 8 * testGB, map[string]TierStatus{
			"gemma4-e2b": TierSuggested, "qwen3.5-4b": TierStretch, "qwen3.5-9b": TierBlocked, "qwen3.6-35b-a3b": TierBlocked,
		}},
		{"12gb", 12 * testGB, map[string]TierStatus{
			"gemma4-e2b": TierSuggested, "qwen3.5-4b": TierStretch, "qwen3.5-9b": TierBlocked, "qwen3.6-35b-a3b": TierBlocked,
		}},
		{"16gb", 16 * testGB, map[string]TierStatus{
			"gemma4-e2b": TierEligible, "qwen3.5-4b": TierSuggested, "qwen3.5-9b": TierStretch, "qwen3.6-35b-a3b": TierBlocked,
		}},
		{"20gb", 20 * testGB, map[string]TierStatus{
			"gemma4-e2b": TierEligible, "qwen3.5-4b": TierSuggested, "qwen3.5-9b": TierStretch, "qwen3.6-35b-a3b": TierBlocked,
		}},
		{"24gb", 24 * testGB, map[string]TierStatus{
			"gemma4-e2b": TierEligible, "qwen3.5-4b": TierEligible, "qwen3.5-9b": TierSuggested, "qwen3.6-35b-a3b": TierBlocked,
		}},
		{"28gb", 28 * testGB, map[string]TierStatus{
			"gemma4-e2b": TierEligible, "qwen3.5-4b": TierEligible, "qwen3.5-9b": TierSuggested, "qwen3.6-35b-a3b": TierBlocked,
		}},
		{"32gb", 32 * testGB, map[string]TierStatus{
			"gemma4-e2b": TierEligible, "qwen3.5-4b": TierEligible, "qwen3.5-9b": TierSuggested, "qwen3.6-35b-a3b": TierStretch,
		}},
		{"128gb", 128 * testGB, map[string]TierStatus{
			"gemma4-e2b": TierEligible, "qwen3.5-4b": TierEligible, "qwen3.5-9b": TierSuggested, "qwen3.6-35b-a3b": TierStretch,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tiered := TieredCatalogForRAM(tc.ram)
			got := map[string]TierStatus{}
			for _, tm := range tiered {
				got[tm.Model.Name] = tm.Status
			}
			for name, want := range tc.expect {
				if got[name] != want {
					t.Errorf("%s at %s: got %v, want %v", name, tc.name, got[name], want)
				}
			}
		})
	}
}

// TestSelectableForRAM exercises the direct per-model gate check used by
// /model selection.
func TestSelectableForRAM(t *testing.T) {
	// On an 8GB machine: 4b is a warned stretch, 9b is blocked outright.
	status, known := SelectableForRAM("qwen3.5-4b", 8*testGB)
	if !known || status != TierStretch {
		t.Fatalf("qwen3.5-4b at 8GB: got known=%v status=%v, want known=true status=stretch", known, status)
	}
	status, known = SelectableForRAM("qwen3.5-9b", 8*testGB)
	if !known || status != TierBlocked {
		t.Fatalf("qwen3.5-9b at 8GB: got known=%v status=%v, want known=true status=blocked", known, status)
	}
	if _, known := SelectableForRAM("not-a-real-model", 128*testGB); known {
		t.Fatal("expected unknown model name to report known=false")
	}
}
