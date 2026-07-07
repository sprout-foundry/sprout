package console

import (
	"bytes"
	"testing"
)

func TestStatusFooter_SetShowKeymapHint_SetsFlag(t *testing.T) {
	f := &StatusFooter{w: &bytes.Buffer{}, isTTY: true}
	f.SetShowKeymapHint(true)
	f.mu.Lock()
	got := f.showKeymapHint
	f.mu.Unlock()
	if !got {
		t.Error("expected showKeymapHint=true after SetShowKeymapHint(true)")
	}
}

func TestStatusFooter_SetShowKeymapHint_Disable(t *testing.T) {
	f := &StatusFooter{w: &bytes.Buffer{}, isTTY: true}
	f.SetShowKeymapHint(true)
	f.SetShowKeymapHint(false)
	f.mu.Lock()
	got := f.showKeymapHint
	f.mu.Unlock()
	if got {
		t.Error("expected showKeymapHint=false after SetShowKeymapHint(false)")
	}
}

func TestStatusFooter_SetShowKeymapHint_NilSafe(t *testing.T) {
	var f *StatusFooter
	f.SetShowKeymapHint(true) // should not panic
}

func TestStatusFooter_hintRowCount_ReturnsOneWhenEnabled(t *testing.T) {
	f := &StatusFooter{w: &bytes.Buffer{}, isTTY: true}
	f.SetShowKeymapHint(true)
	got := f.hintRowCount()
	if got != 1 {
		t.Errorf("hintRowCount() = %d, want 1", got)
	}
}

func TestStatusFooter_hintRowCount_ReturnsZeroWhenDisabled(t *testing.T) {
	f := &StatusFooter{w: &bytes.Buffer{}, isTTY: true}
	got := f.hintRowCount()
	if got != 0 {
		t.Errorf("hintRowCount() = %d, want 0 when hint is disabled", got)
	}
}

func TestStatusFooter_reservedRows_NoSteerNoHint(t *testing.T) {
	f := &StatusFooter{w: &bytes.Buffer{}, isTTY: true}
	// steerActive=false, showKeymapHint=false → 2 rows (rule + content).
	got := f.reservedRows()
	if got != 2 {
		t.Errorf("reservedRows() = %d, want 2 (no steer, no hint)", got)
	}
}

func TestStatusFooter_reservedRows_NoSteerWithHint(t *testing.T) {
	f := &StatusFooter{w: &bytes.Buffer{}, isTTY: true}
	f.SetShowKeymapHint(true)
	// steerActive=false, showKeymapHint=true → 3 rows.
	got := f.reservedRows()
	if got != 3 {
		t.Errorf("reservedRows() = %d, want 3 (no steer, hint)", got)
	}
}

func TestStatusFooter_reservedRows_SteerOneRowNoHint(t *testing.T) {
	f := &StatusFooter{
		w:           &bytes.Buffer{},
		isTTY:       true,
		steerActive: true,
		steerLine:   "hello",
	}
	// steerActive=true (1 row), showKeymapHint=false → 3 rows.
	got := f.reservedRows()
	if got != 3 {
		t.Errorf("reservedRows() = %d, want 3 (steer=1, no hint)", got)
	}
}

func TestStatusFooter_reservedRows_SteerOneRowWithHint(t *testing.T) {
	f := &StatusFooter{
		w:           &bytes.Buffer{},
		isTTY:       true,
		steerActive: true,
		steerLine:   "hello",
	}
	f.SetShowKeymapHint(true)
	// steerActive=true (1 row), showKeymapHint=true → 4 rows.
	got := f.reservedRows()
	if got != 4 {
		t.Errorf("reservedRows() = %d, want 4 (steer=1, hint)", got)
	}
}

func TestStatusFooter_reservedRows_SteerTwoRowsWithHint(t *testing.T) {
	f := &StatusFooter{
		w:           &bytes.Buffer{},
		isTTY:       true,
		steerActive: true,
		steerLine:   "line1\nline2",
	}
	f.SetShowKeymapHint(true)
	// steerActive=true (2 rows), showKeymapHint=true → 5 rows.
	got := f.reservedRows()
	if got != 5 {
		t.Errorf("reservedRows() = %d, want 5 (steer=2, hint)", got)
	}
}

func TestStatusFooter_reservedRows_NilSafe(t *testing.T) {
	var f *StatusFooter
	// Should not panic.
	_ = f.reservedRows()
}

func TestStatusFooter_hintRowCount_NilSafe(t *testing.T) {
	var f *StatusFooter
	// Should not panic.
	_ = f.hintRowCount()
}

func TestSteerRowFor_WithHint(t *testing.T) {
	cases := []struct {
		name      string
		rows      int
		steerRows int
		hintRows  int
		i         int
		want      int
	}{
		{"steer=1,hint=1,i=0", 24, 1, 1, 0, 21},
		{"steer=2,hint=1,i=0", 24, 2, 1, 0, 20},
		{"steer=2,hint=1,i=1", 24, 2, 1, 1, 21},
		{"steer=1,hint=0,i=0", 24, 1, 0, 0, 22},
		{"steer=0,hint=1,i=0", 24, 0, 1, 0, 22},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := steerRowFor(c.rows, c.steerRows, c.hintRows, c.i)
			if got != c.want {
				t.Errorf("steerRowFor(rows=%d, steerRows=%d, hintRows=%d, i=%d) = %d, want %d",
					c.rows, c.steerRows, c.hintRows, c.i, got, c.want)
			}
		})
	}
}
