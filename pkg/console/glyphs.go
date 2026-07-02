package console

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// colorBlindPalette is the process-wide toggle for the CLI-E color-blind
// palette swap. When true, Glyph.color() returns the swapped palette
// (red→cyan, amber→magenta) so success / error / warning remain
// distinguishable under deuteranopia / protanopia.
//
// CLI-E-1 wires the env-var / flag plumbing; CLI-E-2 lives here.
//
// Atomic so the palette can flip at runtime (e.g. on flag re-read after
// PersistentPreRunE) without racing a concurrent render in the
// REPL goroutine. Read in the hot path of color() — atomic.LoadUint32
// is cheaper than sync.RWMutex.
var colorBlindPalette atomic.Bool

// SetColorBlind enables or disables the color-blind palette swap.
// Returns the previous value so callers (tests) can restore it.
func SetColorBlind(enabled bool) bool {
	return colorBlindPalette.Swap(enabled)
}

// ColorBlindEnabled reports the current palette state. Mostly useful
// in tests and /help output.
func ColorBlindEnabled() bool {
	return colorBlindPalette.Load()
}

// ApplyColorBlindFromEnv reads SPROUT_COLOR_BLIND and enables the
// palette swap if set to a truthy value ("1", "true", "yes"). Called
// from cmd/root.go after flag parsing so the CLI flag wins over the
// env var (the flag sets the atomic directly via SetColorBlind).
func ApplyColorBlindFromEnv() {
	v := os.Getenv("SPROUT_COLOR_BLIND")
	switch v {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		SetColorBlind(true)
	}
}

// Glyph encodes a single semantic category for CLI status lines.
// Every output line that announces a status (success, error, warning,
// progress, …) should pick exactly one of these. The rendered prefix is
// `<glyph> ` in the canonical color; consistency makes the scroll
// region scannable at a glance — green ticks = good, red marks =
// problems, amber = needs attention.
//
// Honors NO_COLOR / FORCE_COLOR via envutil.ResolveColorPreference. In
// no-color mode the glyph still renders (it's UTF-8, not ANSI) so the
// semantic is preserved; only the color escape is suppressed.
type Glyph int

const (
	// GlyphSuccess marks a completed action / success state.
	// Replaces: [OK], [clean], [done]
	GlyphSuccess Glyph = iota
	// GlyphError marks a failure / error state.
	// Replaces: [FAIL]
	GlyphError
	// GlyphWarning marks something that needs attention but is non-fatal.
	// Replaces: [WARN], [skip] (some)
	GlyphWarning
	// GlyphInfo marks a system / informational message.
	// Replaces: [bot], [web], [skills], [chart] (welcome banner uses)
	GlyphInfo
	// GlyphAction marks an action in flight / submitted.
	// Replaces: [tool], [chart] (progress uses), [RELOAD]
	GlyphAction
	// GlyphPaused marks paused / queued state — waiting for something.
	// Replaces: [||] (interrupting), [queued]
	GlyphPaused
	// GlyphStopped marks a stopped / interrupted / aborted state.
	// Replaces: [STOP], [!]
	GlyphStopped
	// GlyphShell marks a shell command being executed. Replaces the
	// bare "$ " prompt prefix in agent_workflow_runner and similar
	// sites. Distinct from GlyphAction (which uses →) so power users
	// can grep / scroll for shell output specifically.
	//
	// CLI-F-3: explicit constant for the shell-prompt glyph.
	GlyphShell
	// GlyphDim marks secondary / continuation / metric lines that
	// shouldn't draw the eye. Replaces: [skip] (some), [debug]
	GlyphDim
)

// glyphRune is the visible character for the glyph. UTF-8; widely
// supported in terminal fonts.
func (g Glyph) Rune() string {
	switch g {
	case GlyphSuccess:
		return "✓"
	case GlyphError:
		return "✗"
	case GlyphWarning:
		return "⚠"
	case GlyphInfo:
		return "ⓘ"
	case GlyphAction:
		return "→"
	case GlyphPaused:
		return "⏸"
	case GlyphStopped:
		return "⏹"
	case GlyphShell:
		return "$"
	case GlyphDim:
		return "·"
	default:
		return "·"
	}
}

