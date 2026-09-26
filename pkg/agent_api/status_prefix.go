package api

// status_prefix.go — package-local helper for emitting [WARN] bracketed
// status lines.
//
// Why not console.Glyph*? This package cannot import pkg/console without
// closing an import cycle:
//   pkg/console -> pkg/configuration (keymap_registration.go needs the
//   configuration.Manager API) -> pkg/agent_api (api_keys.go). Adding
// agent_api -> pkg/console would complete the loop, so the bracketed
// literal is emitted locally instead, verbatim, to keep the existing
// user-facing surface intact.
//
// If the console -> configuration edge in keymap_registration.go is ever
// broken (e.g. by giving the footer a narrower verbosity-toggle
// interface), this helper can be deleted in favor of
// console.GlyphWarning.Fprintln(os.Stderr, msg).

import (
	"fmt"
	"io"
	"os"
)

// bracketWarn writes "[WARN] <msg>\n" to the configured writer.
func bracketWarn(w io.Writer, msg string) {
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprintf(w, "[WARN] %s\n", msg)
}
