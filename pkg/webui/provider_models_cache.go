//go:build !js

package webui

import (
	"context"
	"sync"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// Live model discovery asks each provider's API (and the remote model
// registry) for its model list. Behind an egress proxy, or with a provider
// host unreachable, every lookup runs to its timeout — and the provider
// listing asks every known provider, so an uncached listing took as long as
// the slowest host allowed. The cache below makes the listing independent
// of remote latency: each provider's live result is fetched once in the
// background, served from memory afterwards, and refreshed in the
// background when stale. Failures are cached too, so an unreachable host
// is retried on a schedule rather than on every request.
const (
	// providerModelsFetchTimeout bounds one background lookup.
	providerModelsFetchTimeout = 10 * time.Second
	// providerModelsFreshFor is how long a successful lookup is served
	// before a background refresh starts.
	providerModelsFreshFor = 10 * time.Minute
	// providerModelsRetryAfter is how long a failed lookup is served (as a
	// failure, so callers use their fallbacks) before it is retried.
	providerModelsRetryAfter = 2 * time.Minute
)

// providerModelsFetchFunc performs one live model lookup for a provider.
type providerModelsFetchFunc func(ctx context.Context, pt api.ClientType) ([]api.ModelInfo, error)

// providerModelsCache holds the latest live model lookup per provider for
// the life of the process. The zero value is ready to use.
type providerModelsCache struct {
	mu      sync.Mutex
	entries map[api.ClientType]*providerModelsEntry
	// fetch overrides the live lookup (tests); nil uses
	// api.GetModelsForProviderCtx.
	fetch providerModelsFetchFunc
}

type providerModelsEntry struct {
	models  []api.ModelInfo
	err     error
	fetched bool          // a lookup has completed at least once
	retryAt time.Time     // when the cached result goes stale
	running chan struct{} // non-nil while a lookup runs; closed when it ends
	started time.Time     // when the running lookup started
}

// lookup returns the cached live result for pt. When there is none, or it
// is stale, it starts a background lookup — never more than one per
// provider at a time. A stale result is returned at once; with no result
// yet, lookup waits for the background lookup until wait has passed since
// that lookup started (or until ctx is done), so concurrent and repeated
// callers share one wait budget instead of each paying it again. ok is
// false when no result was available in time; the lookup keeps running and
// later calls see its result.
func (c *providerModelsCache) lookup(ctx context.Context, pt api.ClientType, wait time.Duration) (models []api.ModelInfo, err error, ok bool) {
	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[api.ClientType]*providerModelsEntry)
	}
	e := c.entries[pt]
	if e == nil {
		e = &providerModelsEntry{}
		c.entries[pt] = e
	}
	if e.running == nil && (!e.fetched || !time.Now().Before(e.retryAt)) {
		e.running = make(chan struct{})
		e.started = time.Now()
		go c.refresh(context.WithoutCancel(ctx), pt, e, e.running)
	}
	if e.fetched {
		models, err = e.models, e.err
		c.mu.Unlock()
		return models, err, true
	}
	running := e.running
	wait -= time.Since(e.started)
	c.mu.Unlock()

	if wait <= 0 {
		return nil, nil, false
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-running:
		c.mu.Lock()
		defer c.mu.Unlock()
		return e.models, e.err, e.fetched
	case <-timer.C:
	case <-ctx.Done():
	}
	return nil, nil, false
}

// refresh runs one live lookup. parent carries the triggering request's
// values but not its cancellation, so a client disconnect doesn't discard a
// result other requests are waiting for; providerModelsFetchTimeout bounds it.
func (c *providerModelsCache) refresh(parent context.Context, pt api.ClientType, e *providerModelsEntry, running chan struct{}) {
	fetch := c.fetch
	if fetch == nil {
		fetch = api.GetModelsForProviderCtx
	}
	ctx, cancel := context.WithTimeout(parent, providerModelsFetchTimeout)
	models, err := fetch(ctx, pt)
	cancel()

	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	switch {
	case err == nil:
		e.models, e.err = models, nil
		e.retryAt = now.Add(providerModelsFreshFor)
	case e.fetched && e.err == nil:
		// Keep serving the last good list; retry the refresh later.
		e.retryAt = now.Add(providerModelsRetryAfter)
	default:
		e.models, e.err = nil, err
		e.retryAt = now.Add(providerModelsRetryAfter)
	}
	e.fetched = true
	e.running = nil
	close(running)
}
