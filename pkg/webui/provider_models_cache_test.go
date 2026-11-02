//go:build !js

package webui

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// blockingFetch returns a fetcher that blocks until release is closed (or
// its context ends) and counts its calls per provider.
func blockingFetch(release <-chan struct{}, calls *atomic.Int32, models []api.ModelInfo, err error) providerModelsFetchFunc {
	return func(ctx context.Context, pt api.ClientType) ([]api.ModelInfo, error) {
		calls.Add(1)
		select {
		case <-release:
			return models, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func waitForLookup(t *testing.T, c *providerModelsCache, pt api.ClientType) ([]api.ModelInfo, error) {
	t.Helper()
	models, err, ok := c.lookup(context.Background(), pt, 2*time.Second)
	if !ok {
		t.Fatalf("lookup for %s did not complete", pt)
	}
	return models, err
}

func TestProviderModelsCache_FirstLookupWaitsAtMostWait(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	c := &providerModelsCache{fetch: blockingFetch(release, &calls, []api.ModelInfo{{ID: "m1"}}, nil)}

	for i := 0; i < 3; i++ {
		start := time.Now()
		_, _, ok := c.lookup(context.Background(), "slow", 30*time.Millisecond)
		if ok {
			t.Fatal("lookup reported a result while the fetch is still running")
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("lookup took %v with a 30ms wait", elapsed)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fetches while one is running = %d, want 1", got)
	}

	close(release)
	models, err := waitForLookup(t, c, "slow")
	if err != nil || len(models) != 1 || models[0].ID != "m1" {
		t.Fatalf("lookup after the fetch = (%v, %v), want [m1]", models, err)
	}
}

func TestProviderModelsCache_ZeroWaitNeverBlocks(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	var calls atomic.Int32
	c := &providerModelsCache{fetch: blockingFetch(release, &calls, nil, nil)}

	start := time.Now()
	if _, _, ok := c.lookup(context.Background(), "slow", 0); ok {
		t.Fatal("zero-wait lookup reported a result before any fetch completed")
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("zero-wait lookup took %v", elapsed)
	}
}

func TestProviderModelsCache_FailureIsCached(t *testing.T) {
	release := make(chan struct{})
	close(release)
	var calls atomic.Int32
	fetchErr := errors.New("unreachable")
	c := &providerModelsCache{fetch: blockingFetch(release, &calls, nil, fetchErr)}

	if _, err := waitForLookup(t, c, "down"); !errors.Is(err, fetchErr) {
		t.Fatalf("first lookup err = %v, want %v", err, fetchErr)
	}
	for i := 0; i < 5; i++ {
		if _, err, ok := c.lookup(context.Background(), "down", 0); !ok || !errors.Is(err, fetchErr) {
			t.Fatalf("cached lookup %d = (ok=%v, err=%v), want the cached failure", i, ok, err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fetches = %d, want 1 (failures are cached until the retry time)", got)
	}
}

func TestProviderModelsCache_StaleResultServedWhileRefreshing(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	c := &providerModelsCache{fetch: blockingFetch(release, &calls, []api.ModelInfo{{ID: "new"}}, nil)}
	c.entries = map[api.ClientType]*providerModelsEntry{
		"p": {models: []api.ModelInfo{{ID: "old"}}, fetched: true, retryAt: time.Now().Add(-time.Second)},
	}

	start := time.Now()
	models, err, ok := c.lookup(context.Background(), "p", time.Minute)
	if !ok || err != nil || len(models) != 1 || models[0].ID != "old" {
		t.Fatalf("stale lookup = (%v, %v, ok=%v), want the stale [old]", models, err, ok)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("stale lookup waited %v for the refresh", elapsed)
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		models, _, _ := c.lookup(context.Background(), "p", 0)
		if len(models) == 1 && models[0].ID == "new" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("refreshed list never replaced the stale one: %v", models)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fetches = %d, want 1", got)
	}
}

func TestProviderModelsCache_FailedRefreshKeepsLastGoodList(t *testing.T) {
	release := make(chan struct{})
	close(release)
	var calls atomic.Int32
	c := &providerModelsCache{fetch: blockingFetch(release, &calls, nil, errors.New("flaky"))}
	c.entries = map[api.ClientType]*providerModelsEntry{
		"p": {models: []api.ModelInfo{{ID: "good"}}, fetched: true, retryAt: time.Now().Add(-time.Second)},
	}

	_, _, _ = c.lookup(context.Background(), "p", 0) // starts the failing refresh
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 || c.running("p") {
		if time.Now().After(deadline) {
			t.Fatal("refresh never finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
	models, err, ok := c.lookup(context.Background(), "p", 0)
	if !ok || err != nil || len(models) != 1 || models[0].ID != "good" {
		t.Fatalf("after a failed refresh = (%v, %v, ok=%v), want the last good [good]", models, err, ok)
	}
}

// running reports whether a lookup for pt is in flight (tests only).
func (c *providerModelsCache) running(pt api.ClientType) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[pt]
	return e != nil && e.running != nil
}
