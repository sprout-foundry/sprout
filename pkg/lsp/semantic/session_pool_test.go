package semantic

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeSessionAdapter struct {
	mu       sync.Mutex
	runCount int
	healthy  bool
	closed   bool
	err      error
}

type blockingSessionAdapter struct {
	mu           sync.Mutex
	healthy      bool
	closed       bool
	runCount     int
	firstStarted chan struct{}
	unblockFirst chan struct{}
}

func (a *fakeSessionAdapter) Run(input ToolInput) (ToolResult, error) {
	_ = input
	a.mu.Lock()
	a.runCount++
	err := a.err
	a.mu.Unlock()
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Capabilities: Capabilities{Diagnostics: true}}, nil
}

func (a *fakeSessionAdapter) Healthy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.healthy && !a.closed
}

func (a *fakeSessionAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	return nil
}

func (a *blockingSessionAdapter) Run(input ToolInput) (ToolResult, error) {
	_ = input
	a.mu.Lock()
	a.runCount++
	runNum := a.runCount
	a.mu.Unlock()

	if runNum == 1 {
		close(a.firstStarted)
		<-a.unblockFirst
		return ToolResult{}, errors.New("boom")
	}
	return ToolResult{Capabilities: Capabilities{Diagnostics: true}}, nil
}

func (a *blockingSessionAdapter) Healthy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.healthy && !a.closed
}

func (a *blockingSessionAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	return nil
}

func TestSessionPoolReusesHealthyAdapterPerWorkspace(t *testing.T) {
	factoryCalls := 0
	var created []*fakeSessionAdapter
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		_ = workspaceRoot
		factoryCalls++
		adapter := &fakeSessionAdapter{healthy: true}
		created = append(created, adapter)
		return adapter, nil
	}, 0)

	if _, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-a"}); err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if _, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-a"}); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	if _, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-b"}); err != nil {
		t.Fatalf("third run failed: %v", err)
	}

	if factoryCalls != 2 {
		t.Fatalf("expected factory to be called once per workspace, got %d", factoryCalls)
	}
	if created[0].runCount != 2 {
		t.Fatalf("expected first workspace adapter to be reused, got runCount=%d", created[0].runCount)
	}
}

func TestSessionPoolEvictsFailedAdapter(t *testing.T) {
	factoryCalls := 0
	first := &fakeSessionAdapter{healthy: true, err: errors.New("boom")}
	second := &fakeSessionAdapter{healthy: true}
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		_ = workspaceRoot
		factoryCalls++
		if factoryCalls == 1 {
			return first, nil
		}
		return second, nil
	}, 0)

	if _, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-a"}); err == nil {
		t.Fatal("expected first run to fail")
	}
	if !first.closed {
		t.Fatal("expected failed adapter to be closed and evicted")
	}
	if _, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-a"}); err != nil {
		t.Fatalf("expected second run to recreate adapter, got error: %v", err)
	}
	if factoryCalls != 2 {
		t.Fatalf("expected factory to be called twice after eviction, got %d", factoryCalls)
	}
}

func TestSessionPoolDefersEvictionUntilInUseCallsFinish(t *testing.T) {
	first := &blockingSessionAdapter{
		healthy:      true,
		firstStarted: make(chan struct{}),
		unblockFirst: make(chan struct{}),
	}
	second := &fakeSessionAdapter{healthy: true}
	factoryCalls := 0
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		_ = workspaceRoot
		factoryCalls++
		if factoryCalls == 1 {
			return first, nil
		}
		return second, nil
	}, 0)

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = pool.Run(ToolInput{WorkspaceRoot: "/repo-a"})
	}()
	<-first.firstStarted

	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		_, _ = pool.Run(ToolInput{WorkspaceRoot: "/repo-a"})
	}()
	<-secondDone

	time.Sleep(10 * time.Millisecond)
	if first.closed {
		t.Fatal("expected adapter not to close while concurrent call is still in use")
	}

	close(first.unblockFirst)
	<-firstDone

	if !first.closed {
		t.Fatal("expected adapter to close after in-use calls completed")
	}

	if _, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-a"}); err != nil {
		t.Fatalf("expected recreated adapter to run, got: %v", err)
	}
	if factoryCalls != 2 {
		t.Fatalf("expected factory to be called twice, got %d", factoryCalls)
	}
}

