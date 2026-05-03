package utils

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewTokenBucket(t *testing.T) {
	tb := NewTokenBucket(1.0, 5)

	if tb.GetRate() != 1.0 {
		t.Errorf("Expected rate 1.0, got %f", tb.GetRate())
	}
	if tb.GetBurst() != 5 {
		t.Errorf("Expected burst 5, got %d", tb.GetBurst())
	}
	// Should start with burst tokens
	if tb.GetAvailableTokens() != 5.0 {
		t.Errorf("Expected 5 tokens, got %f", tb.GetAvailableTokens())
	}
}

func TestWaitBasic(t *testing.T) {
	tb := NewTokenBucket(10.0, 5) // 10 tokens per second, burst 5
	ctx := context.Background()

	// First 5 requests should succeed immediately (burst)
	for i := 0; i < 5; i++ {
		start := time.Now()
		err := tb.Wait(ctx)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if elapsed > 100*time.Millisecond {
			t.Errorf("Wait should be immediate for first %d requests, took %v", 5, elapsed)
		}
	}

	// 6th request should wait for token refill
	// With the new reservation-based algorithm, all waiters can claim their reservation
	// slots immediately, so the actual wait depends on the reservation timing
	start := time.Now()
	err := tb.Wait(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// With rate 10 tps, we should wait ~100ms for the 6th token
	// The test range is wider to accommodate implementation differences
	if elapsed < 80*time.Millisecond || elapsed > 300*time.Millisecond {
		t.Errorf("Expected wait ~100ms, got %v", elapsed)
	}
}

func TestTryWaitBasic(t *testing.T) {
	tb := NewTokenBucket(10.0, 3)

	// First 3 TryWait should succeed (burst)
	for i := 0; i < 3; i++ {
		if !tb.TryWait() {
			t.Errorf("TryWait should succeed for first %d requests", 3)
		}
	}

	// 4th TryWait should fail
	if tb.TryWait() {
		t.Errorf("TryWait should fail when bucket is empty")
	}

	// Wait a bit for refill
	time.Sleep(150 * time.Millisecond)

	// Now TryWait should succeed again
	if !tb.TryWait() {
		t.Errorf("TryWait should succeed after refill")
	}
}

func TestBurstCapacity(t *testing.T) {
	tb := NewTokenBucket(1.0, 10) // 1 tps, burst 10
	ctx := context.Background()

	// Should be able to consume all burst tokens immediately
	start := time.Now()
	for i := 0; i < 10; i++ {
		if err := tb.Wait(ctx); err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
	}
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("Consuming burst tokens should be immediate, took %v", elapsed)
	}

	// Next request should wait
	start = time.Now()
	err := tb.Wait(ctx)
	elapsed = time.Since(start)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// With rate 1 tps, should wait ~1s after burst exhausted
	// The test range is wider to accommodate implementation differences
	if elapsed < 900*time.Millisecond || elapsed > 1200*time.Millisecond {
		t.Errorf("Expected wait ~1s after burst exhausted, got %v", elapsed)
	}
}

func TestConcurrentAccess(t *testing.T) {
	tb := NewTokenBucket(50.0, 10) // 50 tps, burst 10
	ctx := context.Background()

	const numGoroutines = 20
	const requestsPerGoroutine = 5

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	startTime := time.Now()
	errors := make(chan error, numGoroutines*requestsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				if err := tb.Wait(ctx); err != nil {
					errors <- err
				}
			}
		}()
	}

	wg.Wait()
	close(errors)
	elapsed := time.Since(startTime)

	// Count errors
	errorCount := 0
	for err := range errors {
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
			errorCount++
		}
	}

	if errorCount > 0 {
		t.Errorf("Unexpected errors: %d", errorCount)
	}

	// With 50 tps, 100 requests should take ~2 seconds
	// The first 10 (burst) are immediate, the remaining 90 take ~1.8s at 50 tps
	// Allow some margin for goroutine overhead
	expectedMin := 1.6 * float64(time.Second)
	expectedMax := 2.5 * float64(time.Second)

	if float64(elapsed) < expectedMin || float64(elapsed) > expectedMax {
		t.Errorf("Expected duration ~2s for 100 requests at 50 tps, got %v", elapsed)
	}
}

func TestContextCancellation(t *testing.T) {
	tb := NewTokenBucket(1.0, 1) // 1 tps, burst 1
	ctx := context.Background()

	// Consume burst token
	if err := tb.Wait(ctx); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Create cancelable context with short timeout
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// Wait should be canceled
	err := tb.Wait(ctx)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got %v", err)
	}

	// With the new reservation-based algorithm, canceling doesn't return a token
	// because we only reserved a slot, we didn't consume one yet.
	// The bucket state should remain as is (empty or near empty).
	// After ~50ms, we should have ~0.05 tokens from refill.
	available := tb.GetAvailableTokens()
	if available < 0.04 {
		t.Errorf("Expected ~0.05 tokens after 50ms at 1 tps, got %f", available)
	}
}

