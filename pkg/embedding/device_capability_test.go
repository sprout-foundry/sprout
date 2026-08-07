package embedding

import (
	"strings"
	"testing"
)

// dualModelSupported gates the second ONNX provider on RAM-constrained
// devices. The threshold itself is a constant, so the test exercises the
// fallback path (unknown memory → enable) and the parse path (Linux
// /proc/meminfo layout).
//
// We can't easily simulate "this device has 4 GB" from a test — the read
// hits /proc/meminfo on real Linux/Android. So the tests cover what we CAN
// verify: the parser, the unknown-OS default, and the threshold comparison.

func TestDualModelSupportedThreshold(t *testing.T) {
	// The floor must be 16 GB exactly. Changing it requires re-measuring on
	// the smallest target device class, per the device_capability.go doc —
	// don't bump it on a hunch.
	if dualModelMemoryFloor != 16<<30 {
		t.Fatalf("dualModelMemoryFloor = %d bytes, want exactly %d (16 GB) — see device_capability.go doc",
			dualModelMemoryFloor, 16<<30)
	}
}

func TestTotalSystemMemoryParsesMemTotal(t *testing.T) {
	// totalSystemMemory returns the MemTotal field from /proc/meminfo. On
	// any Linux/Android runner this is non-zero; on other platforms it
	// returns (0, false). Verify both the value and the OK flag are sane.
	mem, ok := totalSystemMemory()
	switch {
	case ok:
		if mem <= 0 {
			t.Errorf("totalSystemMemory returned ok=true but mem=%d (want > 0)", mem)
		}
		// Sanity ceiling — no test runner has 1 TB of RAM.
		if mem > 1<<40 {
			t.Errorf("totalSystemMemory returned mem=%d (> 1 TB); parser is wrong", mem)
		}
	default:
		// ok=false is expected on non-Linux platforms (macOS/Windows CI).
		// Log so a failure on Linux/Android is visible.
		t.Logf("totalSystemMemory returned ok=false (likely non-Linux OS); skipping value check")
	}
}

func TestDualModelSupportedMatchesMemTotal(t *testing.T) {
	// When /proc/meminfo is readable, dualModelSupported must agree with
	// whether MemTotal >= the floor. Verify the two functions don't drift.
	mem, ok := totalSystemMemory()
	want := dualModelSupported()
	if want != ok {
		// ok=true means "we read it"; want means "mem >= floor". If we
		// successfully read memory, want must reflect the comparison.
		if ok {
			actualGate := mem >= dualModelMemoryFloor
			if actualGate != want {
				t.Errorf("dualModelSupported() = %v, but MemTotal %d vs floor %d suggests %v",
					want, mem, dualModelMemoryFloor, actualGate)
			}
		}
	}
}

// The parser must accept the canonical /proc/meminfo layout. We exercise
// the parsing logic directly via a string-fed variant — extract the
// parsing into a pure function so it's testable without faking /proc.
func TestParseMemTotalFromMeminfo(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		want   int64
		wantOk bool
	}{
		{
			name: "typical linux meminfo",
			input: strings.Join([]string{
				"MemTotal:       16384000 kB",
				"MemFree:         8192000 kB",
				"MemAvailable:   12000000 kB",
				"",
			}, "\n"),
			want:   16384000 * 1024,
			wantOk: true,
		},
		{
			name: "android meminfo (same format)",
			input: strings.Join([]string{
				"MemTotal:       11113796 kB", // ~11 GB Snapdragon, observed on a real device
				"MemFree:         1413000 kB",
				"MemAvailable:    2854000 kB",
				"",
			}, "\n"),
			want:   11113796 * 1024,
			wantOk: true,
		},
		{
			name:   "missing MemTotal",
			input:  "MemFree:   1024 kB\n",
			want:   0,
			wantOk: false,
		},
		{
			name:   "blank",
			input:  "",
			want:   0,
			wantOk: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseMemTotal(tc.input)
			if ok != tc.wantOk {
				t.Fatalf("parseMemTotal(%q) ok = %v, want %v", tc.name, ok, tc.wantOk)
			}
			if got != tc.want {
				t.Errorf("parseMemTotal(%q) = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}
