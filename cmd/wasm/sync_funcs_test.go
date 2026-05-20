//go:build js && wasm

// Tests for the WASM sync transport scaffold (SP-046-1d/1f/1g). The actual
// WebSocket transport lives in `../platform`; these tests pin the agent-
// side hooks so when the WS lands they still behave correctly.
//
// Run via:
//   GOOS=js GOARCH=wasm go test \
//     -exec "$(go env GOROOT)/lib/wasm/go_js_wasm_exec" \
//     ./cmd/wasm/

package main

import (
	"syscall/js"
	"testing"
	"time"
)

func TestSyncEndpoint_RoundTrip(t *testing.T) {
	// Use the JS-side function path so we exercise the same surface the
	// platform code will hit. argString reads from a slice of js.Value;
	// build that directly here.
	result := setSyncEndpointFunc(js.Undefined(), []js.Value{js.ValueOf("wss://example.com/sync")})
	got := getSyncEndpointFunc(js.Undefined(), nil)
	if got != "wss://example.com/sync" {
		t.Errorf("getSyncEndpoint = %v, want wss://example.com/sync", got)
	}
	res, ok := result.(map[string]interface{})
	if !ok || res["ok"] != true {
		t.Errorf("setSyncEndpoint returned %v, want ok=true", result)
	}

	// Clearing with empty string should reset to unset.
	_ = setSyncEndpointFunc(js.Undefined(), []js.Value{js.ValueOf("")})
	if getSyncEndpointFunc(js.Undefined(), nil) != "" {
		t.Error("setSyncEndpoint('') should clear the endpoint")
	}
}

func TestApplyFileMetadata_PopulatesStash(t *testing.T) {
	const path = "src/main.go"
	const metaJSON = `{"browser_seq":7,"container_seq":3,"last_synced_browser":5,"last_synced_container":3}`
	result := applyFileMetadataFunc(js.Undefined(), []js.Value{
		js.ValueOf(path),
		js.ValueOf(metaJSON),
	})
	res, ok := result.(map[string]interface{})
	if !ok || res["ok"] != true {
		t.Fatalf("applyFileMetadata returned %v, want ok=true", result)
	}

	md, present := peekFileMetadata(path)
	if !present {
		t.Fatal("metadata not stashed after applyFileMetadata")
	}
	if md.BrowserSeq != 7 || md.LastSyncedBrowser != 5 {
		t.Errorf("md = %+v, want BrowserSeq=7, LastSyncedBrowser=5", md)
	}
	if !md.HasUnsyncedBrowserEdits() {
		t.Error("md.HasUnsyncedBrowserEdits() = false, want true")
	}
}

func TestApplyFileMetadata_RejectsBadJSON(t *testing.T) {
	result := applyFileMetadataFunc(js.Undefined(), []js.Value{
		js.ValueOf("p.txt"),
		js.ValueOf("not json"),
	})
	res, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	if _, has := res["error"]; !has {
		t.Errorf("expected error key in result, got %v", res)
	}
}

func TestSessionMoved_InvokesRegisteredHandler(t *testing.T) {
	called := make(chan struct{}, 1)
	cb := js.FuncOf(func(_ js.Value, _ []js.Value) interface{} {
		called <- struct{}{}
		return nil
	})
	defer cb.Release()

	res := onSessionMovedFunc(js.Undefined(), []js.Value{cb.Value})
	if r, _ := res.(map[string]interface{}); r["ok"] != true {
		t.Fatalf("onSessionMoved returned %v", res)
	}

	res = sessionMovedFunc(js.Undefined(), nil)
	if r, _ := res.(map[string]interface{}); r["ok"] != true {
		t.Fatalf("sessionMoved returned %v", res)
	}

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("session-moved handler was not invoked")
	}
}

func TestSessionMoved_WithoutHandlerReportsError(t *testing.T) {
	// Reset any prior registration (a previous test may have set one).
	syncMu.Lock()
	syncSessionMovedCB = js.Value{}
	syncMu.Unlock()

	res := sessionMovedFunc(js.Undefined(), nil)
	r, _ := res.(map[string]interface{})
	if _, has := r["error"]; !has {
		t.Errorf("expected error when no handler registered, got %v", res)
	}
}

func TestHeartbeat_StartStopFiresPings(t *testing.T) {
	// Use a tiny interval so the test runs fast; reset after.
	syncMu.Lock()
	saved := syncHeartbeatInterval
	syncHeartbeatInterval = 20 * time.Millisecond
	syncMu.Unlock()
	defer func() {
		syncMu.Lock()
		syncHeartbeatInterval = saved
		syncMu.Unlock()
	}()

	pings := make(chan struct{}, 10)
	pingFn := js.FuncOf(func(_ js.Value, _ []js.Value) interface{} {
		select {
		case pings <- struct{}{}:
		default:
		}
		return nil
	})
	defer pingFn.Release()

	res := startHeartbeatFunc(js.Undefined(), []js.Value{pingFn.Value})
	if r, _ := res.(map[string]interface{}); r["ok"] != true {
		t.Fatalf("startHeartbeat returned %v", res)
	}

	// Expect at least two pings within 100ms with a 20ms interval.
	deadline := time.After(200 * time.Millisecond)
	got := 0
	for got < 2 {
		select {
		case <-pings:
			got++
		case <-deadline:
			t.Fatalf("got %d pings, want at least 2", got)
		}
	}

	res = stopHeartbeatFunc(js.Undefined(), nil)
	r, _ := res.(map[string]interface{})
	if r["ok"] != true || r["was_running"] != true {
		t.Errorf("stopHeartbeat returned %v, want was_running=true", res)
	}
}

func TestHeartbeat_StopWithoutStartIsBenign(t *testing.T) {
	// Ensure no leftover from another test — re-stop is idempotent.
	_ = stopHeartbeatFunc(js.Undefined(), nil)
	res := stopHeartbeatFunc(js.Undefined(), nil)
	r, _ := res.(map[string]interface{})
	if r["ok"] != true || r["was_running"] != false {
		t.Errorf("idle stopHeartbeat returned %v, want ok=true was_running=false", res)
	}
}
