//go:build js

package verify

import (
	"bytes"
	"errors"
)

// devProcess is a no-op for WASM builds: the browser-side plane never launches
// a dev-server process, so a page check that needs a real server cannot run
// under WASM (it is reported as "the dev server did not start"). The type
// exists with the same shape as the platform files so the package compiles
// for js/wasm.
type devProcess struct{}

// spawnDevProcess reports that dev-server process support is unavailable in
// WASM builds (the browser cannot launch a host process).
func spawnDevProcess(_ string, _ string, _ *bytes.Buffer) (*devProcess, error) {
	return nil, errors.New("dev server process support is unavailable in WASM builds")
}

func (p *devProcess) stop() {}

func (p *devProcess) wait() error { return nil }
