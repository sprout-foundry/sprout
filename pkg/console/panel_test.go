package console

import (
	"strings"
	"testing"
)

// CLI-UX-9: box-drawing panels
func TestPanel_BasicRender(t *testing.T) {
	p := Panel{
		Title:   "Test",
		Content: []string{"line one", "line two"},
		Style:   DefaultPanelStyle(),
	}
	lines := p.Lines()
	if len(lines) != 4 { // top + 2 content + bottom
		t.Fatalf("expected 4 lines, got %d: %v", len(lines), lines)
	}
	if !strings.HasPrefix(stripAnsi(lines[0]), boxTopLeft) {
		t.Errorf("top row should start with %s, got %q", boxTopLeft, lines[0])
	}
	if !strings.HasSuffix(stripAnsi(lines[len(lines)-1]), boxBottomRight) {
		t.Errorf("bottom row should end with %s, got %q", boxBottomRight, lines[len(lines)-1])
	}
	for i, l := range lines {
		// Top and bottom rows use horizontal-only borders; content
		// rows should contain the vertical side bars.
		if i == 0 || i == len(lines)-1 {
			continue
		}
		if !strings.Contains(stripAnsi(l), boxVertical) {
			t.Errorf("row %d should contain vertical border: %q", i, l)
		}
	}
}

func TestPanel_TitleInTopBorder(t *testing.T) {
	p := Panel{
		Title:   "My Todos",
		Content: []string{"item"},
		Style:   DefaultPanelStyle(),
	}
	lines := p.Lines()
	if !strings.Contains(stripAnsi(lines[0]), "My Todos") {
		t.Errorf("title should appear in top border, got %q", lines[0])
	}
}

func TestPanel_NoTitle(t *testing.T) {
	p := Panel{
		Content: []string{"item"},
		Style:   DefaultPanelStyle(),
	}
	lines := p.Lines()
	// Top row should be just horizontal rule, no text.
	top := stripAnsi(lines[0])
	if strings.ContainsAny(top, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		t.Errorf("top border should have no text when title empty, got %q", top)
	}
}

func TestPanel_WordWrapsLongContent(t *testing.T) {
	longLine := "this is a very long line that should wrap to multiple rows inside the panel because it exceeds the content width"
	p := Panel{
		Title:   "Wrap",
		Content: []string{longLine},
		Style: PanelStyle{
			BorderColor: "\033[36m",
			Padding:     1,
			MinWidth:    20,
			MaxWidth:    40,
		},
	}
	lines := p.Lines()
	if len(lines) < 4 {
		t.Errorf("expected wrapping to produce multiple rows, got %d: %v", len(lines), lines)
	}
}

func TestPanel_MinWidth(t *testing.T) {
	p := Panel{
		Title:   "x",
		Content: []string{"hi"},
		Style: PanelStyle{
			BorderColor: "",
			Padding:     1,
			MinWidth:    30,
		},
	}
	lines := p.Lines()
	for _, l := range lines {
		// All visible-width lines should be at least 30 chars.
		if panelVisibleLen(l) < 30 {
			t.Errorf("panel width %d < minWidth 30: %q", panelVisibleLen(l), l)
		}
	}
}

func TestPanel_RenderIsNewlineJoined(t *testing.T) {
	p := Panel{
		Title:   "T",
		Content: []string{"a", "b"},
		Style:   DefaultPanelStyle(),
	}
	out := p.Render()
	if strings.Count(out, "\n") != 3 {
		t.Errorf("expected 3 newlines in Render output, got %d in %q", strings.Count(out, "\n"), out)
	}
}

func TestBoxHint(t *testing.T) {
	hint := BoxHint("enter")
	if !strings.Contains(stripAnsi(hint), "enter") {
		t.Errorf("BoxHint should contain label, got %q", hint)
	}
	if !strings.Contains(hint, boxTopLeft) {
		t.Errorf("BoxHint should contain box-drawing chars, got %q", hint)
	}
}
