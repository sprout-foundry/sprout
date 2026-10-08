//go:build !js

package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

// serviceRestarter is an optional capability on a serviceManager: a native
// single-step restart (launchd kickstart, systemd restart). Managers without
// it fall back to Stop()+Start().
type serviceRestarter interface {
	Restart() error
}

// restartDrainTimeout is how long `service restart` waits for active agent
// queries to finish before proceeding. Overridable via
// SPROUT_SERVICE_DRAIN_TIMEOUT (seconds). A package var so tests can
// shorten it.
var restartDrainTimeout = 15 * time.Second

func init() {
	if v := os.Getenv("SPROUT_SERVICE_DRAIN_TIMEOUT"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			restartDrainTimeout = time.Duration(secs) * time.Second
		}
	}
}

// serviceBaseURL is the daemon base URL the helpers hit. A var (not the
// const serviceURL) so tests can point it at an httptest server.
var serviceBaseURL = serviceURL

// healthPollInterval is the poll cadence for drainActiveQueries and
// waitHealthyAfter. A var so tests can shorten it.
var healthPollInterval = 500 * time.Millisecond

// setServiceURLForTest points the package's HTTP helpers at a test server
// for the duration of the test.
func setServiceURLForTest(t interface{ Cleanup(func()) }, url string) {
	prev := serviceBaseURL
	serviceBaseURL = url
	t.Cleanup(func() { serviceBaseURL = prev })
}

// fetchHealthStatus queries the daemon's /health endpoint. Returns nil when
// the daemon is unreachable (not an error for the callers here — restart
// and status both proceed sensibly without it).
func fetchHealthStatus() *healthSnapshot {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(serviceBaseURL + "/health")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var hs healthSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&hs); err != nil {
		return nil
	}
	return &hs
}

// healthSnapshot is the subset of the daemon /health payload this package
// reads. Unknown fields are ignored (defensive decoding, mirrors
// pkg/daemon.HealthStatus).
type healthSnapshot struct {
	Status        string `json:"status"`
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	ActiveQueries int    `json:"active_queries"`
}

// busyCount reports how much work an in-flight daemon is doing: active
// queries plus (best-effort) background agent sessions. A nil snapshot
// (daemon unreachable) counts as zero.
func busyCount(hs *healthSnapshot) int {
	if hs == nil {
		return 0
	}
	busy := hs.ActiveQueries
	if count, err := checkActiveSessions(); err == nil && count > 0 {
		busy += count
	}
	return busy
}

// drainActiveQueries polls /health until the daemon reports no active
// queries and no background sessions, or the timeout elapses. Returns nil
// when drained; an error naming the remaining work otherwise.
func drainActiveQueries(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		hs := fetchHealthStatus()
		if hs == nil {
			// Daemon went away mid-drain — nothing left to drain.
			return nil
		}
		sessions, _ := checkActiveSessions()
		if hs.ActiveQueries == 0 && sessions == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("still busy: %d quer(y/ies), %d background session(s)", hs.ActiveQueries, sessions)
		}
		time.Sleep(healthPollInterval)
	}
}

// waitHealthy polls until the daemon answers /health with 200 OK (and, when
// it reports a version, until that version differs from prevVersion — so a
// restart that re-executes the OLD binary too fast is not mistaken for the
// new one). Returns the final snapshot, or nil on timeout.
func waitHealthy(timeout time.Duration) *healthSnapshot {
	return waitHealthyAfter("", timeout)
}

// waitHealthyAfter is waitHealthy with an optional previous-version guard:
// pass the old daemon's version to wait until a DIFFERENT version is
// serving. Pass "" to accept the first healthy response.
func waitHealthyAfter(prevVersion string, timeout time.Duration) *healthSnapshot {
	deadline := time.Now().Add(timeout)
	for {
		if hs := fetchHealthStatus(); hs != nil {
			if prevVersion == "" || hs.Version == "" || hs.Version != prevVersion {
				return hs
			}
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(healthPollInterval)
	}
}
