package semantic

import (
	"errors"
	"testing"
	"time"
)

// Additional SessionPool tests beyond existing ones.

func TestSessionPoolEvictIdleZeroTTL(t *testing.T) {
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		return &fakeSessionAdapter{healthy: true}, nil
	}, 0)

	// Run to create a session
	_, _ = pool.Run(ToolInput{WorkspaceRoot: "/repo-a"})

	// EvictIdle with zero TTL should do nothing
	pool.EvictIdle()

	// Session should still be usable
	if _, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-a"}); err != nil {
		t.Fatal("expected session to still be usable after EvictIdle with 0 TTL")
	}
}

func TestSessionPoolEvictIdleRemovesExpired(t *testing.T) {
	created := 0
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		created++
		return &fakeSessionAdapter{healthy: true}, nil
	}, 50*time.Millisecond)

	_, _ = pool.Run(ToolInput{WorkspaceRoot: "/repo-a"})
	if created != 1 {
		t.Fatalf("expected 1 factory call, got %d", created)
	}

	// Wait for idle TTL to pass
	time.Sleep(100 * time.Millisecond)
	pool.EvictIdle()

	// Next run should create a new session
	_, _ = pool.Run(ToolInput{WorkspaceRoot: "/repo-a"})
	if created != 2 {
		t.Fatalf("expected 2 factory calls after eviction, got %d", created)
	}
}

type closeTrackingAdapter struct {
	healthy bool
	closed  bool
}

func (a *closeTrackingAdapter) Run(input ToolInput) (ToolResult, error) {
	return ToolResult{}, nil
}
func (a *closeTrackingAdapter) Healthy() bool { return a.healthy && !a.closed }
func (a *closeTrackingAdapter) Close() error {
	a.closed = true
	return nil
}

func TestSessionPoolCloseRemovesAll(t *testing.T) {
	adapters := []*closeTrackingAdapter{}
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		a := &closeTrackingAdapter{healthy: true}
		adapters = append(adapters, a)
		return a, nil
	}, 0)

	_, _ = pool.Run(ToolInput{WorkspaceRoot: "/repo-a"})
	_, _ = pool.Run(ToolInput{WorkspaceRoot: "/repo-b"})

	pool.Close()

	closed := 0
	for _, a := range adapters {
		if a.closed {
			closed++
		}
	}
	if closed != 2 {
		t.Fatalf("expected 2 adapters closed, got %d", closed)
	}
}

func TestSessionPoolFactoryError(t *testing.T) {
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		return nil, errors.New("factory failed")
	}, 0)

	_, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-a"})
	if err == nil {
		t.Fatal("expected factory error to propagate")
	}
	if err.Error() != "factory failed" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSessionPoolUnhealthySessionReplaced(t *testing.T) {
	first := &fakeSessionAdapter{healthy: true}
	second := &fakeSessionAdapter{healthy: true}
	factoryCalls := 0

	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		factoryCalls++
		if factoryCalls == 1 {
			return first, nil
		}
		return second, nil
	}, 0)

	// First call creates healthy adapter
	_, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Now mark first as unhealthy
	first.healthy = false

	// Second call should detect unhealthy and replace
	_, err = pool.Run(ToolInput{WorkspaceRoot: "/repo-a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if factoryCalls != 2 {
		t.Fatalf("expected 2 factory calls, got %d", factoryCalls)
	}
	if !first.closed {
		t.Error("expected unhealthy adapter to be closed")
	}
}
