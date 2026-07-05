//go:build !js

package console

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Box-drawing characters for CLI panels. These are the light-vertical +
// heavy-horizontal variant used by gh/lazygit/lazydocker — visually
// distinct enough to read at a glance without being noisy.
//
// We use the same codepoints as those tools so muscle memory transfers:
//	┌ ─ ┐  top corners + horizontal rule
//	│      vertical side
//	└ ─ ┘  bottom corners
//	├ ┤ ┬ ┴ ┼  tee joints (for multi-column panels)
const (
	boxTopLeft     = "┌"
	boxTopRight    = "┐"
	boxBottomLeft  = "└"
	boxBottomRight = "┘"
	boxHorizontal  = "─"
	boxVertical    = "│"
	boxTeeDown     = "┬"
	boxTeeUp       = "┴"
	boxTeeRight    = "├"
	boxTeeLeft     = "┤"
)

// PanelStyle controls the visual treatment of a Panel. Zero value is a
// valid default: brand-colored top/bottom borders, no title.
type PanelStyle struct {
	BorderColor string // ANSI escape for the border (e.g. "\033[36m" for cyan)
	TitleColor  string // ANSI escape for the title text
	Padding     int    // spaces between border and content (default 1)
	MinWidth    int    // minimum panel width including borders (default 40)
	MaxWidth    int    // maximum panel width including borders (0 = unbounded)
}

// DefaultPanelStyle returns a brand-colored panel style suitable for
// most CLI output — cyan borders with a white title.
func DefaultPanelStyle() PanelStyle {
	return PanelStyle{
		BorderColor: "\033[36m",   // cyan
		TitleColor:  "\033[1;97m", // bold bright-white
		Padding:     1,
		MinWidth:    40,
		MaxWidth:    120,
	}
}

// Panel is a bordered text block with an optional title. Content lines
// are passed individually — use Panel.Render or Panel.Lines to output.
type Panel struct {
	Title   string   // optional header text (rendered in the top border)
	Content []string // body lines (each rendered on its own row)
	Style   PanelStyle
}

// Render returns the panel as a single string with embedded newlines.
// Each content line is wrapped to the panel's max width when set.
func (p Panel) Render() string {
	return strings.Join(p.Lines(), "\n")
}

// Lines returns the panel rendered as individual terminal rows. The
// output includes the top border, title (if any), content rows, and
// bottom border. Each row is a complete terminal line.
func (p Panel) Lines() []string {
	style := p.Style
	if style.Padding == 0 {
		style.Padding = 1
	}
	if style.MinWidth == 0 {
		style.MinWidth = 40
	}
	border := style.BorderColor
	reset := "\033[0m"
	dim := "\033[2m"

	// Determine content width: longest content line or title, plus padding.
	// Then clamp to min/max bounds.
	contentWidth := style.MinWidth - 2*(style.Padding+1) // subtract borders + padding
	for _, line := range p.Content {
		if w := visibleLen(line); w > contentWidth {
			contentWidth = w
		}
	}
	if p.Title != "" {
		if w := visibleLen(p.Title) + 2; w > contentWidth {
			contentWidth = w
		}
	}
	// Total panel width = content + 2*padding + 2 borders.
	panelWidth := contentWidth + 2*(style.Padding+1)
	if style.MaxWidth > 0 && panelWidth > style.MaxWidth {
		panelWidth = style.MaxWidth
		contentWidth = panelWidth - 2*(style.Padding+1)
	}

	// Top border with optional title: ┌─ Title ─...─┐
	topBorder := renderTopBorder(p.Title, panelWidth, border, style.TitleColor, dim, reset)

	// Content rows: pad to panel width with spaces. Each content line
	// may wrap to multiple visual rows (renderContentRow returns
	// multiple lines joined by \n when wrapping kicks in).
	rows := make([]string, 0, len(p.Content)+2)
	rows = append(rows, topBorder)
	for _, line := range p.Content {
		wrapped := renderContentRow(line, panelWidth, contentWidth, style.Padding, border, reset)
		for _, rl := range strings.Split(wrapped, "\n") {
			rows = append(rows, rl)
		}
	}
	rows = append(rows, renderBottomBorder(panelWidth, border, reset))

	return rows
}

