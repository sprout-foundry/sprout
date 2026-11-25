package core

import (
	"testing"
	"time"
)

// =============================================================================
// FakeRand is a deterministic randSource for jitter testing.
// =============================================================================

type FakeRand struct {
	Values []float64
	Index  int
}

func (f *FakeRand) Float64() float64 {
	if f.Index >= len(f.Values) {
		f.Index = 0
	}
	v := f.Values[f.Index]
	f.Index++
	return v
}

// =============================================================================
// Default Values
// =============================================================================

func TestBackoff_DefaultValues(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 3, 0.0)

	if b.InitialDelay != 100*time.Millisecond {
		t.Errorf("InitialDelay = %v, want 100ms", b.InitialDelay)
	}
	if b.MaxDelay != 5*time.Second {
		t.Errorf("MaxDelay = %v, want 5s", b.MaxDelay)
	}
	if b.Multiplier != 2.0 {
		t.Errorf("Multiplier = %v, want 2.0", b.Multiplier)
	}
	if b.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %v, want 3", b.MaxAttempts)
	}
	if b.Jitter != 0.0 {
		t.Errorf("Jitter = %v, want 0.0", b.Jitter)
	}
	if b.attempt != 0 {
		t.Errorf("attempt = %d, want 0", b.attempt)
	}
	if b.currentDelay != 0 {
		t.Errorf("currentDelay = %v, want 0", b.currentDelay)
	}
}

// =============================================================================
// NextAttempt returning true/false correctly
// =============================================================================

func TestBackoff_NextAttempt_ReturnsTrueWithinLimit(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 3, 0.0)

	// All 3 attempts should return true
	for i := 1; i <= 3; i++ {
		if !b.NextAttempt() {
			t.Errorf("NextAttempt() call %d = false, want true", i)
		}
	}
}

func TestBackoff_NextAttempt_ReturnsFalseAfterExhaustion(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 3, 0.0)

	// First 3 return true
	for i := 1; i <= 3; i++ {
		if !b.NextAttempt() {
			t.Errorf("NextAttempt() call %d = false, want true", i)
		}
	}

	// 4th should return false
	if b.NextAttempt() {
		t.Error("NextAttempt() call 4 = true, want false")
	}

	// 5th should also return false
	if b.NextAttempt() {
		t.Error("NextAttempt() call 5 = true, want false")
	}
}

func TestBackoff_NextAttempt_ZeroMaxAttemptsMeansUnlimited(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 0, 0.0)

	// MaxAttempts = 0 means unlimited retries
	for i := 1; i <= 10; i++ {
		if !b.NextAttempt() {
			t.Errorf("NextAttempt() call %d = false, want true (unlimited)", i)
		}
	}
}

func TestBackoff_NextAttempt_OneAttempt(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 1, 0.0)

	if !b.NextAttempt() {
		t.Error("First NextAttempt() should return true for MaxAttempts=1")
	}

	if b.NextAttempt() {
		t.Error("Second NextAttempt() should return false")
	}
}

// =============================================================================
// Delay increasing exponentially
// =============================================================================

func TestBackoff_DelayExponentialGrowth(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 10*time.Second, 2.0, 10, 0.0)

	// Attempt 1: delay = InitialDelay = 100ms
	if !b.NextAttempt() {
		t.Fatal("NextAttempt(1) should be true")
	}
	d := b.Delay()
	if d != 100*time.Millisecond {
		t.Errorf("Attempt 1 delay = %v, want 100ms", d)
	}

	// Attempt 2: delay = 100ms * 2 = 200ms
	if !b.NextAttempt() {
		t.Fatal("NextAttempt(2) should be true")
	}
	d = b.Delay()
	if d != 200*time.Millisecond {
		t.Errorf("Attempt 2 delay = %v, want 200ms", d)
	}

	// Attempt 3: delay = 200ms * 2 = 400ms
	if !b.NextAttempt() {
		t.Fatal("NextAttempt(3) should be true")
	}
	d = b.Delay()
	if d != 400*time.Millisecond {
		t.Errorf("Attempt 3 delay = %v, want 400ms", d)
	}

	// Attempt 4: delay = 400ms * 2 = 800ms
	if !b.NextAttempt() {
		t.Fatal("NextAttempt(4) should be true")
	}
	d = b.Delay()
	if d != 800*time.Millisecond {
		t.Errorf("Attempt 4 delay = %v, want 800ms", d)
	}
}

