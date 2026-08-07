//go:build !wasm && cgo

package embedding

import (
	"context"
	"fmt"

	onnxruntime "github.com/yalue/onnxruntime_go"
)

// runSessionWithOptions wraps session.RunWithOptions with ctx-aware termination.
// ORT's session.Run ignores Go contexts; a hung Run holds the inference gate
// permit forever. Terminate() on ctx cancellation lets the in-flight Run
// return promptly.
func runSessionWithOptions(
	ctx context.Context,
	session *onnxruntime.DynamicAdvancedSession,
	inputs, outputs []onnxruntime.Value,
) error {
	opts, err := onnxruntime.NewRunOptions()
	if err != nil {
		// Fall back to plain Run — better to try than to fail the embed.
		return session.Run(inputs, outputs)
	}
	defer opts.Destroy()

	// Watchdog: on ctx cancellation, signal ORT to terminate the Run.
	// done is closed when runSessionWithOptions returns so the watchdog
	// knows to exit without racing the deferred opts.Destroy().
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			// Best-effort: if Terminate fails (e.g. opts already destroyed),
			// there's nothing useful to do — the Run will still return when
			// it returns. The flag set in C is what makes it return sooner.
			_ = opts.Terminate()
		case <-done:
		}
	}()

	if err := session.RunWithOptions(inputs, outputs, opts); err != nil {
		// If the watchdog fired, surface ctx.Err() so callers see the cause
		// rather than the ORT-internal terminate error string.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("onnx run (terminated): %w", ctxErr)
		}
		return err
	}
	return nil
}
