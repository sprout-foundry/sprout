package configuration

// status_prefix.go — package-local helpers for emitting [OK] / [WARN]
// bracketed status lines.
//
// Why not console.Glyph*? pkg/console imports pkg/configuration
// (via console/ci_output_handler.go for IsCI detection), so this
// package cannot import pkg/console. The bracketed literals here
// are kept verbatim to preserve the existing user-facing surface.
// New code that doesn't hit this cycle should prefer console.Glyph*.
//
// If the cycle is ever broken (e.g., by inverting the CI check to a
// pure function), these helpers can be deleted and replaced with
// console.GlyphSuccess.Fprintln(os.Stdout, msg) at the call sites.

import (
	"fmt"
	"io"
	"os"
)

// bracketOK writes "[OK] <msg>\n" to the configured writer. Default
// writer is os.Stdout. Kept narrow on purpose — there are no
// format-string variants in this package's onboarding code.
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