// renderTopBorder builds "┌── Title ─...─┐" with the title centered
// between the left and right horizontal rules. When title is empty,
// the border is a plain "┌──...─┐".
func renderTopBorder(title string, panelWidth int, border, titleColor, dim, reset string) string {
	if title == "" {
		return border + strings.Repeat(boxHorizontal, panelWidth-2) + reset + border + reset
	}
	// "┌─ Title ─...─┐" — 1 char left, then title, then fill to width-2, then ┐.
	// We need: 1 (┌) + 1 (─) + space + title + space + ─... + 1 (┐) = panelWidth
	// After the left "┌─ ": 1+1+1=3 chars used, 1 char (┐) reserved at end.
	// Available for title + fill: panelWidth - 4 chars.
	const leftPrefix = boxTopLeft + boxHorizontal + " "
	const rightSuffix = boxHorizontal + boxTopRight
	available := panelWidth - visibleLen(leftPrefix) - visibleLen(rightSuffix)
	titleRendered := " " + title + " "
	if visibleLen(titleRendered) >= available {
		// Title too long — truncate.
		maxTitle := available - 2
		if maxTitle < 1 {
			titleRendered = ""
		} else {
			runes := []rune(titleRendered)
			if len(runes) > maxTitle {
				titleRendered = string(runes[:maxTitle-1]) + "…"
			}
		}
		available = panelWidth - visibleLen(leftPrefix) - visibleLen(rightSuffix)
	}
	fill := strings.Repeat(boxHorizontal, available-visibleLen(titleRendered))
	return border + leftPrefix + titleColor + titleRendered + reset + border + fill + rightSuffix + reset
}

// renderContentRow wraps a single line to contentWidth and pads it
// inside the panel borders: "│  content...   │".
func renderContentRow(line string, panelWidth, contentWidth, padding int, border, reset string) string {
	// Wrap content if it exceeds contentWidth.
	wrapped := wrapText(line, contentWidth)
	lines := strings.Split(wrapped, "\n")
	for i, l := range lines {
		lines[i] = border + boxVertical + reset + strings.Repeat(" ", padding) + padToVisible(l, contentWidth) + strings.Repeat(" ", padding) + border + boxVertical + reset
	}
	return strings.Join(lines, "\n")
}

// renderBottomBorder builds "└──...─┘".
func renderBottomBorder(panelWidth int, border, reset string) string {
	return border + boxBottomLeft + strings.Repeat(boxHorizontal, panelWidth-2) + boxBottomRight + reset
}

// wrapText word-wraps s to width w, breaking at whitespace when possible.
// Falls back to hard-wrapping at w when no whitespace is found.
func wrapText(s string, w int) string {
	if w <= 0 || visibleLen(s) <= w {
		return s
	}
	var b strings.Builder
	for visibleLen(s) > w {
		// Find last space within w chars.
		breakAt := -1
		visCount := 0
		for i, r := range s {
			visCount++
			if visCount > w {
				break
			}
			if r == ' ' {
				breakAt = i
			}
		}
		if breakAt <= 0 {
			breakAt = w
		}
		b.WriteString(s[:breakAt])
		b.WriteByte('\n')
		s = strings.TrimLeft(s[breakAt:], " ")
	}
	b.WriteString(s)
	return b.String()
}

// padToVisible right-pads s with spaces so its visible width equals w.
func padToVisible(s string, w int) string {
	vl := visibleLen(s)
	if vl >= w {
		return s
	}
	return s + strings.Repeat(" ", w-vl)
}

// visibleLen returns the number of terminal cells s occupies, ignoring
// ANSI escape sequences. Uses visibleLen from status_footer.go if
// available, else falls back to utf8 rune count.
func panelVisibleLen(s string) int {
	return utf8.RuneCountInString(stripAnsi(s))
}

// stripAnsi removes ANSI escape sequences for width measurement.
// Simple state machine — handles CSI sequences (\033[...m) and skips them.
func stripAnsi(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			// Skip CSI sequence until a letter (m, K, A, etc.)
			i += 2
			for i < len(s) {
				c := s[i]
				i++
				if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
					break
				}
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// BoxHint returns a brief one-line rendering of a box-drawing example,
// useful for inline UI hints (footer, tooltips) without rendering a
// full multi-row panel.
func BoxHint(label string) string {
	return fmt.Sprintf("%s%s%s %s %s%s%s", "\033[36m", boxTopLeft+boxHorizontal, boxHorizontal, label, boxHorizontal, boxBottomRight, "\033[0m")
}