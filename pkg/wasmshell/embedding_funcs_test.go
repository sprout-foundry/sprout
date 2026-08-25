//go:build !wasm

package wasmshell

import "testing"

func TestEmbeddingMode_StringRoundTrip(t *testing.T) {
	for _, m := range []EmbeddingMode{ModeAuto, ModeOff, ModeONNX, ModeStatic} {
		if string(m) == "" {
			t.Errorf("mode %v has empty string", m)
		}
	}
}

func TestSetEmbeddingMode_RoundTrip(t *testing.T) {
	saved := CurrentMode()
	defer SetEmbeddingMode(saved)
	SetEmbeddingMode(ModeOff)
	if CurrentMode() != ModeOff {
		t.Fatal("expected ModeOff")
	}
	SetEmbeddingMode(ModeStatic)
	if CurrentMode() != ModeStatic {
		t.Fatal("expected ModeStatic")
	}
}

func TestGetEmbeddingStatus_DefaultValues(t *testing.T) {
	s := GetEmbeddingStatus()
	if s.Mode == "" {
		t.Errorf("expected non-empty Mode")
	}
}

func TestErrEmbeddingDisabled(t *testing.T) {
	if ErrEmbeddingDisabled == nil || ErrEmbeddingDisabled.Error() == "" {
		t.Fatal("ErrEmbeddingDisabled must be non-nil with a message")
	}
	if ErrEmbeddingUnavailable == nil || ErrEmbeddingUnavailable.Error() == "" {
		t.Fatal("ErrEmbeddingUnavailable must be non-nil with a message")
	}
}
