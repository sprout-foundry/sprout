package embedding

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// defaultBatchAttentionBudget bounds one ORT call's working set; this bounds
// how many run at once. Without the second bound the first is not a budget —
// the daemon ran four concurrent builds through a shared provider whose Embed
// path takes only a read lock, and RSS went from 1 GB to 4.5 GB.
func TestAcquireInferenceCapsConcurrency(t *testing.T) {
	var live, peak int64

	var wg sync.WaitGroup
	for i := 0; i < maxConcurrentInference*4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := acquireInference(context.Background())
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			defer release()

			n := atomic.AddInt64(&live, 1)
			for {
				old := atomic.LoadInt64(&peak)
				if n <= old || atomic.CompareAndSwapInt64(&peak, old, n) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			atomic.AddInt64(&live, -1)
		}()
	}
	wg.Wait()

	if peak > maxConcurrentInference {
		t.Errorf("peak concurrent inferences = %d, want <= %d", peak, maxConcurrentInference)
	}
	if peak < 2 {
		t.Errorf("peak concurrent inferences = %d; the gate serialized more than intended", peak)
	}
	if got := atomic.LoadInt64(&live); got != 0 {
		t.Errorf("%d permits still held after all releases", got)
	}
}

// A cancelled build must not sit blocked behind a long batch from another
// workspace — waiters honor the caller's context.
func TestAcquireInferenceHonorsContext(t *testing.T) {
	var held []func()
	for i := 0; i < maxConcurrentInference; i++ {
		release, err := acquireInference(context.Background())
		if err != nil {
			t.Fatalf("saturating acquire: %v", err)
		}
		held = append(held, release)
	}
	defer func() {
		for _, release := range held {
			release()
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	release, err := acquireInference(ctx)
	if err == nil {
		release()
		t.Fatal("acquire succeeded on a cancelled context with the gate saturated")
	}
	if !errorsIsCancelled(err) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func errorsIsCancelled(err error) bool {
	return err == context.Canceled
}

// A nil context is treated as Background rather than panicking — callers deep
// in the provider do not all thread one.
func TestAcquireInferenceNilContext(t *testing.T) {
	release, err := acquireInference(nil)
	if err != nil {
		t.Fatalf("acquire with nil ctx: %v", err)
	}
	release()
}