func TestBackoff_DelayWithMultiplier3(t *testing.T) {
	b := NewExponentialBackoff(10*time.Millisecond, 100*time.Second, 3.0, 10, 0.0)

	if !b.NextAttempt() {
		t.Fatal("NextAttempt(1) should be true")
	}
	if d := b.Delay(); d != 10*time.Millisecond {
		t.Errorf("Attempt 1 = %v, want 10ms", d)
	}

	if !b.NextAttempt() {
		t.Fatal("NextAttempt(2) should be true")
	}
	if d := b.Delay(); d != 30*time.Millisecond {
		t.Errorf("Attempt 2 = %v, want 30ms", d)
	}

	if !b.NextAttempt() {
		t.Fatal("NextAttempt(3) should be true")
	}
	if d := b.Delay(); d != 90*time.Millisecond {
		t.Errorf("Attempt 3 = %v, want 90ms", d)
	}

	if !b.NextAttempt() {
		t.Fatal("NextAttempt(4) should be true")
	}
	if d := b.Delay(); d != 270*time.Millisecond {
		t.Errorf("Attempt 4 = %v, want 270ms", d)
	}
}

func TestBackoff_DelayWithMultiplier1_5(t *testing.T) {
	b := NewExponentialBackoff(10*time.Millisecond, 100*time.Second, 1.5, 10, 0.0)

	if !b.NextAttempt() {
		t.Fatal("NextAttempt(1) should be true")
	}
	if d := b.Delay(); d != 10*time.Millisecond {
		t.Errorf("Attempt 1 = %v, want 10ms", d)
	}

	if !b.NextAttempt() {
		t.Fatal("NextAttempt(2) should be true")
	}
	if d := b.Delay(); d != 15*time.Millisecond {
		t.Errorf("Attempt 2 = %v, want 15ms", d)
	}

	if !b.NextAttempt() {
		t.Fatal("NextAttempt(3) should be true")
	}
	// 10ms * 1.5 = 15ms, then 15ms * 1.5 = 22.5ms (22500000ns, exact integer)
	if d := b.Delay(); d != 22*time.Millisecond+500*time.Microsecond {
		t.Errorf("Attempt 3 = %v, want 22.5ms", d)
	}
}

func TestBackoff_DelayWithMultiplier0_5(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 100*time.Second, 0.5, 10, 0.0)

	if !b.NextAttempt() {
		t.Fatal("NextAttempt(1) should be true")
	}
	if d := b.Delay(); d != 100*time.Millisecond {
		t.Errorf("Attempt 1 = %v, want 100ms", d)
	}

	if !b.NextAttempt() {
		t.Fatal("NextAttempt(2) should be true")
	}
	// 100ms * 0.5 = 50ms
	if d := b.Delay(); d != 50*time.Millisecond {
		t.Errorf("Attempt 2 = %v, want 50ms", d)
	}

	if !b.NextAttempt() {
		t.Fatal("NextAttempt(3) should be true")
	}
	// 50ms * 0.5 = 25ms
	if d := b.Delay(); d != 25*time.Millisecond {
		t.Errorf("Attempt 3 = %v, want 25ms", d)
	}
}

// =============================================================================
// Delay with constant multiplier (Multiplier = 1.0)
// =============================================================================

func TestBackoff_DelayWithConstantMultiplier(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 10*time.Second, 1.0, 5, 0.0)

	for i := 1; i <= 5; i++ {
		if !b.NextAttempt() {
			t.Fatalf("NextAttempt(%d) should be true", i)
		}
		if d := b.Delay(); d != 100*time.Millisecond {
			t.Errorf("Attempt %d = %v, want 100ms (constant)", i, d)
		}
	}
}

// =============================================================================
// MaxDelay capping
// =============================================================================

func TestBackoff_DelayMaxDelayCapping(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 300*time.Millisecond, 2.0, 10, 0.0)

	// Attempt 1: 100ms (no cap yet)
	if !b.NextAttempt() {
		t.Fatal("NextAttempt(1) should be true")
	}
	if d := b.Delay(); d != 100*time.Millisecond {
		t.Errorf("Attempt 1 = %v, want 100ms", d)
	}

	// Attempt 2: 200ms (no cap yet)
	if !b.NextAttempt() {
		t.Fatal("NextAttempt(2) should be true")
	}
	if d := b.Delay(); d != 200*time.Millisecond {
		t.Errorf("Attempt 2 = %v, want 200ms", d)
	}

	// Attempt 3: min(400ms, 300ms) = 300ms (capped)
	if !b.NextAttempt() {
		t.Fatal("NextAttempt(3) should be true")
	}
	if d := b.Delay(); d != 300*time.Millisecond {
		t.Errorf("Attempt 3 = %v, want 300ms (capped)", d)
	}

	// Attempt 4: stays at 300ms (capped)
	if !b.NextAttempt() {
		t.Fatal("NextAttempt(4) should be true")
	}
	if d := b.Delay(); d != 300*time.Millisecond {
		t.Errorf("Attempt 4 = %v, want 300ms (still capped)", d)
	}

	// Attempt 5: still capped
	if !b.NextAttempt() {
		t.Fatal("NextAttempt(5) should be true")
	}
	if d := b.Delay(); d != 300*time.Millisecond {
		t.Errorf("Attempt 5 = %v, want 300ms (still capped)", d)
	}
}

