// Package updatecheck implements the passive "new release available"
// check: it throttles GitHub release lookups to once per check interval,
// caches the result in the sprout state directory, and answers "is there
// a newer version than X" for the CLI notice and the WebUI banner.
//
// All persistence is best-effort: any read/write failure degrades to
// "no cached state" rather than an error, because the check must never
// disrupt startup or command output.
package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

const (
	// CheckInterval is the minimum time between GitHub release lookups.
	// The unauthenticated GitHub API allows 60 req/hr per IP; one lookup
	// per process start at this throttle stays far below that even with
	// many sprout invocations, since only the first start in the window
	// hits the network.
	CheckInterval = 24 * time.Hour

	// NoticeInterval is how often the CLI re-shows the update notice for
	// the same newer version. Each new cached version notifies immediately.
	NoticeInterval = 24 * time.Hour

	// fetchTimeout bounds a single release lookup so a slow or proxied
	// network can never stall a startup path that awaits the goroutine's
	// state write.
	fetchTimeout = 3 * time.Second

	stateFile = "update-check.json"

	releasesLatestURL = "https://api.github.com/repos/sprout-foundry/sprout/releases/latest"
)

// releasesURL is a package var so tests can point FetchLatestTag at an
// httptest server.
var releasesURL = releasesLatestURL

// State is the persisted update-check cache.
type State struct {
	// LastCheck is when the release lookup last ran (success or failure).
	// Failures advance it too, so an unreachable GitHub doesn't turn every
	// startup into a network attempt.
	LastCheck time.Time `json:"last_check,omitempty"`

	// LatestRelease is the newest stable release tag seen. Empty until the
	// first successful lookup.
	LatestRelease string `json:"latest_release,omitempty"`

	// NoticeShownAt / NoticeShownFor record when the update notice was
	// last printed and for which version, enforcing the once-per-notice-
	// interval nag limit.
	NoticeShownAt  time.Time `json:"notice_shown_at,omitempty"`
	NoticeShownFor string    `json:"notice_shown_for,omitempty"`
}

// Skipped resolves every skip condition for both the check and its
// notice: non-semver builds ("dev", empty) can't be compared, the env
// var opts out, CI environments shouldn't make network calls, and the
// config knob disables the feature. Callers fold nil-config into
// disabled=true (fail closed: no config, no GitHub call).
func Skipped(current string, disabled bool) bool {
	if current == "" || current == "dev" {
		return true
	}
	if os.Getenv("SPROUT_NO_UPDATE_CHECK") == "1" {
		return true
	}
	if os.Getenv("CI") != "" {
		return true
	}
	return disabled
}

// NormalizeVersion strips a leading 'v' (or 'V') so v1.2.3 / V1.2.3 /
// 1.2.3 compare equal, then re-adds the 'v' because upstream release
// tags carry it. "dev" passes through unchanged.
func NormalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "dev" {
		return v
	}
	return "v" + trimVersion(v)
}

func trimVersion(v string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V"))
}

// IsNewer reports whether latest is a strict semver upgrade over current.
// Either side failing to parse as semver ("dev", git-hash builds, empty
// strings) yields false — no notice rather than a wrong notice.
func IsNewer(current, latest string) bool {
	cur, err := semver.NewVersion(trimVersion(current))
	if err != nil {
		return false
	}
	lat, err := semver.NewVersion(trimVersion(latest))
	if err != nil {
		return false
	}
	return lat.GreaterThan(cur)
}

// NeedsCheck reports whether the cached state is stale enough to justify
// another release lookup. A zero LastCheck (first run) always needs one;
// a future LastCheck (clock moved back) counts as recently checked so a
// skewed clock can't trigger a lookup on every start.
func NeedsCheck(s State, now time.Time) bool {
	return now.Sub(s.LastCheck) >= CheckInterval
}

// stateMu serializes the read-modify-write cycles below. The CLI refresh
// goroutine, the daemon refresh goroutine, and the notice renderer can
// run in one process; without the lock, a refresh persisting its pre-fetch
// snapshot would wipe a notice marker the main goroutine just saved,
// re-triggering the notice on the next start.
var (
	stateMu  sync.Mutex
	fetching bool
)

