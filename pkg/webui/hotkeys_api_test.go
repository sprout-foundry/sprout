//go:build !js

package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestHandleAPIHotkeys_GET(t *testing.T) {
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/hotkeys", nil)
	rec := httptest.NewRecorder()

	server.handleAPIHotkeys(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	if _, ok := resp["version"]; !ok {
		t.Error("expected version field in response")
	}
	if _, ok := resp["hotkeys"]; !ok {
		t.Error("expected hotkeys field in response")
	}
}

func TestHandleAPIHotkeys_PUT_Valid(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	config := HotkeyConfig{
		Version: "1.0",
		Hotkeys: []HotkeyEntry{
			{Key: "Ctrl+S", CommandID: "save_file"},
		},
	}
	body, _ := json.Marshal(config)

	req := httptest.NewRequest(http.MethodPut, "/api/hotkeys", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleAPIHotkeys(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if success, ok := resp["success"].(bool); !ok || !success {
		t.Error("expected success=true")
	}
}

func TestHandleAPIHotkeys_PUT_InvalidConfig(t *testing.T) {
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	config := HotkeyConfig{
		Version: "", // missing version triggers validation error
		Hotkeys: []HotkeyEntry{
			{Key: "Ctrl+S", CommandID: "save_file"},
		},
	}
	body, _ := json.Marshal(config)

	req := httptest.NewRequest(http.MethodPut, "/api/hotkeys", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleAPIHotkeys(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestHandleAPIHotkeys_PUT_InvalidJSON(t *testing.T) {
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/hotkeys", bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()

	server.handleAPIHotkeys(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestHandleAPIHotkeys_MethodNotAllowed(t *testing.T) {
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/hotkeys", nil)
	rec := httptest.NewRecorder()

	server.handleAPIHotkeys(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", rec.Code)
	}
}

func TestHandleAPIHotkeysValidate(t *testing.T) {
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("valid config returns 200", func(t *testing.T) {
		config := HotkeyConfig{
			Version: "1.0",
			Hotkeys: []HotkeyEntry{
				{Key: "Ctrl+S", CommandID: "save_file"},
			},
		}
		body, _ := json.Marshal(config)

		req := httptest.NewRequest(http.MethodPost, "/api/hotkeys/validate", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		server.handleAPIHotkeysValidate(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}
		if valid, ok := resp["valid"].(bool); !ok || !valid {
			t.Error("expected valid=true")
		}
	})

	t.Run("invalid config returns 400", func(t *testing.T) {
		config := HotkeyConfig{
			Version: "1.0",
			Hotkeys: []HotkeyEntry{
				{Key: "", CommandID: "save_file"}, // empty key triggers validation error
			},
		}
		body, _ := json.Marshal(config)

		req := httptest.NewRequest(http.MethodPost, "/api/hotkeys/validate", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		server.handleAPIHotkeysValidate(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rec.Code)
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/hotkeys/validate", bytes.NewReader([]byte("not json")))
		rec := httptest.NewRecorder()

		server.handleAPIHotkeysValidate(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rec.Code)
		}
	})

	t.Run("non-POST returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/hotkeys/validate", nil)
		rec := httptest.NewRecorder()

		server.handleAPIHotkeysValidate(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected status 405, got %d", rec.Code)
		}
	})
}

func TestHandleAPIHotkeysPreset(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("vscode preset returns 200", func(t *testing.T) {
		reqBody := map[string]string{"preset": "vscode"}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/hotkeys/preset", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		server.handleAPIHotkeysPreset(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}
		if success, ok := resp["success"].(bool); !ok || !success {
			t.Error("expected success=true")
		}
		if preset, ok := resp["preset"].(string); !ok || preset != "vscode" {
			t.Errorf("expected preset=vscode, got %v", resp["preset"])
		}
	})

	t.Run("empty preset returns 400", func(t *testing.T) {
		reqBody := map[string]string{"preset": ""}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/hotkeys/preset", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		server.handleAPIHotkeysPreset(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rec.Code)
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/hotkeys/preset", bytes.NewReader([]byte("not json")))
		rec := httptest.NewRecorder()

		server.handleAPIHotkeysPreset(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rec.Code)
		}
	})

	t.Run("non-POST returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/hotkeys/preset", nil)
		rec := httptest.NewRecorder()

		server.handleAPIHotkeysPreset(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected status 405, got %d", rec.Code)
		}
	})
}