func TestBackoff_DelayMaxDelayReachedEarly(t *testing.T) {
	// InitialDelay=10ms, Multiplier=2, MaxDelay=100ms
	// Delays grow: 10, 20, 40, 80, 160(capped), 160, ...
	b := NewExponentialBackoff(10*time.Millisecond, 100*time.Millisecond, 2.0, 20, 0.0)

	// Call NextAttempt until delay reaches MaxDelay
	for b.NextAttempt() {
		d := b.Delay()
		if d >= 100*time.Millisecond {
			break
		}
	}

	// Now verify subsequent attempts stay capped
	for i := 0; i < 5; i++ {
		if !b.NextAttempt() {
			t.Fatalf("Attempt %d should be true", i)
		}
		if d := b.Delay(); d != 100*time.Millisecond {
			t.Errorf("Capped delay attempt %d = %v, want 100ms", i, d)
		}
	}
}

func TestBackoff_DelayNoMaxDelay(t *testing.T) {
	b := NewExponentialBackoff(1, 0, 2.0, 30, 0.0)

	if !b.NextAttempt() {
		t.Fatal("NextAttempt(1) should be true")
	}
	if d := b.Delay(); d != 1 {
		t.Errorf("Attempt 1 = %v, want 1", d)
	}

	// Keep growing exponentially since MaxDelay = 0
	for i := 0; i < 5; i++ {
		if !b.NextAttempt() {
			break
		}
		// Just verify it keeps growing without panicking
		d := b.Delay()
		if d <= 0 {
			t.Errorf("Attempt %d: delay = %v (should grow)", i, d)
		}
	}
}

// =============================================================================
// Jitter adding randomness within bounds
// =============================================================================

func TestBackoff_NoJitterProducesDeterministicDelay(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 10, 0.0)

	for i := 0; i < 10; i++ {
		b.Reset()
		if !b.NextAttempt() {
			t.Fatal("NextAttempt should be true")
		}
		d := b.Delay()
		if d != 100*time.Millisecond {
			t.Errorf("No jitter: attempt %d delay = %v, want 100ms", i, d)
		}
	}
}

func TestBackoff_JitterAddsRandomnessWithinBounds(t *testing.T) {
	// Jitter = 0.5 means add up to 50% of base delay
	// Formula: delay = base + randValue * 0.5 * base
	tests := []struct {
		name      string
		randValue float64
		wantDelay time.Duration
	}{
		{"rand 0.0 → no jitter", 0.0, 100 * time.Millisecond},
		{"rand 1.0 → max jitter 50%", 1.0, 150 * time.Millisecond},
		{"rand 0.5 → 25% jitter", 0.5, 125 * time.Millisecond},
		{"rand 0.1 → 5% jitter", 0.1, 105 * time.Millisecond},
		{"rand 0.99 → 49.5% jitter", 0.99, 149*time.Millisecond + 500*time.Microsecond},
		{"rand 0.01 → 0.5% jitter", 0.01, 100*time.Millisecond + 500*time.Microsecond},
		{"bounds check 0%", 0.0, 100 * time.Millisecond},
		{"bounds check 100%", 1.0, 150 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 10, 0.5).
				WithRand(&FakeRand{Values: []float64{tt.randValue}})

			if !b.NextAttempt() {
				t.Fatal("NextAttempt should be true")
			}
			d := b.Delay()
			if d != tt.wantDelay {
				t.Errorf("delay = %v, want %v", d, tt.wantDelay)
			}
		})
	}
}

