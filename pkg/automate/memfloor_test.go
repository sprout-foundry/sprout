//go:build !js

package automate

import (
	"fmt"
	"testing"
)

const realWorldVMStat = `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                                    5154.
Pages active:                                 90000.
Pages inactive:                              187359.
Pages speculative:                              350.
Pages throttled:                                  0.
Pages wired down:                               40000.
Pages purgeable:                                5573.
"Translation faults":                      200000000.
Pages copy-on-write:                        15000000.
Pages zero filled:                          60000000.
Pages reactivated:                          15000000.
Pages purged:                                 1000000.
File-backed pages:                            200000.
Anonymous pages:                              130000.
Pages stored in compressor:                   250000.
Pages occupied by compressor:                  50000.
Decompressions:                             14000000.
Compressions:                               19000000.
Pageins:                                    13000000.
Pageouts:                                     100000.
Swapins:                                       400000.
Swapouts:                                      600000.
`

func TestAvailableBytesFromVMStat_RealWorldCapture(t *testing.T) {
	avail, err := availableBytesFromVMStat(realWorldVMStat, 16384)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := (5154 + 187359 + 350 + 5573) * int64(16384)
	if avail != want {
		t.Errorf("avail = %d, want %d (free+inactive+speculative+purgeable)", avail, want)
	}
}

func TestAvailableBytesFromVMStat_MissingOptionalFields(t *testing.T) {
	out := `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                                    100.
Pages active:                                 90000.
Pages speculative:                              10.
`
	avail, err := availableBytesFromVMStat(out, 16384)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := (100 + 10) * int64(16384); avail != want {
		t.Errorf("avail = %d, want %d (missing inactive/purgeable count as 0)", avail, want)
	}
}

func TestAvailableBytesFromVMStat_MissingFreeIsError(t *testing.T) {
	if _, err := availableBytesFromVMStat("Pages inactive: 100.\n", 16384); err == nil {
		t.Error("expected error when Pages free is absent")
	}
}

func TestAvailableBytesFromVMStat_LargeInactiveIsNotLow(t *testing.T) {
	// The false-positive that motivated this: tiny free/speculative/purgeable,
	// large inactive. Pre-fix this read as ~173MB and tripped the 1.5GB floor.
	out := `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                                     100.
Pages inactive:                              120000.
Pages speculative:                               20.
Pages purgeable:                                  5.
`
	avail, err := availableBytesFromVMStat(out, 16384)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if avail < DefaultMinAvailableBytes {
		t.Errorf("avail %d is below the %d floor — large inactive must count", avail, DefaultMinAvailableBytes)
	}
}

func TestAvailableBytesFromVMStat_TrailingPeriods(t *testing.T) {
	out := "Pages free: 5154.\nPages inactive: 1000.\n"
	avail, err := availableBytesFromVMStat(out, 4096)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := (5154 + 1000) * int64(4096); avail != want {
		t.Errorf("avail = %d, want %d", avail, want)
	}
}

func TestPageSizeFromVMStatHeader(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{realWorldVMStat, 16384},
		{"Mach Virtual Memory Statistics: (page size of 4096 bytes)", 4096},
		{"Pages free: 1.\n", 0}, // no header
		{"Mach Virtual Memory Statistics:\nPages free: 1.\n", 0},
	}
	for _, c := range cases {
		if got := pageSizeFromVMStatHeader(c.in); got != c.want {
			t.Errorf("pageSizeFromVMStatHeader(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestVMStatFieldPages(t *testing.T) {
	if n, err := vmStatFieldPages(realWorldVMStat, "Pages purgeable"); err != nil || n != 5573 {
		t.Errorf("purgeable = %d, %v; want 5573, nil", n, err)
	}
	if n, err := vmStatFieldPages(realWorldVMStat, "Pages bogus"); err != nil || n != 0 {
		t.Errorf("missing field = %d, %v; want 0, nil", n, err)
	}
	if _, err := vmStatFieldPages("Pages free: oops.\n", "Pages free"); err == nil {
		t.Error("expected error for unparseable Pages free")
	}
	// "Pages freez" must not match "Pages free"; the old prefix-based
	// matcher would have returned 999 here.
	if n, err := vmStatFieldPages("Pages free: 5.\nPages freez: 999.\n", "Pages free"); err != nil || n != 5 {
		t.Errorf("exact-match = %d, %v; want 5, nil (sibling field must not match)", n, err)
	}
	// Negative optional fields read as 0, unlike a negative Pages free.
	if n, err := vmStatFieldPages("Pages inactive: -1.\n", "Pages inactive"); err != nil || n != 0 {
		t.Errorf("negative optional = %d, %v; want 0, nil", n, err)
	}
}

func TestFormatBytes_Gigabyte(t *testing.T) {
	// Sanity check for the error-message formatting on real-world numbers.
	avail, _ := availableBytesFromVMStat(realWorldVMStat, 16384)
	if got, want := formatBytes(avail), "3.0 GB"; got != want {
		t.Errorf("formatBytes(%d) = %q, want %q", avail, got, want)
	}
}

func TestAvailableBytesFromVMStat_NegativeFreeNotCounted(t *testing.T) {
	// Defensive: a negative or garbage free value must not silently pass.
	if _, err := availableBytesFromVMStat(fmt.Sprintf("Pages free: -1.\n"), 4096); err == nil {
		t.Error("expected error for negative Pages free")
	}
}
