//go:build !windows

package webui

import (
	"testing"
)

// ---------------------------------------------------------------------------
// isPIDAlive (Unix implementation using syscall.Kill)
// ---------------------------------------------------------------------------

func TestIsPIDAlive_NonPositive(t *testing.T) {
	tests := []struct {
		pid int
	}{
		{0},
		{-1},
		{-100},
	}
	for _, tt := range tests {
		if isPIDAlive(tt.pid) {
			t.Errorf("isPIDAlive(%d) = true, want false", tt.pid)
		}
	}
}

func TestIsPIDAlive_Pid1(t *testing.T) {
	// PID 1 (init) should always be alive on Unix
	if !isPIDAlive(1) {
		t.Error("isPIDAlive(1) = false, want true (init process)")
	}
}

func TestIsPIDAlive_NonExistentPID(t *testing.T) {
	// Very large PID should not exist
	got := isPIDAlive(999999999)
	if got {
		t.Errorf("isPIDAlive(999999999) = true, want false (non-existent PID)")
	}
}