func TestBackoff_JitterFullJitterMode(t *testing.T) {
	// Jitter >= 1.0 means full jitter: delay becomes random [0, baseDelay]
	tests := []struct {
		name      string
		randValue float64
		baseDelay time.Duration
		wantMin   time.Duration
		wantMax   time.Duration
	}{
		{"full jitter 0%", 0.0, 100 * time.Millisecond, 0, 0},
		{"full jitter 100%", 1.0, 100 * time.Millisecond, 100 * time.Millisecond, 100 * time.Millisecond},
		{"full jitter 50%", 0.5, 100 * time.Millisecond, 50 * time.Millisecond, 50 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 10, 1.5).
				WithRand(&FakeRand{Values: []float64{tt.randValue}})

			if !b.NextAttempt() {
				t.Fatal("NextAttempt should be true")
			}
			d := b.Delay()
			if d < tt.wantMin || d > tt.wantMax {
				t.Errorf("delay = %v, want [%v, %v]", d, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestBackoff_JitterBoundsMultipleTrials(t *testing.T) {
	// Verify jitter bounds with many random values
	fake := &FakeRand{
		Values: make([]float64, 200),
		Index:  0,
	}
	for i := range fake.Values {
		fake.Values[i] = float64(i%100) / 100.0 // 0.00, 0.01, ..., 0.99
	}

	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 10, 0.5).
		WithRand(fake)

	for i := 0; i < 200; i++ {
		if !b.NextAttempt() {
			break
		}
		d := b.Delay()

		// Jitter = 0.5, so delay in [base, base + 0.5*base]
		// base grows exponentially: InitialDelay * Multiplier^(attempt-1), capped at MaxDelay
		base := float64(b.InitialDelay)
		for j := 1; j < b.attempt; j++ {
			base *= b.Multiplier
			if base > float64(b.MaxDelay) && b.MaxDelay > 0 {
				base = float64(b.MaxDelay)
				break
			}
		}
		wantMin := time.Duration(base)
		wantMax := time.Duration(float64(b.MaxDelay) * 1.5)
		if wantMax < wantMin {
			wantMax = wantMin
		}

		if d < wantMin || d > wantMax {
			t.Errorf("Trial %d (attempt %d): delay = %v, want [%v, %v]",
				i, b.attempt, d, wantMin, wantMax)
		}
	}
}

func TestBackoff_JitterWithDifferentBaseDelays(t *testing.T) {
	b := NewExponentialBackoff(50*time.Millisecond, 5*time.Second, 2.0, 5, 0.25).
		WithRand(&FakeRand{Values: []float64{0.0, 1.0}})

	// Attempt 1: base = 50ms
	if !b.NextAttempt() {
		t.Fatal("NextAttempt(1) should be true")
	}
	d := b.Delay()
	if d != 50*time.Millisecond {
		t.Errorf("Attempt 1: delay = %v, want 50ms (jitter 0%%)", d)
	}

	// Attempt 2: base = 100ms
	if !b.NextAttempt() {
		t.Fatal("NextAttempt(2) should be true")
	}
	d = b.Delay()
	// Jitter = 1.0 → max possible: 100ms + 25% = 125ms
	if d != 125*time.Millisecond {
		t.Errorf("Attempt 2: delay = %v, want 125ms (jitter 100%%)", d)
	}
}

func TestBackoff_JitterZeroVsPositive(t *testing.T) {
	// Two backoffs with same params, different jitter
	bNoJitter := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 10, 0.0)
	bWithJitter := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 10, 0.5).
		WithRand(&FakeRand{Values: []float64{0.5}})

	if !bNoJitter.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	if !bWithJitter.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}

	d1 := bNoJitter.Delay()
	d2 := bWithJitter.Delay()

	// No jitter should always be exactly the base delay
	if d1 != 100*time.Millisecond {
		t.Errorf("No jitter delay = %v, want 100ms", d1)
	}

	// With jitter, delay should be > base (since 0.5 * 100ms * 0.5 = 25ms)
	if d2 <= 100*time.Millisecond {
		t.Errorf("With jitter delay = %v, expected > 100ms", d2)
	}
}

// =============================================================================
// MaxAttempts limiting retries
// =============================================================================

func TestBackoff_MaxAttemptsLimitsRetries(t *testing.T) {
	tests := []struct {
		name        string
		maxAttempts int
		wantCount   int
	}{
		{"0 unlimited", 0, 10},
		{"1 retry", 1, 1},
		{"3 retries", 3, 3},
		{"5 retries", 5, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, tt.maxAttempts, 0.0)

			count := 0
			for b.NextAttempt() {
				count++
				if tt.maxAttempts == 0 && count >= 10 {
					break // safety limit for unlimited
				}
			}

			if tt.maxAttempts == 0 {
				if count >= 10 {
					// Ran at least 10 — that's enough for "unlimited"
				} else {
					t.Errorf("unlimited: count = %d, want >= 10", count)
				}
			} else if count != tt.wantCount {
				t.Errorf("count = %d, want %d", count, tt.wantCount)
			}
		})
	}
}

