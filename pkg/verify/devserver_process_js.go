//go:build js

package verify

import (
	"bytes"
	"errors"
)

// DevProcess is a no-op for WASM builds: the browser-side plane never launches
// a dev-server process, so a check or preview that needs a real server cannot
// run under WASM (it is reported as "the dev server did not start"). The type
// exists with the same shape as the platform files so the package compiles
// for js/wasm.
type DevProcess struct{}

// StartDevProcess reports that dev-server process support is unavailable in
// WASM builds (the browser cannot launch a host process).
func StartDevProcess(_ string, _ string, _ *bytes.Buffer) (*DevProcess, error) {
	return nil, errors.New("dev server process support is unavailable in WASM builds")
}

func (p *DevProcess) Stop() {}

func (p *DevProcess) Wait() error { return nil }
