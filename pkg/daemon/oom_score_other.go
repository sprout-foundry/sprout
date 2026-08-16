//go:build !linux && !js

package daemon

func init() {
	// No oom_score_adj equivalent on this platform; the preference is a no-op.
	readOOMScoreAdj = func() (string, error) { return "0", nil }
	writeOOMScoreAdj = func(string) error { return nil }
}
