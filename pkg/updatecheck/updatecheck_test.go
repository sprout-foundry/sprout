package updatecheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v1.2.3", "v1.2.3"},
		{"1.2.3", "v1.2.3"},
		{"V1.2.3", "v1.2.3"},
		{"  v0.14.0  ", "v0.14.0"},
		{"dev", "dev"},
		{"", "v"},
	}
	for _, tc := range cases {
		if got := NormalizeVersion(tc.in); got != tc.want {
			t.Errorf("NormalizeVersion(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v1.0.0", "v1.1.0", true},
		{"v1.1.0", "v1.0.0", false},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.0", "v2.0.0-rc1", true},
		{"dev", "v9.9.9", false},
		{"v1.0.0", "dev", false},
		{"", "v1.0.0", false},
		{"v1.0.0", "", false},
		{"abc1234", "v1.0.0", false},
	}
	for _, tc := range cases {
		if got := IsNewer(tc.current, tc.latest); got != tc.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestNeedsCheck(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	if !NeedsCheck(State{}, now) {
		t.Error("zero state must need a check")
	}
	fresh := State{LastCheck: now.Add(-time.Hour)}
	if NeedsCheck(fresh, now) {
		t.Error("state checked 1h ago must not need a check (interval 24h)")
	}
	stale := State{LastCheck: now.Add(-25 * time.Hour)}
	if !NeedsCheck(stale, now) {
		t.Error("state checked 25h ago must need a check")
	}
	skewed := State{LastCheck: now.Add(time.Hour)}
	if NeedsCheck(skewed, now) {
		t.Error("future LastCheck (clock skew) must count as recently checked")
	}
}

func TestNoticeFor_ShowsOncePerVersion(t *testing.T) {
	withTempStateDir(t)

	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	writeTestState(t, State{LatestRelease: "v0.15.0"})

	latest, ok := NoticeFor("v0.14.0", now)
	if !ok || latest != "v0.15.0" {
		t.Fatalf("NoticeFor = %q, %v; want v0.15.0, true", latest, ok)
	}

	if _, ok := NoticeFor("v0.14.0", now.Add(time.Hour)); ok {
		t.Error("notice must not repeat within NoticeInterval for the same version")
	}

	if _, ok := NoticeFor("v0.15.0", now.Add(2*time.Hour)); ok {
		t.Error("notice must not fire when current >= latest")
	}

	// A newer cached version resets the shown-marker.
	writeTestState(t, State{LatestRelease: "v0.16.0"})
	if _, ok := NoticeFor("v0.14.0", now.Add(2*time.Hour)); !ok {
		t.Error("a new cached version must notify immediately")
	}
}

func TestCachedNewer_NoSideEffects(t *testing.T) {
	withTempStateDir(t)

	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	writeTestState(t, State{LatestRelease: "v0.15.0"})
	for i := 0; i < 3; i++ {
		if _, ok := CachedNewer("v0.14.0", now); !ok {
			t.Fatal("CachedNewer should keep reporting while cache holds a newer version")
		}
	}
	s := LoadState()
	if !s.NoticeShownAt.IsZero() || s.NoticeShownFor != "" {
		t.Errorf("CachedNewer mutated state: %+v", s)
	}
}

func TestRefreshMaybe_FetchesWhenStaleAndPersists(t *testing.T) {
	withTempStateDir(t)
	defer withTestReleasesURL(t, `{"tag_name":"v0.15.0"}`)()

	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

	RefreshMaybe(context.Background(), now)
	s := LoadState()
	if s.LatestRelease != "v0.15.0" {
		t.Fatalf("LatestRelease = %q, want v0.15.0", s.LatestRelease)
	}
	if !s.LastCheck.Equal(now) {
		t.Errorf("LastCheck = %v, want %v", s.LastCheck, now)
	}

	// Fresh cache: no second fetch even when the endpoint changes.
	defer withTestReleasesURL(t, `{"tag_name":"v0.99.0"}`)()
	RefreshMaybe(context.Background(), now.Add(time.Hour))
	if s := LoadState(); s.LatestRelease != "v0.15.0" {
		t.Errorf("fresh cache was re-fetched: LatestRelease = %q", s.LatestRelease)
	}

	// Stale again: fetch runs and updates the cache.
	RefreshMaybe(context.Background(), now.Add(25*time.Hour))
	if s := LoadState(); s.LatestRelease != "v0.99.0" {
		t.Errorf("stale cache not refreshed: LatestRelease = %q", s.LatestRelease)
	}
}

// writeTestState seeds the cache file directly through the package's own
// save path.
func writeTestState(t *testing.T, s State) {
	t.Helper()
	saveState(s)
}

func TestRefreshMaybe_FetchFailureStillAdvancesThrottle(t *testing.T) {
	withTempStateDir(t)
	defer withTestReleasesURLStatus(t, "internal error", http.StatusInternalServerError)()

	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	RefreshMaybe(context.Background(), now)

	s := LoadState()
	if !s.LastCheck.Equal(now) {
		t.Errorf("failed fetch must advance LastCheck, got %v", s.LastCheck)
	}
	if s.LatestRelease != "" {
		t.Errorf("failed fetch must not record a version, got %q", s.LatestRelease)
	}
}

func TestLoadState_CorruptJSONYieldsZero(t *testing.T) {
	withTempStateDir(t)

	path, err := statePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s := LoadState(); !s.LastCheck.IsZero() || s.LatestRelease != "" {
		t.Errorf("corrupt state must degrade to zero, got %+v", s)
	}
}

func TestStateRoundTrip(t *testing.T) {
	withTempStateDir(t)

	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	writeTestState(t, State{LastCheck: now, LatestRelease: "v0.15.0", NoticeShownAt: now, NoticeShownFor: "v0.15.0"})
	out := LoadState()
	if !out.LastCheck.Equal(now) || out.LatestRelease != "v0.15.0" ||
		!out.NoticeShownAt.Equal(now) || out.NoticeShownFor != "v0.15.0" {
		t.Errorf("round trip mismatch: %+v", out)
	}
}

func TestSkipped(t *testing.T) {
	t.Setenv("SPROUT_NO_UPDATE_CHECK", "")
	t.Setenv("CI", "")
	if Skipped("v1.0.0", false) {
		t.Error("release build with no opt-outs must not skip")
	}
	if !Skipped("dev", false) {
		t.Error("dev build must skip")
	}
	if !Skipped("", false) {
		t.Error("empty version must skip")
	}
	if !Skipped("v1.0.0", true) {
		t.Error("config-disabled must skip")
	}

	t.Setenv("SPROUT_NO_UPDATE_CHECK", "1")
	if !Skipped("v1.0.0", false) {
		t.Error("SPROUT_NO_UPDATE_CHECK=1 must skip")
	}
	if Skipped("v1.0.0", false) && false {
		t.Error("unreachable")
	}

	t.Setenv("SPROUT_NO_UPDATE_CHECK", "0")
	if Skipped("v1.0.0", false) {
		t.Error("SPROUT_NO_UPDATE_CHECK=0 must not skip (only =1 opts out)")
	}

	t.Setenv("SPROUT_NO_UPDATE_CHECK", "")
	t.Setenv("CI", "1")
	if !Skipped("v1.0.0", false) {
		t.Error("CI environment must skip")
	}
}

func TestRefreshMaybe_ConcurrentLaunchesFetchOnce(t *testing.T) {
	withTempStateDir(t)

	var fetches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fetches, 1)
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.15.0"}`))
	}))
	defer srv.Close()
	old := releasesURL
	releasesURL = srv.URL
	defer func() { releasesURL = old }()

	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			RefreshMaybe(context.Background(), now)
		}()
	}
	wg.Wait()

	if n := atomic.LoadInt32(&fetches); n != 1 {
		t.Errorf("concurrent refreshes hit the API %d times, want exactly 1", n)
	}
	if s := LoadState(); s.LatestRelease != "v0.15.0" {
		t.Errorf("LatestRelease = %q, want v0.15.0", s.LatestRelease)
	}
}

func TestNoticeFor_SurvivesConcurrentRefresh(t *testing.T) {
	withTempStateDir(t)
	defer withTestReleasesURL(t, `{"tag_name":"v0.15.0"}`)()

	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	writeTestState(t, State{LastCheck: now.Add(-25 * time.Hour), LatestRelease: "v0.15.0"})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		RefreshMaybe(context.Background(), now)
	}()
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond) // let the refresh start fetching
		if _, ok := NoticeFor("v0.14.0", now); !ok {
			t.Error("notice should be due")
		}
	}()
	wg.Wait()

	s := LoadState()
	if s.NoticeShownFor != "v0.15.0" {
		t.Errorf("refresh wiped the notice marker: NoticeShownFor = %q", s.NoticeShownFor)
	}
	if s.LatestRelease != "v0.15.0" {
		t.Errorf("LatestRelease = %q, want v0.15.0", s.LatestRelease)
	}
}

func TestFetchLatestTag_ParsesTagName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v0.15.0"})
	}))
	defer srv.Close()

	old := releasesURL
	releasesURL = srv.URL
	defer func() { releasesURL = old }()

	tag, err := FetchLatestTag(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v0.15.0" {
		t.Fatalf("tag = %q, want v0.15.0", tag)
	}
}