func TestBackoff_MaxAttemptsExactLimit(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 5, 0.0)

	attemptNum := 0
	for b.NextAttempt() {
		attemptNum++
		// Verify we can call Delay() during each attempt
		_ = b.Delay()
	}

	if attemptNum != 5 {
		t.Errorf("Total attempts = %d, want 5", attemptNum)
	}

	// Verify NextAttempt now returns false
	if b.NextAttempt() {
		t.Error("After exhausting MaxAttempts, NextAttempt() should return false")
	}
}

func TestBackoff_MaxAttemptsCombinedWithMaxDelay(t *testing.T) {
	// MaxAttempts = 3, MaxDelay = 200ms, InitialDelay = 100ms, Multiplier = 2.0
	// Delays: 100ms, 200ms, 200ms (capped)
	b := NewExponentialBackoff(100*time.Millisecond, 200*time.Millisecond, 2.0, 3, 0.0)

	expectedDelays := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 200 * time.Millisecond}

	for i, expected := range expectedDelays {
		if !b.NextAttempt() {
			t.Errorf("NextAttempt(%d) should be true", i+1)
		}
		d := b.Delay()
		if d != expected {
			t.Errorf("Attempt %d delay = %v, want %v", i+1, d, expected)
		}
	}

	// Should be exhausted
	if b.NextAttempt() {
		t.Error("Should be exhausted after MaxAttempts=3")
	}
}

// =============================================================================
// Reset functionality
// =============================================================================

func TestBackoff_Reset(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 3, 0.0)

	b.NextAttempt()
	b.NextAttempt()
	if b.attempt != 2 {
		t.Errorf("attempt = %d, want 2", b.attempt)
	}
	if b.currentDelay != 200*time.Millisecond {
		t.Errorf("currentDelay = %v, want 200ms", b.currentDelay)
	}

	b.Reset()

	if b.attempt != 0 {
		t.Errorf("After Reset: attempt = %d, want 0", b.attempt)
	}
	if b.currentDelay != 0 {
		t.Errorf("After Reset: currentDelay = %v, want 0", b.currentDelay)
	}
}

func TestBackoff_ResetAndReuse(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 3, 0.0)

	// First sequence
	if !b.NextAttempt() {
		t.Fatal("NextAttempt(1) should be true")
	}
	if d := b.Delay(); d != 100*time.Millisecond {
		t.Errorf("First sequence attempt 1 = %v, want 100ms", d)
	}

	b.Reset()

	// Second sequence should be identical
	if !b.NextAttempt() {
		t.Fatal("NextAttempt after Reset should be true")
	}
	if d := b.Delay(); d != 100*time.Millisecond {
		t.Errorf("Second sequence attempt 1 = %v, want 100ms", d)
	}
}

func TestBackoff_ResetAfterExhaustion(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 2, 0.0)

	// Exhaust all retries
	b.NextAttempt() // 1
	b.NextAttempt() // 2
	b.NextAttempt() // 3 (returns false, but advances counter)

	// Reset should allow starting fresh
	b.Reset()
	if !b.NextAttempt() {
		t.Error("After Reset from exhaustion, NextAttempt() should return true")
	}
	if d := b.Delay(); d != 100*time.Millisecond {
		t.Errorf("After Reset: delay = %v, want 100ms", d)
	}
}

func TestBackoff_MultipleResetCycles(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 1000*time.Millisecond, 2.0, 3, 0.0)

	for cycle := 0; cycle < 5; cycle++ {
		b.Reset()

		// Attempt 1
		if !b.NextAttempt() {
			t.Errorf("Cycle %d: attempt 1 should be true", cycle)
		}
		if d := b.Delay(); d != 100*time.Millisecond {
			t.Errorf("Cycle %d: attempt 1 delay = %v, want 100ms", cycle, d)
		}

		// Attempt 2
		if !b.NextAttempt() {
			t.Errorf("Cycle %d: attempt 2 should be true", cycle)
		}
		if d := b.Delay(); d != 200*time.Millisecond {
			t.Errorf("Cycle %d: attempt 2 delay = %v, want 200ms", cycle, d)
		}

		// Attempt 3
		if !b.NextAttempt() {
			t.Errorf("Cycle %d: attempt 3 should be true", cycle)
		}
		if d := b.Delay(); d != 400*time.Millisecond {
			t.Errorf("Cycle %d: attempt 3 delay = %v, want 400ms", cycle, d)
		}

		// Attempt 4 (should fail)
		if b.NextAttempt() {
			t.Errorf("Cycle %d: attempt 4 should be false", cycle)
		}
	}
}

