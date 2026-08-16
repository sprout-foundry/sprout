//go:build linux && !js

package daemon

import "os"

const oomScoreAdjPath = "/proc/self/oom_score_adj"

func init() {
	readOOMScoreAdj = func() (string, error) {
		data, err := os.ReadFile(oomScoreAdjPath)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	writeOOMScoreAdj = func(value string) error {
		return os.WriteFile(oomScoreAdjPath, []byte(value), 0o644)
	}
}
