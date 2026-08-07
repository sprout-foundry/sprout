package embedding

import (
	"testing"
)

func TestCommName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/home/user/go/bin/sprout", "sprout"},
		{"./sprout", "sprout"},
		{"sprout", "sprout"},
		{"/usr/local/bin/sprout-agent", "sprout-agent"},
		// /proc/*/comm is truncated to 15 chars
		{"/usr/local/bin/sprout-embedding-daemon", "sprout-embeddin"},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := commName(tc.path)
			if got != tc.want {
				t.Errorf("commName(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestDefaultIntraOpThreads_Override(t *testing.T) {
	t.Setenv("SPROUT_ONNX_THREADS", "6")
	if got := defaultIntraOpThreads(); got != 6 {
		t.Errorf("with SPROUT_ONNX_THREADS=6, got %d, want 6", got)
	}
}

func TestDefaultIntraOpThreads_ZeroOverride(t *testing.T) {
	t.Setenv("SPROUT_ONNX_THREADS", "0")
	if got := defaultIntraOpThreads(); got != 1 {
		t.Errorf("with SPROUT_ONNX_THREADS=0, got %d, want 1 (floor)", got)
	}
}

func TestDefaultIntraOpThreads_InvalidOverride(t *testing.T) {
	t.Setenv("SPROUT_ONNX_THREADS", "not-a-number")
	// Should fall through to auto-detection, which returns >= 1
	got := defaultIntraOpThreads()
	if got < 1 {
		t.Errorf("with invalid override, got %d, want >= 1", got)
	}
}

func TestCountSproutProcesses(t *testing.T) {
	n := countSproutProcesses()
	if n < 1 {
		t.Errorf("countSproutProcesses() = %d, want >= 1 (at least this process)", n)
	}
}