func TestBackoff_ResetBeforeFirstAttempt(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 3, 0.0)

	// Resetting before any attempts is a no-op but shouldn't panic
	b.Reset()
	if b.attempt != 0 {
		t.Errorf("After pre-first Reset: attempt = %d, want 0", b.attempt)
	}

	// Should still work normally
	if !b.NextAttempt() {
		t.Error("After pre-Reset, NextAttempt() should return true")
	}
	if d := b.Delay(); d != 100*time.Millisecond {
		t.Errorf("Delay = %v, want 100ms", d)
	}
}

// =============================================================================
// Edge Cases: zero/negative values
// =============================================================================

func TestBackoff_ZeroInitialDelay(t *testing.T) {
	b := NewExponentialBackoff(0, 5*time.Second, 2.0, 3, 0.0)

	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	// 0 * 2.0 = 0, so delay stays at 0
	d := b.Delay()
	if d != 0 {
		t.Errorf("Zero InitialDelay: delay = %v, want 0", d)
	}
}

func TestBackoff_NegativeInitialDelay(t *testing.T) {
	// Negative durations are valid in Go but produce negative delays
	b := NewExponentialBackoff(-100*time.Millisecond, 5*time.Second, 2.0, 3, 0.0)

	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	d := b.Delay()
	// Negative initial delay → negative delay (Go allows this)
	if d >= 0 {
		t.Errorf("Negative InitialDelay: delay = %v, want negative", d)
	}
}

func TestBackoff_ZeroMaxDelay(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 0, 2.0, 5, 0.0)

	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	if d := b.Delay(); d != 100*time.Millisecond {
		t.Errorf("Attempt 1 = %v, want 100ms", d)
	}

	// MaxDelay = 0 means no cap
	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	// 100 * 2 = 200ms, MaxDelay = 0 means no capping
	if d := b.Delay(); d != 200*time.Millisecond {
		t.Errorf("Attempt 2 = %v, want 200ms (no cap)", d)
	}
}

func TestBackoff_NegativeMaxDelay(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, -5*time.Second, 2.0, 5, 0.0)

	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	// Negative MaxDelay won't cap anything since currentDelay > MaxDelay
	// (positive > negative), so it should just keep growing
	if d := b.Delay(); d != 100*time.Millisecond {
		t.Errorf("Attempt 1 = %v, want 100ms", d)
	}
}

func TestBackoff_ZeroMultiplier(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 0.0, 5, 0.0)

	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	if d := b.Delay(); d != 100*time.Millisecond {
		t.Errorf("Attempt 1 = %v, want 100ms", d)
	}

	// Attempt 2: 100ms * 0.0 = 0
	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	if d := b.Delay(); d != 0 {
		t.Errorf("Attempt 2 = %v, want 0", d)
	}

	// Attempt 3: 0 * 0.0 = 0
	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	if d := b.Delay(); d != 0 {
		t.Errorf("Attempt 3 = %v, want 0", d)
	}
}

func TestBackoff_NegativeMultiplier(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, -1.0, 5, 0.0)

	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	if d := b.Delay(); d != 100*time.Millisecond {
		t.Errorf("Attempt 1 = %v, want 100ms", d)
	}

	// Attempt 2: 100ms * -1 = -100ms
	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	if d := b.Delay(); d != -100*time.Millisecond {
		t.Errorf("Attempt 2 = %v, want -100ms", d)
	}
}

func TestBackoff_ZeroMaxAttemptsWithZeroValue(t *testing.T) {
	// MaxAttempts = 0 is treated as unlimited, not zero retries
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 0, 0.0)

	count := 0
	for b.NextAttempt() {
		count++
		if count >= 100 {
			break // safety limit
		}
	}
	if count < 100 {
		t.Errorf("MaxAttempts=0 should be unlimited, got %d attempts", count)
	}
}

