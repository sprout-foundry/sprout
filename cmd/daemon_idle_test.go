package cmd

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeIdleServer is a controllable idleServer for reapIdleDaemon tests.
type fakeIdleServer struct {
	mu            sync.Mutex
	activeClients int
	activeQueries int
}

func (f *fakeIdleServer) ActiveClientCount(time.Duration) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.activeClients
}

func (f *fakeIdleServer) ActiveQueryCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.activeQueries
}

func (f *fakeIdleServer) setClients(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.activeClients = n
}

func (f *fakeIdleServer) setQueries(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.activeQueries = n
}

// TestReapIdleDaemon_FiresAfterIdle verifies the daemon self-terminates once
// no client has been active for the idle window.
func TestReapIdleDaemon_FiresAfterIdle(t *testing.T) {
	oldInterval := idleCheckInterval
	idleCheckInterval = 20 * time.Millisecond
	defer func() { idleCheckInterval = oldInterval }()

	srv := &fakeIdleServer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var cancelled int32
	idleCancel := func() { atomic.AddInt32(&cancelled, 1); cancel() }

	go reapIdleDaemon(ctx, idleCancel, srv, 100*time.Millisecond)

	require.Eventually(t, func() bool { return atomic.LoadInt32(&cancelled) > 0 },
		3*time.Second, 20*time.Millisecond,
		"daemon should cancel after the idle window with no clients")
}

// TestReapIdleDaemon_ActiveClientSuppressesReap verifies a recently-seen
// client keeps the daemon alive well past the idle window.
func TestReapIdleDaemon_ActiveClientSuppressesReap(t *testing.T) {
	oldInterval := idleCheckInterval
	idleCheckInterval = 20 * time.Millisecond
	defer func() { idleCheckInterval = oldInterval }()

	srv := &fakeIdleServer{activeClients: 1}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var cancelled int32
	go reapIdleDaemon(ctx, func() { atomic.AddInt32(&cancelled, 1); cancel() }, srv, 100*time.Millisecond)

	time.Sleep(400 * time.Millisecond)
	require.Zero(t, atomic.LoadInt32(&cancelled),
		"daemon must NOT cancel while a client is active")
}

// TestReapIdleDaemon_ActiveQuerySuppressesReap verifies an in-flight query
// keeps the daemon alive even with no client contexts.
func TestReapIdleDaemon_ActiveQuerySuppressesReap(t *testing.T) {
	oldInterval := idleCheckInterval
	idleCheckInterval = 20 * time.Millisecond
	defer func() { idleCheckInterval = oldInterval }()

	srv := &fakeIdleServer{activeQueries: 1}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var cancelled int32
	go reapIdleDaemon(ctx, func() { atomic.AddInt32(&cancelled, 1); cancel() }, srv, 100*time.Millisecond)

	time.Sleep(400 * time.Millisecond)
	require.Zero(t, atomic.LoadInt32(&cancelled),
		"daemon must NOT cancel while a query is active")
}

// TestReapIdleDaemon_StopsOnContextCancel verifies the goroutine exits when
// the daemon is shut down by other means (signal), without calling cancel.
func TestReapIdleDaemon_StopsOnContextCancel(t *testing.T) {
	oldInterval := idleCheckInterval
	idleCheckInterval = 10 * time.Millisecond
	defer func() { idleCheckInterval = oldInterval }()

	srv := &fakeIdleServer{}
	ctx, cancel := context.WithCancel(context.Background())

	var cancelled int32
	go reapIdleDaemon(ctx, func() { atomic.AddInt32(&cancelled, 1); cancel() }, srv, 100*time.Millisecond)

	time.Sleep(50 * time.Millisecond)
	cancel() // external shutdown (e.g. SIGTERM path)
	time.Sleep(100 * time.Millisecond)
	require.Zero(t, atomic.LoadInt32(&cancelled),
		"daemon must not fire the idle cancel after external shutdown")
}
