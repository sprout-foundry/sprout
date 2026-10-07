package verify

import (
	"context"
	"testing"
	"time"
)

// TestStopDevServerNilSafety pins the safety contract the page and
// interaction checks rely on when they `defer stopDevServer(...)` immediately
// after `startDevServer`: a nil handle and a handle whose process never
// started (a spawn failure leaves proc nil) are both safe no-ops.
// startDevServer always returns a non-nil handle, but the deferred stop must
// stay panic-free regardless.
func TestStopDevServerNilSafety(t *testing.T) {
	stopDevServer(nil)
	stopDevServer(&devServer{})
}

// TestStopDevServerIdempotent pins that stopping an already-reaped server is a
// no-op: startDevServer stops and reaps the process on its failure paths, and
// the caller's deferred stop runs again on the same handle.
func TestStopDevServerIdempotent(t *testing.T) {
	shAvailable(t)
	ds, reason := startDevServer(context.Background(), t.TempDir(), "true", 1, 5*time.Second)
	_ = reason
	if ds == nil {
		t.Fatal("startDevServer must always return a non-nil handle")
	}
	stopDevServer(ds)
	stopDevServer(ds) // second stop on the reaped handle must be a no-op
}