func TestBackoff_SmallInitialDelay(t *testing.T) {
	b := NewExponentialBackoff(1, 5*time.Second, 2.0, 5, 0.0)

	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	if d := b.Delay(); d != 1 {
		t.Errorf("Attempt 1 = %v, want 1", d)
	}

	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	if d := b.Delay(); d != 2 {
		t.Errorf("Attempt 2 = %v, want 2", d)
	}
}

func TestBackoff_VeryLargeMultiplier(t *testing.T) {
	b := NewExponentialBackoff(1, 1000, 100.0, 5, 0.0)

	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	if d := b.Delay(); d != 1 {
		t.Errorf("Attempt 1 = %v, want 1", d)
	}

	// Attempt 2: 1 * 100 = 100
	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	if d := b.Delay(); d != 100 {
		t.Errorf("Attempt 2 = %v, want 100", d)
	}

	// Attempt 3: 100 * 100 = 10000, capped to 1000
	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	if d := b.Delay(); d != 1000 {
		t.Errorf("Attempt 3 = %v, want 1000 (capped)", d)
	}
}

func TestBackoff_NegativeAttemptsShouldNotPanic(t *testing.T) {
	// MaxAttempts = -1: the check is `b.MaxAttempts > 0 && b.attempt > b.MaxAttempts`
	// Since -1 > 0 is false, it acts like unlimited (same as 0).
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, -1, 0.0)

	// Negative MaxAttempts → unlimited (same as 0)
	count := 0
	for b.NextAttempt() {
		count++
		if count >= 10 {
			break
		}
	}
	if count < 10 {
		t.Errorf("Negative MaxAttempts: unlimited behavior expected, got %d attempts", count)
	}
}

// =============================================================================
// Jitter = 1.0 (full jitter boundary)
// =============================================================================

func TestBackoff_JitterExactlyOne(t *testing.T) {
	// Jitter = 1.0 triggers full jitter mode [0, delay]
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 5, 1.0).
		WithRand(&FakeRand{Values: []float64{0.5}})

	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	d := b.Delay()
	// Full jitter: 100ms * 0.5 = 50ms
	if d != 50*time.Millisecond {
		t.Errorf("Full jitter 50%%: delay = %v, want 50ms", d)
	}
}

func TestBackoff_JitterAboveOne(t *testing.T) {
	// Jitter > 1.0 also triggers full jitter mode
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 5, 2.0).
		WithRand(&FakeRand{Values: []float64{0.75}})

	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	d := b.Delay()
	// Full jitter: 100ms * 0.75 = 75ms
	if d != 75*time.Millisecond {
		t.Errorf("Full jitter >1.0: delay = %v, want 75ms", d)
	}
}

func TestBackoff_JitterJustBelowOne(t *testing.T) {
	// Jitter = 0.999 → partial jitter mode: delay + rand(0, 0.999*delay)
	// 100ms + 1.0 * 0.999 * 100ms = 100ms + 99.9ms = 199.9ms
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 5, 0.999).
		WithRand(&FakeRand{Values: []float64{1.0}}) // max value

	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	d := b.Delay()
	if d != 100*time.Millisecond+99*time.Millisecond+900*time.Microsecond {
		t.Errorf("Jitter 0.999 with max rand: delay = %v, want 199.9ms", d)
	}
}

// =============================================================================
// RandSource abstraction
// =============================================================================

func TestBackoff_WithRandCustomSource(t *testing.T) {
	// Verify WithRand properly overrides the default rand source
	custom := &FakeRand{Values: []float64{0.5}}
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 3, 0.5).
		WithRand(custom)

	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}
	// Formula: 100ms + 0.5 * 0.5 * 100ms = 100 + 25 = 125ms
	d := b.Delay()
	if d != 125*time.Millisecond {
		t.Errorf("Custom rand source: delay = %v, want 125ms", d)
	}
}

func TestBackoff_NilRandSourcePanics(t *testing.T) {
	// If randSrc is nil, Delay should panic with a clear message
	b := &ExponentialBackoff{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     5 * time.Second,
		Multiplier:   2.0,
		MaxAttempts:  3,
		Jitter:       0.5,
		randSrc:      nil, // explicitly nil
	}

	if !b.NextAttempt() {
		t.Fatal("NextAttempt should be true")
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("Delay with nil randSrc should panic")
		}
	}()
	b.Delay()
}

// =============================================================================
// Full retry sequence
// =============================================================================

