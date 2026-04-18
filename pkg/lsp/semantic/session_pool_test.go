package semantic

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeSessionAdapter struct {
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
	a.runCount++
	if a.err != nil {
		return ToolResult{}, a.err
	}
	return ToolResult{Capabilities: Capabilities{Diagnostics: true}}, nil
}

func (a *fakeSessionAdapter) Healthy() bool { return a.healthy && !a.closed }

func (a *fakeSessionAdapter) Close() error {
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
