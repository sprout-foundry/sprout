package changes

import (
	"strings"
	"testing"
)

// TestChangeBuffer_EvictsPastCountCap proves the in-memory change buffer is
// bounded: appending far past the cap keeps the buffer at/under the cap and
// retains the most recent entries (oldest are evicted first).
func TestChangeBuffer_EvictsPastCountCap(t *testing.T) {
	prevChanges, prevBytes := maxTrackedChanges, maxTrackedBytes
	SetTrackedChangeCaps(100, 0)
	t.Cleanup(func() { SetTrackedChangeCaps(prevChanges, prevBytes) })

	ct := NewTrackerForTesting("rev")
	for i := 0; i < 1000; i++ {
		ct.appendChange(TrackedFileChange{
			FilePath:     "/w/f" + itoa(i),
			OriginalCode: "o",
			NewCode:      "n",
		})
	}

	got := ct.GetChanges()
	if len(got) > 100 {
		t.Errorf("buffer not capped: %d entries (cap 100)", len(got))
	}
	// The most recent entry must be present.
	if len(got) == 0 || got[len(got)-1].FilePath != "/w/f999" {
		t.Errorf("most recent entry must survive eviction, tail: %+v", got[len(got)-1:])
	}
	// The very first entry must be gone (oldest-first eviction).
	for _, ch := range got {
		if ch.FilePath == "/w/f0" {
			t.Error("the oldest entry should have been evicted")
		}
	}
}

// TestChangeBuffer_EvictsPastByteCap proves the byte cap bounds retained
// content even when the entry count is small.
func TestChangeBuffer_EvictsPastByteCap(t *testing.T) {
	prevChanges, prevBytes := maxTrackedChanges, maxTrackedBytes
	SetTrackedChangeCaps(0, 4096) // 4 KiB content cap
	t.Cleanup(func() { SetTrackedChangeCaps(prevChanges, prevBytes) })

	ct := NewTrackerForTesting("rev")
	big := strings.Repeat("x", 1024)
	for i := 0; i < 100; i++ {
		ct.appendChange(TrackedFileChange{
			FilePath:     "/w/big" + itoa(i),
			OriginalCode: big,
			NewCode:      big,
		})
	}
	if n := trackedContentBytes(ct.GetChanges()); n > 4096 {
		t.Errorf("retained content over the byte cap: %d > 4096", n)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
