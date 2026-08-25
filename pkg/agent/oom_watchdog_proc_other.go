//go:build !linux && !js

package agent

// probe is a no-op on non-Linux platforms.
// The OOMWatchdog still runs its ticker cycle but always reports
// zero node count and zero RSS, so no alerts are ever triggered.
func (w *OOMWatchdog) probe() (*OOMProbeResult, error) {
	return &OOMProbeResult{}, nil
}