func TestUpdateRate(t *testing.T) {
	tb := NewTokenBucket(1.0, 5)
	ctx := context.Background()

	// Consume all burst tokens
	for i := 0; i < 5; i++ {
		tb.Wait(ctx)
	}

	// Next request should wait with rate 1.0
	start := time.Now()
	tb.Wait(ctx)
	wait1 := time.Since(start)

	// With rate 1.0, should wait ~1s
	if wait1 < 900*time.Millisecond {
		t.Errorf("Expected wait ~1s at rate 1.0, got %v", wait1)
	}

	// Consume the refilled token (may need to wait again if there's a queue)
	tb.Wait(ctx)

	// Update rate to 10.0 tps
	tb.UpdateRate(10.0, 5)

	// Consume any remaining tokens
	for tb.GetAvailableTokens() >= 1 {
		tb.Wait(ctx)
	}

	// Now drain again by consuming one more to start the queue
	tb.Wait(ctx)

	// Next request should be faster (10x rate)
	start = time.Now()
	tb.Wait(ctx)
	wait2 := time.Since(start)

	// With rate 10.0, should wait ~100ms
	if wait2 < 80*time.Millisecond || wait2 > 300*time.Millisecond {
		t.Errorf("Expected wait ~100ms at rate 10.0, got %v", wait2)
	}
}

func TestUpdateBurst(t *testing.T) {
	tb := NewTokenBucket(10.0, 3)
	ctx := context.Background()

	// Consume burst
	for i := 0; i < 3; i++ {
		tb.Wait(ctx)
	}

	// Should wait for next token
	tb.Wait(ctx)

	// Update burst to 10
	tb.UpdateRate(10.0, 10)

	// Wait a bit for refill
	time.Sleep(150 * time.Millisecond)

	// Should have ~1.5 tokens, but burst is 10
	available := tb.GetAvailableTokens()
	if available > 10 {
		t.Errorf("Available tokens (%f) should not exceed burst (10)", available)
	}

	// Try to consume burst (should get ~1-2 tokens immediately)
	start := time.Now()
	consumed := 0
	for i := 0; i < 10; i++ {
		if tb.TryWait() {
			consumed++
		} else {
			break
		}
	}
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Errorf("Consuming available tokens should be immediate, took %v", elapsed)
	}
	if consumed == 0 {
		t.Errorf("Should have been able to consume some tokens after refill")
	}
}

func TestUnlimitedAccess(t *testing.T) {
	tb := NewTokenBucket(-1.0, -1) // Invalid config = unlimited
	ctx := context.Background()

	// Should be able to make many requests immediately
	start := time.Now()
	for i := 0; i < 100; i++ {
		if err := tb.Wait(ctx); err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
	}
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("Unlimited access should not wait, took %v", elapsed)
	}

	// TryWait should always succeed
	for i := 0; i < 100; i++ {
		if !tb.TryWait() {
			t.Errorf("TryWait should always succeed with unlimited access")
		}
	}
}

func TestZeroRate(t *testing.T) {
	tb := NewTokenBucket(0.0, 10) // Zero rate = unlimited
	ctx := context.Background()

	// Should behave as unlimited
	for i := 0; i < 10; i++ {
		if err := tb.Wait(ctx); err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !tb.TryWait() {
			t.Errorf("TryWait should succeed with zero rate")
		}
	}
}

func TestTokenRefill(t *testing.T) {
	tb := NewTokenBucket(10.0, 5)
	ctx := context.Background()

	// Consume all tokens
	for i := 0; i < 5; i++ {
		tb.Wait(ctx)
	}

	// Should be empty
	if tb.GetAvailableTokens() > 0.1 {
		t.Errorf("Expected bucket to be empty, got %f tokens", tb.GetAvailableTokens())
	}

	// Wait for refill (100ms should give 1 token at 10 tps)
	time.Sleep(100 * time.Millisecond)

	// Should have ~1 token now
	available := tb.GetAvailableTokens()
	if available < 0.8 || available > 1.2 {
		t.Errorf("Expected ~1 token after 100ms at 10 tps, got %f", available)
	}
}

func TestDrainAndWait(t *testing.T) {
	tb := NewTokenBucket(5.0, 3) // 5 tps, burst 3
	ctx := context.Background()

	// Drain burst
	for i := 0; i < 3; i++ {
		tb.Wait(ctx)
	}

	// Should wait for refill and then succeed
	start := time.Now()
	err := tb.Wait(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// With 5 tps, should wait ~200ms for 1 token
	if elapsed < 150*time.Millisecond || elapsed > 300*time.Millisecond {
		t.Errorf("Expected wait ~200ms, got %v", elapsed)
	}
}

func TestGetAvailableTokens(t *testing.T) {
	tb := NewTokenBucket(10.0, 10)
	ctx := context.Background()

	// Start with full burst
	available := tb.GetAvailableTokens()
	if available != 10.0 {
		t.Errorf("Expected 10 tokens, got %f", available)
	}

	// Consume 3 tokens
	tb.Wait(ctx)
	tb.Wait(ctx)
	tb.Wait(ctx)

	// Should have 7 left
	available = tb.GetAvailableTokens()
	if available < 6.9 || available > 7.1 {
		t.Errorf("Expected ~7 tokens, got %f", available)
	}
}

func BenchmarkTokenBucketWait(b *testing.B) {
	tb := NewTokenBucket(1000.0, 100) // High rate for benchmark
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tb.Wait(ctx)
	}
}

func BenchmarkTokenBucketTryWait(b *testing.B) {
	tb := NewTokenBucket(1000.0, 100) // High rate for benchmark

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tb.TryWait()
	}
}