// NoticeFor returns the cached newer version when a notice is due: the
// cached LatestRelease must be a strict semver upgrade over current and
// not already shown for that version within NoticeInterval. When ok, the
// shown-marker is persisted so the caller can print unconditionally.
func NoticeFor(current string, now time.Time) (latest string, ok bool) {
	stateMu.Lock()
	defer stateMu.Unlock()

	s := LoadState()
	if s.LatestRelease == "" || !IsNewer(current, s.LatestRelease) {
		return "", false
	}
	if s.NoticeShownFor == s.LatestRelease && now.Sub(s.NoticeShownAt) < NoticeInterval {
		return "", false
	}
	s.NoticeShownAt = now
	s.NoticeShownFor = s.LatestRelease
	saveState(s)
	return s.LatestRelease, true
}

// CachedNewer is NoticeFor without the side effect, for read-only
// consumers like the WebUI bootstrap payload.
func CachedNewer(current string, now time.Time) (latest string, ok bool) {
	stateMu.Lock()
	defer stateMu.Unlock()

	s := LoadState()
	if s.LatestRelease == "" || !IsNewer(current, s.LatestRelease) {
		return "", false
	}
	return s.LatestRelease, true
}

// RefreshMaybe runs a release lookup when the cached state is stale and
// persists the outcome. It is synchronous but bounded by fetchTimeout;
// callers invoke it from a goroutine so startup never waits on it.
//
// The in-flight flag dedupes concurrent launches in one process (the
// interactive CLI and the in-process web server both call this). The lock
// is released during the network call so notice rendering and bootstrap
// reads never block on the fetch, and the merge re-reads state afterward
// so a marker persisted meanwhile survives.
func RefreshMaybe(ctx context.Context, now time.Time) {
	stateMu.Lock()
	s := LoadState()
	if fetching || !NeedsCheck(s, now) {
		stateMu.Unlock()
		return
	}
	fetching = true
	stateMu.Unlock()

	latest, err := FetchLatestTag(ctx)

	stateMu.Lock()
	fetching = false
	fresh := LoadState()
	fresh.LastCheck = now
	if err == nil {
		fresh.LatestRelease = NormalizeVersion(latest)
	}
	saveState(fresh)
	stateMu.Unlock()
}

// FetchLatestTag returns the most recent non-draft, non-prerelease
// release tag from GitHub. Bounded by fetchTimeout.
func FetchLatestTag(ctx context.Context) (string, error) {
	return fetchLatestTagBounded(ctx, fetchTimeout)
}

// FetchLatestTagLongTimeout is FetchLatestTag with the upgrade command's
// generous 5-minute budget: the user explicitly asked for a version
// lookup, so a slow or proxied link gets the same patience the old
// inline implementation had.
func FetchLatestTagLongTimeout(ctx context.Context) (string, error) {
	return fetchLatestTagBounded(ctx, 5*time.Minute)
}

func fetchLatestTagBounded(ctx context.Context, timeout time.Duration) (string, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "sprout-upgrade")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode API response: %w", err)
	}
	if payload.TagName == "" {
		return "", errors.New("GitHub API returned an empty tag_name")
	}
	return payload.TagName, nil
}

// LoadState reads the cached update-check state. Any failure — missing
// file, corrupt JSON, unwritable state dir — returns the zero State.
func LoadState() State {
	path, err := statePath()
	if err != nil {
		return State{}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return State{}
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}
	}
	return s
}

func saveState(s State) {
	path, err := statePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	// Write-then-rename keeps concurrent readers (CLI notice, daemon
	// bootstrap) from ever seeing a truncated or half-written file.
	tmp, err := os.CreateTemp(filepath.Dir(path), stateFile+".tmp-*")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return
	}
	_ = os.Rename(tmpPath, path)
}

func statePath() (string, error) {
	dir, err := envutil.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, stateFile), nil
}
