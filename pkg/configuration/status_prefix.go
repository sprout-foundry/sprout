package configuration

// status_prefix.go — package-local helpers for emitting [OK] / [WARN]
// bracketed status lines.
//
// Why not console.Glyph*? pkg/console imports pkg/configuration
// (console/keymap_registration.go needs the Manager API for the footer
// tooltip + verbosity toggle), so importing pkg/console from here is a
// direct two-package import cycle. The bracketed literals here are kept
// verbatim to preserve the existing user-facing surface;
// onboarding_glyph_test.go pins that output until the helpers are ported
// to the console.Glyph* surface.
// New code that doesn't hit this cycle should prefer console.Glyph*.
//
// If the console -> configuration edge in keymap_registration.go is ever
// broken (e.g. by giving the footer a narrower toggle interface), these
// helpers can be deleted and replaced with console.Glyph* at the call
// sites.

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
