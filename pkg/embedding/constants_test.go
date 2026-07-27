package embedding

import (
	"testing"
	"time"
)

func TestAutoBuildTimeout_SmallWorkspace(t *testing.T) {
	// 10 files: 30s base + 10*4*100ms = 34s, clamped to minTimeout (2min).
	got := autoBuildTimeout(10)
	if got != 2*time.Minute {
		t.Errorf("small workspace: expected min 2min, got %v", got)
	}
}

func TestAutoBuildTimeout_MediumWorkspace(t *testing.T) {
	// 500 files: 30s + 500*4*100ms = 30s + 200s = 230s = 3m50s.
	got := autoBuildTimeout(500)
	want := 30*time.Second + 500*4*100*time.Millisecond
	if got != want {
		t.Errorf("medium workspace: expected %v, got %v", want, got)
	}
}

func TestAutoBuildTimeout_LargeWorkspace(t *testing.T) {
	// 3000 files: 30s + 3000*4*100ms = 30s + 1200s = 1230s = 20.5min,
	// clamped to maxTimeout (15min).
	got := autoBuildTimeout(3000)
	if got != 15*time.Minute {
		t.Errorf("large workspace: expected max 15min, got %v", got)
	}
}

func TestAutoBuildTimeout_NeverNegative(t *testing.T) {
	got := autoBuildTimeout(0)
	if got < 2*time.Minute {
		t.Errorf("zero files: expected min 2min, got %v", got)
	}
}
