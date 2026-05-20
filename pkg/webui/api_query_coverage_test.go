//go:build !js

package webui

import (
	"sync"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/stretchr/testify/assert"
)

func newMinimalTestServer(t *testing.T) *ReactWebServer {
	t.Helper()
	return &ReactWebServer{
		eventBus:       events.NewEventBus(),
		clientContexts: make(map[string]*webClientContext),
	}
}

// =====================================================================
// incrementActiveQueriesWithQuery
// =====================================================================

func TestQueryCoverage_IncrementActiveQueriesWithQuery_Basic(t *testing.T) {
	ws := newMinimalTestServer(t)

	ws.incrementActiveQueriesWithQuery("client1", "test query")

	assert.Equal(t, 1, ws.activeQueries, "activeQueries should be 1 after increment")
	ctx := ws.clientContexts["client1"]
	assert.NotNil(t, ctx, "client context should be created")
	assert.True(t, ctx.ActiveQuery, "ActiveQuery should be true")
	assert.Equal(t, "test query", ctx.CurrentQuery, "CurrentQuery should be set")
}

func TestQueryCoverage_IncrementActiveQueriesWithQuery_IncrementsCounter(t *testing.T) {
	ws := newMinimalTestServer(t)

	ws.incrementActiveQueriesWithQuery("c1", "query1")
	ws.incrementActiveQueriesWithQuery("c2", "query2")

	assert.Equal(t, 2, ws.activeQueries, "activeQueries should be 2")
}

func TestQueryCoverage_IncrementActiveQueriesWithQuery_CreatesNewContext(t *testing.T) {
	ws := newMinimalTestServer(t)

	// No contexts exist yet
	assert.Nil(t, ws.clientContexts["new-client"])

	ws.incrementActiveQueriesWithQuery("new-client", "hello")

	ctx := ws.clientContexts["new-client"]
	assert.NotNil(t, ctx, "should create context for new client")
	assert.True(t, ctx.ActiveQuery)
	assert.Equal(t, "hello", ctx.CurrentQuery)
}

func TestQueryCoverage_IncrementActiveQueriesWithQuery_ExistingContext(t *testing.T) {
	ws := newMinimalTestServer(t)

	// Seed an existing context
	ws.mutex.Lock()
	ws.clientContexts["c1"] = &webClientContext{
		WorkspaceRoot: "/tmp",
	}
	ws.mutex.Unlock()

	ws.incrementActiveQueriesWithQuery("c1", "new query")

	ctx := ws.clientContexts["c1"]
	assert.True(t, ctx.ActiveQuery)
	assert.Equal(t, "new query", ctx.CurrentQuery)
	assert.Equal(t, 1, ws.activeQueries)
}

// =====================================================================
// hasActiveQuery
// =====================================================================

func TestQueryCoverage_HasActiveQuery_InitiallyFalse(t *testing.T) {
	ws := newMinimalTestServer(t)

	assert.False(t, ws.hasActiveQuery(), "should be false when no queries active")
}

func TestQueryCoverage_HasActiveQuery_AfterIncrement(t *testing.T) {
	ws := newMinimalTestServer(t)

	ws.incrementActiveQueriesWithQuery("c1", "q")

	assert.True(t, ws.hasActiveQuery(), "should be true after increment")
}

func TestQueryCoverage_HasActiveQuery_AfterDecrement(t *testing.T) {
	ws := newMinimalTestServer(t)

	ws.incrementActiveQueriesWithQuery("c1", "q")
	ws.decrementActiveQueries("c1")

	assert.False(t, ws.hasActiveQuery(), "should be false after decrement")
}

// =====================================================================
// incrementActiveQueries (already 100%, but test edge cases)
// =====================================================================

func TestQueryCoverage_IncrementActiveQueries_Basic(t *testing.T) {
	ws := newMinimalTestServer(t)

	ws.incrementActiveQueries("c1")

	assert.Equal(t, 1, ws.activeQueries)
	ctx := ws.clientContexts["c1"]
	assert.NotNil(t, ctx)
	assert.True(t, ctx.ActiveQuery)
}

// =====================================================================
// Concurrent access
// =====================================================================

func TestQueryCoverage_ConcurrentIncrements(t *testing.T) {
	ws := newMinimalTestServer(t)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ws.incrementActiveQueriesWithQuery("client", "query")
		}()
	}
	wg.Wait()

	assert.Equal(t, 100, ws.activeQueries, "should handle concurrent increments")
}

func TestQueryCoverage_DecrementNeverNegative(t *testing.T) {
	ws := newMinimalTestServer(t)

	ws.decrementActiveQueries("nonexistent")

	assert.Equal(t, 0, ws.activeQueries, "decrement should not go negative")
}
