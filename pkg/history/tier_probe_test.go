package history

import (
	"os"
	"path/filepath"
	"testing"
)

// loadRevisionTierOnly must classify identically to
// loadRevisionTextForTier (the manifest path uses the cheap probe; the
// full-text path remains the reference behavior).
func TestLoadRevisionTierOnly_MatchesTextVariant(t *testing.T) {
	tmp := t.TempDir()

	makeRev := func(name string, instructions, response, conversation bool) string {
		dir := filepath.Join(tmp, name)
		os.MkdirAll(dir, 0o755)
		if instructions {
			os.WriteFile(filepath.Join(dir, "instructions.txt"), []byte("i"), 0o644)
		}
		if response {
			os.WriteFile(filepath.Join(dir, "llm_response.txt"), []byte("r"), 0o644)
		}
		if conversation {
			os.WriteFile(filepath.Join(dir, "conversation.json"), []byte("{}"), 0o644)
		}
		return dir
	}

	cases := []struct {
		name                                 string
		instructions, response, conversation bool
	}{
		{"hot", true, true, true},
		{"warm-no-conversation", true, true, false},
		{"instructions-only", true, false, false},
		{"empty", false, false, false},
	}

	for _, tc := range cases {
		dir := makeRev(tc.name, tc.instructions, tc.response, tc.conversation)
		want, _, _ := loadRevisionTextForTier(dir)
		got := loadRevisionTierOnly(dir)
		if got != want {
			t.Errorf("%s: tier-only %q != text-variant %q", tc.name, got, want)
		}
	}

	// Missing revision dir classifies as "".
	if got := loadRevisionTierOnly(filepath.Join(tmp, "does-not-exist")); got != "" {
		t.Errorf("missing dir: got %q, want empty", got)
	}
}
