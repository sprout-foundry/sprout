//go:build !js

package cmd

import (
	"log"
	"os"
)

// redirectGoLogToWorkspace points Go's default `log` package output at
// .sprout/workspace.log (appended) so package-level log.Printf calls in
// sprout's internals don't interleave with the interactive REPL's
// stderr-bound chrome (activity indicator, status footer, prompts).
//
// Returns a restore func the caller defers; pass the original output back
// to log.SetOutput so background non-interactive paths (subagents, daemon
// mode) keep their stderr logging intact when this REPL session ends.
//
// On any failure (workspace dir not writable, permission error, etc.) the
// function silently returns a no-op restore — better to keep the spinner
// thrash than to wedge the user's session on a logging concern.
func redirectGoLogToWorkspace() (restore func(), err error) {
	if err := os.MkdirAll(".sprout", 0o755); err != nil {
		return func() {}, err
	}
	f, err := os.OpenFile(".sprout/workspace.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return func() {}, err
	}
	prev := log.Writer()
	log.SetOutput(f)
	return func() {
		log.SetOutput(prev)
		_ = f.Close()
	}, nil
}