func TestBackoff_FullRetrySequence(t *testing.T) {
	b := NewExponentialBackoff(50*time.Millisecond, 500*time.Millisecond, 2.0, 3, 0.0)

	expectedDelays := []time.Duration{
		50 * time.Millisecond,  // attempt 1: 50 * 2^0
		100 * time.Millisecond, // attempt 2: 50 * 2^1
		200 * time.Millisecond, // attempt 3: 50 * 2^2
	}

	for i, expected := range expectedDelays {
		if !b.NextAttempt() {
			t.Errorf("NextAttempt(%d) should be true", i+1)
		}
		d := b.Delay()
		if d != expected {
			t.Errorf("Attempt %d delay = %v, want %v", i+1, d, expected)
		}
	}

	// Next should return false
	if b.NextAttempt() {
		t.Error("After MaxAttempts, NextAttempt() should return false")
	}
}

func TestBackoff_FullRetryWithJitter(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 500*time.Millisecond, 2.0, 3, 0.25).
		WithRand(&FakeRand{Values: []float64{0.0, 1.0, 0.5}})

	// Attempt 1: 100ms + 0% = 100ms
	if !b.NextAttempt() {
		t.Fatal("NextAttempt(1) should be true")
	}
	d := b.Delay()
	if d != 100*time.Millisecond {
		t.Errorf("Attempt 1 = %v, want 100ms", d)
	}

	// Attempt 2: 200ms + 100% of 25% = 250ms
	if !b.NextAttempt() {
		t.Fatal("NextAttempt(2) should be true")
	}
	d = b.Delay()
	if d != 250*time.Millisecond {
		t.Errorf("Attempt 2 = %v, want 250ms", d)
	}

	// Attempt 3: 400ms + 50% of 25% = 400 + 50 = 450ms
	if !b.NextAttempt() {
		t.Fatal("NextAttempt(3) should be true")
	}
	d = b.Delay()
	if d != 450*time.Millisecond {
		t.Errorf("Attempt 3 = %v, want 450ms", d)
	}

	// Should be exhausted
	if b.NextAttempt() {
		t.Error("Should be exhausted")
	}
}

// =============================================================================
// Delay returns 0 when no attempts made
// =============================================================================

func TestBackoff_DelayBeforeFirstAttempt(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 3, 0.0)

	d := b.Delay()
	if d != 0 {
		t.Errorf("Delay before any NextAttempt = %v, want 0", d)
	}
}

func TestBackoff_DelayAfterReset(t *testing.T) {
	b := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0, 3, 0.0)

	b.NextAttempt()
	b.Reset()

	// After reset, currentDelay is 0
	d := b.Delay()
	if d != 0 {
		t.Errorf("Delay after Reset = %v, want 0", d)
	}
}

// =============================================================================
// Concurrency safety of Reset (basic sanity)
// =============================================================================

func TestBackoff_ResetDoesNotAffectOtherFields(t *testing.T) {
	b := NewExponentialBackoff(
		500*time.Millisecond,
		10*time.Second,
		3.0,
		7,
		0.25,
	)

	b.Reset()

	// Reset should only affect attempt and currentDelay
	if b.InitialDelay != 500*time.Millisecond {
		t.Errorf("InitialDelay changed to %v", b.InitialDelay)
	}
	if b.MaxDelay != 10*time.Second {
		t.Errorf("MaxDelay changed to %v", b.MaxDelay)
	}
	if b.Multiplier != 3.0 {
		t.Errorf("Multiplier changed to %v", b.Multiplier)
	}
	if b.MaxAttempts != 7 {
		t.Errorf("MaxAttempts changed to %v", b.MaxAttempts)
	}
	if b.Jitter != 0.25 {
		t.Errorf("Jitter changed to %v", b.Jitter)
	}
}

// =============================================================================
// Delay progression with jitter and cap
// =============================================================================

func TestBackoff_JitterWithMaxDelayCap(t *testing.T) {
	b := NewExponentialBackoff(1, 100*time.Millisecond, 2.0, 10, 0.5).
		WithRand(&FakeRand{Values: []float64{1.0, 1.0, 1.0}})

	// Keep trying until we hit the cap
	for b.NextAttempt() {
		d := b.Delay()
		if d >= 100*time.Millisecond {
			// Once capped, jitter adds up to 50% more
			break
		}
	}

	// Now verify capped + jitter behavior
	for i := 0; i < 3; i++ {
		if !b.NextAttempt() {
			break
		}
		d := b.Delay()
		// Base is capped at 100ms, jitter adds up to 50% = up to 150ms
		if d < 100*time.Millisecond || d > 150*time.Millisecond {
			t.Errorf("Capped with jitter: delay = %v, want [100ms, 150ms]", d)
		}
	}
}
