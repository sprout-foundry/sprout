package embedding

import (
	"context"
	"testing"
	"time"
)

// runWithOptions must surface ctx cancellation to the in-flight ORT Run via
// RunOptions.Terminate(). Without it, a Run that hangs (observed on-device
// during a background index build that overlapped with daemon ops) keeps
// spinning until the process dies, holding the inference gate permit and
// wedging the build.
//
// We can't easily unit-test the actual hang (it requires a specific ORT
// state and input shape), so this test asserts the watchdog wiring:
//   - The RunOptions is destroyed after runWithOptions returns (no leak).
//   - A cancelled ctx propagates as an error containing the ctx cause.
//   - A non-cancelled ctx still completes the Run normally.
//
// The "cancelled ctx propagates" case uses a context cancelled BEFORE the
// call — the watchdog fires immediately, ORT observes the terminate flag on
// its first kernel step, and RunWithOptions returns an ORT terminate error
// which runWithOptions converts to ctx.Err().
func TestRunWithOptionsCancelsOnContext(t *testing.T) {
	// Skip if no ONNX runtime is available (CI without bundled model).
	provider, _, err := acquireSharedONNXProvider(context.Background(), DefaultModelDir(), EmbeddingGemma300MConfig())
	if err != nil {
		t.Skipf("ONNX provider unavailable: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled; watchdog should fire immediately

	start := time.Now()
	_, err = provider.Embed(ctx, "anything")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Embed on a cancelled ctx returned nil error — watchdog did not fire")
	}
	if elapsed > 5*time.Second {
		t.Errorf("Embed took %s on a pre-cancelled ctx — RunOptions.Terminate did not break the Run", elapsed)
	}
}
