//go:build !js

package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A vision provider that never answers is given up on after the attempt
// timeout, with an error saying so, instead of holding the tool call open.
func TestDoVisionRetryGivesUpOnASilentProvider(t *testing.T) {
	start := time.Now()
	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}, RetryOptions{OpName: "analyze_image", AttemptTimeout: 50 * time.Millisecond, TotalTimeout: time.Second, BaseDelay: time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "no answer from the vision model") {
		t.Fatalf("err = %v, want the no-answer error", err)
	}
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatalf("took %s, want about one attempt timeout", took)
	}
}

// The total timeout bounds the call across retries.
func TestDoVisionRetryTotalTimeout(t *testing.T) {
	start := time.Now()
	_ = DoVisionRetry(context.Background(), func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}, RetryOptions{AttemptTimeout: time.Minute, TotalTimeout: 80 * time.Millisecond, MaxAttempts: 5, BaseDelay: time.Millisecond})
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatalf("took %s, want the total timeout", took)
	}
}
