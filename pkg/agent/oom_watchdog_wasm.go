//go:build js

package agent

// WASM stubs — OOM watchdog is not available in browser environment.
type OOMProbeResult struct{}

type OOMWatchdog struct{}

func NewOOMWatchdog() *OOMWatchdog { return &OOMWatchdog{} }
func (w *OOMWatchdog) Start()      {}
func (w *OOMWatchdog) Stop()       {}
