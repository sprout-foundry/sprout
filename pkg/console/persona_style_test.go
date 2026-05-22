package console

import (
	"strings"
	"testing"
)

func TestPersonaBadge_Depth0_NoBadge(t *testing.T) {
	// Depth 0 is the primary agent — no badge so existing UX is preserved.
	if got := PersonaBadge(0, "coder"); got != "" {
		t.Errorf("depth 0 should produce empty badge, got %q", got)
	}
}

func TestPersonaBadge_EmptyPersona_NoBadge(t *testing.T) {
	if got := PersonaBadge(1, ""); got != "" {
		t.Errorf("empty persona should produce empty badge, got %q", got)
	}
	if got := PersonaBadge(1, "   "); got != "" {
		t.Errorf("whitespace persona should produce empty badge, got %q", got)
	}
}

func TestPersonaBadge_KnownPersona_IncludesPersonaText(t *testing.T) {
	got := PersonaBadge(1, "coder")
	if !strings.Contains(got, "[coder]") {
		t.Errorf("badge should contain [coder], got %q", got)
	}
}

func TestPersonaBadge_NoColorEnv_StripsANSI(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := PersonaBadge(1, "coder")
	if strings.Contains(got, "\033[") {
		t.Errorf("NO_COLOR should strip ANSI escapes, got %q", got)
	}
	if !strings.Contains(got, "[coder]") {
		t.Errorf("plain-text badge should still contain [coder], got %q", got)
	}
}

func TestPersonaBadge_UnknownPersona_StillRendersFallback(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := PersonaBadge(1, "made_up_persona")
	if !strings.Contains(got, "[made_up_persona]") {
		t.Errorf("unknown persona should still render its name, got %q", got)
	}
}

func TestPersonaIndent(t *testing.T) {
	cases := []struct {
		depth int
		want  string
	}{
		{0, ""},
		{1, "  "},
		{2, "    "},
		{3, "      "},
		{-1, ""}, // defensive
	}
	for _, c := range cases {
		if got := PersonaIndent(c.depth); got != c.want {
			t.Errorf("PersonaIndent(%d) = %q, want %q", c.depth, got, c.want)
		}
	}
}

func TestPersonaColor_KnownPersonas_ReturnDistinctColors(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	seen := map[string]string{}
	for _, p := range []string{"coder", "tester", "debugger", "researcher", "code_reviewer", "orchestrator"} {
		c := PersonaColor(p)
		if c == "" {
			t.Errorf("PersonaColor(%q) returned empty with FORCE_COLOR set", p)
			continue
		}
		if prev, exists := seen[c]; exists {
			t.Errorf("color collision: %q and %q share %q", prev, p, c)
		}
		seen[c] = p
	}
}