func TestSessionPoolConcurrentAcquireSingleCreation(t *testing.T) {
	var mu sync.Mutex
	factoryCalls := 0
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		_ = workspaceRoot
		mu.Lock()
		factoryCalls++
		mu.Unlock()
		// Give other goroutines time to pile onto the claim channel while the
		// creator is still running — the exact interleaving that used to spawn
		// a redundant second factory call.
		time.Sleep(5 * time.Millisecond)
		return &fakeSessionAdapter{healthy: true}, nil
	}, 0)

	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-a"}); err != nil {
				t.Errorf("run failed: %v", err)
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	calls := factoryCalls
	mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected exactly one factory call for concurrent same-root acquires, got %d", calls)
	}
}

func TestSessionPoolConcurrentAcquireDifferentRoots(t *testing.T) {
	var mu sync.Mutex
	factoryCalls := 0
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		_ = workspaceRoot
		mu.Lock()
		factoryCalls++
		mu.Unlock()
		// Slow factory: if acquire serialized on p.mu across roots, these
		// concurrent requests would take ~5ms * 4 roots each and the total
		// elapsed time would reveal the lock contention.
		time.Sleep(5 * time.Millisecond)
		return &fakeSessionAdapter{healthy: true}, nil
	}, 0)

	roots := []string{"/repo-a", "/repo-b", "/repo-c", "/repo-d"}
	var wg sync.WaitGroup
	for _, root := range roots {
		wg.Add(1)
		go func(r string) {
			defer wg.Done()
			if _, err := pool.Run(ToolInput{WorkspaceRoot: r}); err != nil {
				t.Errorf("run failed for %s: %v", r, err)
			}
		}(root)
	}
	wg.Wait()

	mu.Lock()
	calls := factoryCalls
	mu.Unlock()
	if calls != len(roots) {
		t.Fatalf("expected one factory call per root, got %d for %d roots", calls, len(roots))
	}
}

func TestSessionPoolWaiterRetriesAfterFactoryError(t *testing.T) {
	var mu sync.Mutex
	factoryCalls := 0
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		_ = workspaceRoot
		mu.Lock()
		factoryCalls++
		call := factoryCalls
		mu.Unlock()
		if call == 1 {
			return nil, errors.New("transient factory failure")
		}
		return &fakeSessionAdapter{healthy: true}, nil
	}, 0)

	results := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-a"})
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	errs := 0
	oks := 0
	for err := range results {
		if err != nil {
			errs++
		} else {
			oks++
		}
	}
	// The goroutine that lost the creation race becomes a waiter and retries
	// after the factory failure; the creator's own caller sees the error.
	if errs != 1 || oks != 1 {
		t.Fatalf("expected one error (creator) and one success (waiter retry), got errs=%d oks=%d", errs, oks)
	}

	mu.Lock()
	calls := factoryCalls
	mu.Unlock()
	if calls != 2 {
		t.Fatalf("expected first factory call to fail and a retry to succeed, got %d calls", calls)
	}
}

func TestSessionPoolReleaseRecyclesIdlePastTTL(t *testing.T) {
	factoryCalls := 0
	var created []*fakeSessionAdapter
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		_ = workspaceRoot
		factoryCalls++
		adapter := &fakeSessionAdapter{healthy: true}
		created = append(created, adapter)
		return adapter, nil
	}, 50*time.Millisecond)

	if _, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-a"}); err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if created[0].closed {
		t.Fatal("expected adapter to stay open immediately after release")
	}

	time.Sleep(80 * time.Millisecond)

	// Run 2: the second acquire observes the entry idle past TTL, marks it for
	// eviction, and release closes it — so a session that sat unused is
	// recycled promptly instead of lingering until the next EvictIdle sweep.
	if _, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-a"}); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	if !created[0].closed {
		t.Fatal("expected idle-past-TTL adapter to be recycled on release")
	}
	if factoryCalls != 1 {
		t.Fatalf("expected no factory call when the idle adapter is still reused for run 2, got %d", factoryCalls)
	}

	// Run 3: pool must now create a fresh adapter (the idle one was closed).
	if _, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-a"}); err != nil {
		t.Fatalf("third run failed: %v", err)
	}
	if factoryCalls != 2 {
		t.Fatalf("expected a fresh adapter after TTL recycling, got %d factory calls", factoryCalls)
	}
}
