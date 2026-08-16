//go:build linux && !js

package embedding

import "testing"

func TestParseMemAvailable(t *testing.T) {
	content := "MemTotal:       16384000 kB\n" +
		"MemFree:         2000000 kB\n" +
		"MemAvailable:    1500000 kB\n" +
		"Buffers:          123456 kB\n"
	got, ok := parseMemAvailable(content)
	if !ok {
		t.Fatal("expected MemAvailable to be found")
	}
	if want := uint64(1500000) * 1024; got != want {
		t.Errorf("parseMemAvailable = %d, want %d", got, want)
	}
}

func TestParseMemAvailableMissing(t *testing.T) {
	if _, ok := parseMemAvailable("MemTotal: 100 kB\nMemFree: 50 kB\n"); ok {
		t.Error("expected (0, false) when MemAvailable is absent")
	}
}

func TestParseMemAvailableMalformed(t *testing.T) {
	if _, ok := parseMemAvailable("MemAvailable: not-a-number kB\n"); ok {
		t.Error("expected (0, false) when MemAvailable is malformed")
	}
}
