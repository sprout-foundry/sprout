//go:build !js && cgo

package embedding

import (
	"context"
	"testing"
	"time"
)

// TestSharedONNXProvider_Dedup verifies the process-wide cache returns the same
// provider+runtime instances across calls for the same model, so multiple
// EmbeddingManagers/agents in one process (e.g. the WebUI daemon's per-session
// agents) share a single ~180MB model rather than each loading their own.
func TestSharedONNXProvider_Dedup(t *testing.T) {
	requireONNXTestModel(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	modelDir := DefaultModelDir()
	cfg := EmbeddingGemma300MConfig()

	p1, r1, err := acquireSharedONNXProvider(ctx, modelDir, cfg)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	p2, r2, err := acquireSharedONNXProvider(ctx, modelDir, cfg)
	if err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}

	if p1 != p2 {
		t.Errorf("expected the same shared provider instance, got two distinct providers")
	}
	if r1 != r2 {
		t.Errorf("expected the same shared runtime instance, got two distinct runtimes")
	}

	// The shared provider must remain usable (not closed) after a manager that
	// borrowed it is closed.
	mgr := NewEmbeddingManager(nil, t.TempDir())
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("manager init failed: %v", err)
	}
	_ = mgr.Close() // must NOT close the shared provider
	if _, err := p1.Embed(ctx, "still alive after manager close"); err != nil {
		t.Errorf("shared provider was torn down by manager.Close(): %v", err)
	}
}
