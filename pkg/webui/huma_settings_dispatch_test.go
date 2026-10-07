//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHumaSettingsDispatch proves the settings/configuration-family Huma
// operations dispatch through the live ServeMux and emit the same response the
// plain handlers did. The operations are thin wrappers over the existing
// handlers, so a regression that broke the Huma wiring would surface as a wrong
// status or a missing payload. It exercises deterministic read routes that need
// no live agent or provider (a GET read that returns a best-effort shape, a
// subtree GET) plus a method gate.
func TestHumaSettingsDispatch(t *testing.T) {
	ws, _ := newTestWebServer(t)
	mux := ws.setupRoutes(context.Background())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// GET /api/local-llm/status — a Huma operation (was a plain handler). The
	// response is deterministic: it reports the local-LLM runtime status, which
	// always carries a `platform` field (the current OS-arch).
	resp, err := srv.Client().Get(srv.URL + "/api/local-llm/status")
	if err != nil {
		t.Fatalf("GET /api/local-llm/status: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /api/local-llm/status body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close /api/local-llm/status body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/local-llm/status: status = %d, want %d (body: %s)", resp.StatusCode, http.StatusOK, body)
	}
	var st map[string]any
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatalf("decode /api/local-llm/status body: %v", err)
	}
	if _, hasPlatform := st["platform"]; !hasPlatform {
		t.Fatalf("GET /api/local-llm/status: response missing `platform` field (body: %s)", body)
	}

	// GET /api/settings/subagent-types — a Huma operation that reuses the plain
	// handler's best-effort fallback: with no config manager it still returns
	// 200 with a `subagent_types` field.
	subResp, err := srv.Client().Get(srv.URL + "/api/settings/subagent-types")
	if err != nil {
		t.Fatalf("GET /api/settings/subagent-types: %v", err)
	}
	subBody, err := io.ReadAll(subResp.Body)
	if err != nil {
		t.Fatalf("read /api/settings/subagent-types body: %v", err)
	}
	if err := subResp.Body.Close(); err != nil {
		t.Errorf("close /api/settings/subagent-types body: %v", err)
	}
	if subResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/settings/subagent-types: status = %d, want %d (body: %s)", subResp.StatusCode, http.StatusOK, subBody)
	}
	var sub map[string]any
	if err := json.Unmarshal(subBody, &sub); err != nil {
		t.Fatalf("decode /api/settings/subagent-types body: %v", err)
	}
	if _, hasTypes := sub["subagent_types"]; !hasTypes {
		t.Fatalf("GET /api/settings/subagent-types: response missing `subagent_types` field (body: %s)", subBody)
	}

	// GET /api/local-llm/start — a write route served by GET should reach the
	// SPA catch-all, not a JSON 200. The Huma operation is registered for POST
	// only; a wrong method has no matching method+path pattern, so it falls
	// through to the "/" catch-all (the same behavior the migrated git family
	// relies on).
	gateResp, err := srv.Client().Get(srv.URL + "/api/local-llm/start")
	if err != nil {
		t.Fatalf("GET /api/local-llm/start: %v", err)
	}
	if _, err := io.Copy(io.Discard, gateResp.Body); err != nil {
		t.Errorf("drain /api/local-llm/start body: %v", err)
	}
	if err := gateResp.Body.Close(); err != nil {
		t.Errorf("close /api/local-llm/start body: %v", err)
	}
	if gateResp.StatusCode == http.StatusOK && strings.Contains(strings.ToLower(gateResp.Header.Get("Content-Type")), "application/json") {
		t.Fatalf("GET /api/local-llm/start unexpectedly served a JSON 200; the method gate should reject it (status %d)", gateResp.StatusCode)
	}
}
