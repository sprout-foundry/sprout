package mcp

// status_prefix.go — package-local helper for emitting [OK] / [WARN]
// bracketed status lines.
//
// Why not console.Glyph*? pkg/configuration imports this package
// (via config.go), and pkg/console imports pkg/configuration (via
// ci_output_handler.go), so importing console here would form a
// cycle:
//
//   pkg/mcp -> pkg/console -> pkg/configuration -> pkg/mcp
//
// The bracketed literal is preserved verbatim to keep the existing
// user-facing surface intact. If the cycle is ever broken, these
// helpers can be replaced with console.GlyphSuccess / GlyphWarning
// call sites.

import (
	"fmt"
	"io"
	"os"
)

// bracketOK writes "[OK] <msg>\n" to the configured writer.
func bracketOK(w io.Writer, msg string) {
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintf(w, "[OK] %s\n", msg)
}

// bracketWarn writes "[WARN] <msg>\n" to the configured writer.
func bracketWarn(w io.Writer, msg string) {
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintf(w, "[WARN] %s\n", msg)
}