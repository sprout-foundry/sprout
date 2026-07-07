package api

// status_prefix.go — package-local helper for emitting [WARN] bracketed
// status lines.
//
// Why not console.Glyph*? pkg/console imports pkg/configuration
// (via ci_output_handler.go for IsCI detection), and pkg/configuration
// imports this package (via api_keys.go). The chain
//   pkg/agent_api -> pkg/console -> pkg/configuration -> pkg/agent_api
// forms an import cycle, so this package cannot import pkg/console
// directly. The bracketed literal is preserved verbatim to keep the
// existing user-facing surface intact.
//
// If the cycle is ever broken (e.g. by extracting CI detection into a
// standalone helper that doesn't depend on pkg/configuration), these
// can be deleted in favor of console.GlyphWarning.Fprintln(os.Stderr, …).

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