// color returns the ANSI prefix for this glyph's canonical color, or
// empty string when color is disabled.
//
// CLI-E: when color-blind mode is active (via SPROUT_COLOR_BLIND env
// var or the --color-blind flag), the canonical palette is swapped to
// one that's distinguishable under deuteranopia / protanopia:
//   - GlyphError    (red)    → bright cyan  (\033[1;36m)
//   - GlyphWarning  (amber)  → magenta      (\033[1;35m)
//   - GlyphStopped  (red)    → bright cyan
//   - GlyphPaused   (amber)  → magenta
//   - other glyphs retain their canonical color so the green/cyan axes
//     that distinguish success/info from errors are unchanged.
//
// The swap is process-wide; tests stub colorBlindPalette via
// SetColorBlindForTest.
func (g Glyph) color() string {
	if !envutil.ResolveColorPreference(true) {
		return ""
	}
	if colorBlindPalette.Load() {
		switch g {
		case GlyphError:
			return "\033[1;36m" // bold bright cyan
		case GlyphStopped:
			return "\033[1;36m"
		case GlyphWarning:
			return "\033[1;35m" // bold magenta
		case GlyphPaused:
			return "\033[1;35m"
		case GlyphSuccess:
			return "\033[32m" // green stays (vs cyan — distinguishable)
		case GlyphInfo:
			return "\033[36m"
		case GlyphAction:
			return "\033[1;96m"
		case GlyphShell:
			return "\033[1;32m" // bold green — "go" prompt feel
		case GlyphDim:
			return "\033[2m"
		default:
			return ""
		}
	}
	switch g {
	case GlyphSuccess:
		return "\033[32m" // green
	case GlyphError:
		return "\033[31m" // red
	case GlyphWarning:
		return "\033[33m" // amber/yellow
	case GlyphInfo:
		return "\033[36m" // cyan
	case GlyphAction:
		return "\033[1;96m" // bold bright-cyan (brand)
	case GlyphPaused:
		return "\033[33m" // amber
	case GlyphStopped:
		return "\033[31m" // red
	case GlyphShell:
		return "\033[1;32m" // bold green
	case GlyphDim:
		return "\033[2m" // dim
	default:
		return ""
	}
}

const ansiReset = "\033[0m"

// Prefix returns the colored glyph plus a single trailing space, ready
// to lead a line:
//
//	fmt.Fprintf(os.Stderr, "%sresumed: %s\n", console.GlyphSuccess.Prefix(), label)
//	→ ✓ resumed: foo
//
// In no-color mode the glyph still appears (the color escape is just
// empty). The reset escape only emits when a color was emitted.
func (g Glyph) Prefix() string {
	c := g.color()
	if c == "" {
		return g.Rune() + " "
	}
	return c + g.Rune() + ansiReset + " "
}

// Print writes "<glyph> <msg>\n" to stderr. Convenience for the most
// common call shape. Use Printf for format-string callers, or
// Fprintln/Fprintf if you need to target a specific writer (tests).
func (g Glyph) Print(msg string) {
	fmt.Fprintln(os.Stderr, g.Prefix()+msg)
}

// Printf writes a formatted line with the glyph prefix to stderr.
func (g Glyph) Printf(format string, args ...any) {
	fmt.Fprint(os.Stderr, g.Prefix()+fmt.Sprintf(format, args...)+"\n")
}

// Fprintln writes the glyph-prefixed message to an explicit writer.
// Tests use this to capture output to a buffer.
func (g Glyph) Fprintln(w io.Writer, msg string) {
	fmt.Fprintln(w, g.Prefix()+msg)
}

// Fprintf writes a formatted glyph-prefixed line to an explicit writer.
func (g Glyph) Fprintf(w io.Writer, format string, args ...any) {
	fmt.Fprint(w, g.Prefix()+fmt.Sprintf(format, args...)+"\n")
}
