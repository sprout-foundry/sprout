//go:build !js

package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBusyCount(t *testing.T) {
	if got := busyCount(nil); got != 0 {
		t.Errorf("busyCount(nil) = %d, want 0", got)
	}
	got := busyCount(&healthSnapshot{ActiveQueries: 2})
	if got != 2 {
		t.Errorf("busyCount(2 queries) = %d, want 2", got)
	}
}

func TestDrainActiveQueriesDrainsImmediately(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","active_queries":0}`))
	}))
	defer srv.Close()

	prevURL := sessionCheckURL
	sessionCheckURL = srv.URL + "/no-sessions"
	t.Cleanup(func() { sessionCheckURL = prevURL })
	setServiceURLForTest(t, srv.URL)

	if err := drainActiveQueries(2 * time.Second); err != nil {
		t.Fatalf("drainActiveQueries: %v", err)
	}
}

func TestDrainActiveQueriesTimesOutWhenBusy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","active_queries":3}`))
	}))
	defer srv.Close()

	prevURL := sessionCheckURL
	sessionCheckURL = srv.URL + "/no-sessions"
	t.Cleanup(func() { sessionCheckURL = prevURL })
	setServiceURLForTest(t, srv.URL)

	err := drainActiveQueries(300 * time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error while busy, got nil")
	}
	if !strings.Contains(err.Error(), "3 quer") {
		t.Errorf("error should name the remaining queries, got: %v", err)
	}
}

func TestDrainActiveQueriesStopsWhenDaemonVanishes(t *testing.T) {
	// A server that immediately closes listeners: simplest way to simulate
	// the daemon exiting mid-drain is an unreachable URL.
	prevURL := sessionCheckURL
	sessionCheckURL = "http://127.0.0.1:1/none"
	t.Cleanup(func() { sessionCheckURL = prevURL })
	setServiceURLForTest(t, "http://127.0.0.1:1")

	if err := drainActiveQueries(5 * time.Second); err != nil {
		t.Fatalf("drain should succeed when the daemon goes away, got: %v", err)
	}
}

func TestDrainActiveQueriesCountsBackgroundSessions(t *testing.T) {
	// /health reports no active queries, but /api/terminal/agent-sessions
	// reports 2 background sessions — the drain must keep waiting.
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","active_queries":0}`))
	})
	mux.HandleFunc("/api/terminal/agent-sessions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"count":2}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	prevURL := sessionCheckURL
	sessionCheckURL = srv.URL + "/api/terminal/agent-sessions"
	t.Cleanup(func() { sessionCheckURL = prevURL })
	setServiceURLForTest(t, srv.URL)

	err := drainActiveQueries(300 * time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout while background sessions are active")
	}
	if !strings.Contains(err.Error(), "2 background session") {
		t.Errorf("error should name background sessions, got: %v", err)
	}
}

func TestWaitHealthyAfterAcceptsFirstWhenNoPrev(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","version":"v1"}`))
	}))
	defer srv.Close()
	setServiceURLForTest(t, srv.URL)

	hs := waitHealthyAfter("", time.Second)
	if hs == nil || hs.Version != "v1" {
		t.Fatalf("waitHealthyAfter = %v, want version v1", hs)
	}
}

func TestWaitHealthyAfterWaitsForNewVersion(t *testing.T) {
	// The daemon reports the OLD version for the first 2 polls, then the
	// new one. waitHealthyAfter("v1") must not return early.
	var polls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		polls++
		w.WriteHeader(http.StatusOK)
		if polls <= 2 {
			_, _ = w.Write([]byte(`{"status":"ok","version":"v1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok","version":"v2"}`))
	}))
	defer srv.Close()
	setServiceURLForTest(t, srv.URL)

	prevPollInterval := healthPollInterval
	healthPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { healthPollInterval = prevPollInterval })

	hs := waitHealthyAfter("v1", 5*time.Second)
	if hs == nil || hs.Version != "v2" {
		t.Fatalf("waitHealthyAfter(v1) = %v, want version v2", hs)
	}
}

func TestWaitHealthyAfterTimesOutOnSameVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","version":"v1"}`))
	}))
	defer srv.Close()
	setServiceURLForTest(t, srv.URL)

	prevPollInterval := healthPollInterval
	healthPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { healthPollInterval = prevPollInterval })

	if hs := waitHealthyAfter("v1", 200*time.Millisecond); hs != nil {
		t.Fatalf("expected timeout while version stays v1, got %v", hs)
	}
}
