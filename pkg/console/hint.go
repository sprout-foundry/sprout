package console

import (
	"fmt"
	"io"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// Hintln writes a secondary guidance line under a glyph-led line: indented
// to align with the message text, dimmed, no glyph of its own.
func Hintln(w io.Writer, msg string) {
	_, _ = fmt.Fprintln(w, "  "+dimIfColor(msg))
}

func dimIfColor(s string) string {
	if !envutil.ResolveColorPreference(true) {
		return s
	}
	return "\033[2m" + s + ansiReset
}

// Heading writes a section title. One style everywhere — no "===" banners,
// underlines, or markdown "##" prefixes in terminal output.
func Heading(w io.Writer, title string) {
	if !envutil.ResolveColorPreference(true) {
		_, _ = fmt.Fprintln(w, title)
		return
	}
	_, _ = fmt.Fprintln(w, ColorBold+title+ansiReset)
}
