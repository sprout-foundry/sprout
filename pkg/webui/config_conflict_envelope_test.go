//go:build !js

package webui

import (
	"errors"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestConfigConflictEnvelope_BuildsExpectedShape(t *testing.T) {
	now := time.Now()
	ccErr := &configuration.ConfigConflictError{
		Path:           "/tmp/sprout/config.json",
		LoadedModTime:  now.Add(-1 * time.Minute),
		LoadedSize:     100,
		CurrentModTime: now,
		CurrentSize:    150,
	}

	envelope, ok := configConflictEnvelope(ccErr, nil)
	if !ok {
		t.Fatal("configConflictEnvelope should recognize a ConfigConflictError")
	}
	if envelope["type"] != "error" {
		t.Errorf("envelope type = %v, want \"error\"", envelope["type"])
	}
	data, _ := envelope["data"].(map[string]interface{})
	if data == nil {
		t.Fatal("envelope.data missing or wrong shape")
	}
	if data["code"] != configConflictErrorCode {
		t.Errorf("data.code = %v, want %q", data["code"], configConflictErrorCode)
	}
	if data["path"] != ccErr.Path {
		t.Errorf("data.path = %v, want %q", data["path"], ccErr.Path)
	}
	if data["message"] != ccErr.Error() {
		t.Errorf("data.message = %v, want %q", data["message"], ccErr.Error())
	}
	if _, present := data["current_summary"]; !present {
		t.Error("data.current_summary should always be present (even if empty)")
	}
}

func TestConfigConflictEnvelope_NonConflictErrorReturnsFalse(t *testing.T) {
	if _, ok := configConflictEnvelope(errors.New("some other error"), nil); ok {
		t.Error("non-ConfigConflictError should return ok=false")
	}
	if _, ok := configConflictEnvelope(nil, nil); ok {
		t.Error("nil error should return ok=false")
	}
}

func TestConfigConflictEnvelope_WrappedErrorStillDetected(t *testing.T) {
	wrapped := &wrapErr{
		err: &configuration.ConfigConflictError{
			Path: "/tmp/cfg.json",
		},
	}
	envelope, ok := configConflictEnvelope(wrapped, nil)
	if !ok {
		t.Fatal("envelope helper must use errors.As to unwrap")
	}
	if envelope["data"].(map[string]interface{})["path"] != "/tmp/cfg.json" {
		t.Error("path field should reach the envelope through errors.As unwrap")
	}
}

// wrapErr is a minimal error wrapper exposing Unwrap so errors.As can
// traverse it — pins the contract that nested ConfigConflictError is
// still detected.
type wrapErr struct{ err error }

func (w *wrapErr) Error() string { return "wrapped: " + w.err.Error() }
func (w *wrapErr) Unwrap() error { return w.err }
