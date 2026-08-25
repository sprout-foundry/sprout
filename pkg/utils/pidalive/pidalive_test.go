//go:build !js && !windows

package pidalive

import (
	"os"
	"testing"
)

func TestIsAlive(t *testing.T) {
	tests := []struct {
		name     string
		pid      int
		want     bool
		skipFunc func() bool
	}{
		{
			name: "zero PID returns false",
			pid:  0,
			want: false,
		},
		{
			name: "negative PID returns false",
			pid:  -1,
			want: false,
		},
		{
			name: "current process returns true",
			pid:  os.Getpid(),
			want: true,
		},
		{
			name: "very high PID returns false",
			pid:  999999999,
			want: false,
		},
		{
			name: "init process (PID 1) returns true on Unix",
			pid:  1,
			want: true,
			skipFunc: func() bool {
				// Skip on non-Linux Unix systems where PID 1 might not be init
				// or might have different semantics
				return os.Getenv("OS") == "Windows_NT" || os.Getenv("TERM") == "dumb"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipFunc != nil && tt.skipFunc() {
				t.Skip("skipping on this platform")
			}
			if got := IsAlive(tt.pid); got != tt.want {
				t.Errorf("IsAlive(%d) = %v, want %v", tt.pid, got, tt.want)
			}
		})
	}
}
