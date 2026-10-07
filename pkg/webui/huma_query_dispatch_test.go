//go:build !js

package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHumaMuxDispatch proves the migrated Huma operations dispatch through the
// live ServeMux (registerHumaRoutes → registerHumaOperations) and emit exactly
// the bytes the plain handlers did, closing the gap between "registered" and
// "wired correctly + byte-identical".
//
// It exercises the deterministic routes (no agent / no provider / no state
// writes): GET /api/query/status (a Huma op that was a plain handler) and
// POST /api/query/stop (no active query in a fresh server → the 200
// already_completed payload). For the status route the response carries no
// dynamic fields, so we assert BYTE-identity: the builder writes the response
// through the live ResponseWriter via writeJSON (json.NewEncoder) and the Huma
// output is a no-op func body, so the wire bytes must be exactly what a fresh
// json.NewEncoder would produce. A regression that made Huma append anything
// (schema links, extra headers, a second body) or alter the encoding would
// change those bytes and fail the check.
//
// It also checks the method gate on a POST-only Huma route: GET /api/query must
// not be served by the POST operation (Huma enforces the method), i.e. it must
// not return a 2xx.
func TestHumaMuxDispatch(t *testing.T) {
	ws, _ := newTestWebServer(t)
	mux := ws.setupRoutes(context.Background())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// GET /api/query/status — a Huma operation (was a plain handler). The
	// client context exists (newTestWebServer), so resolveClientID/resolveChatID
	// resolve and the builder writes a deterministic 200 with active=false.
	resp, err := http.Get(srv.URL + "/api/query/status")
	if err != nil {
		t.Fatalf("GET /api/query/status: %v", err)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /api/query/status body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close /api/query/status body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/query/status: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var status map[string]any
	if err := json.Unmarshal(bodyBytes, &status); err != nil {
		t.Fatalf("decode /api/query/status body: %v", err)
	}
	if status["active"] != false {
		t.Fatalf("GET /api/query/status: active = %v, want false", status["active"])
	}

	// Byte-identity: re-encode the exact payload the builder writes (active +
	// chat_id) through the same json.NewEncoder writeJSON uses, and require the
	// wire bytes to be identical.
	expected := map[string]any{
		"active":  false,
		"chat_id": status["chat_id"],
	}
	var expectedBuf bytes.Buffer
	if err := json.NewEncoder(&expectedBuf).Encode(expected); err != nil {
		t.Fatalf("encode expected status payload: %v", err)
	}
	if !bytes.Equal(bodyBytes, expectedBuf.Bytes()) {
		t.Fatalf("GET /api/query/status: body not byte-identical to writeJSON output.\n got: %s\nwant: %s",
			bodyBytes, expectedBuf.Bytes())
	}

	// POST /api/query/stop — no active query on a fresh server, so the shared
	// stopActiveQuery helper writes the 200 already_completed payload via
	// writeJSON. Assert the payload shape (the timestamp is dynamic, so no
	// full byte comparison here; the byte-identity mechanism is proven by the
	// status check above, which uses the same builder → writeJSON path).
	stopResp, err := http.Post(srv.URL+"/api/query/stop", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/query/stop: %v", err)
	}
	stopBody, err := io.ReadAll(stopResp.Body)
	if err != nil {
		t.Fatalf("read /api/query/stop body: %v", err)
	}
	if err := stopResp.Body.Close(); err != nil {
		t.Errorf("close /api/query/stop body: %v", err)
	}
	if stopResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/query/stop: status = %d, want %d", stopResp.StatusCode, http.StatusOK)
	}
	var stop map[string]any
	if err := json.Unmarshal(stopBody, &stop); err != nil {
		t.Fatalf("decode /api/query/stop body: %v", err)
	}
	if stop["status"] != "ok" || stop["already_completed"] != true {
		t.Fatalf("POST /api/query/stop: payload = %v, want status=ok already_completed=true", stop)
	}

	// Method gate on a POST-only Huma route: GET /api/query must not be served
	// by the POST operation (Huma enforces the method), so it must not return a
	// 2xx.
	gateResp, err := http.Get(srv.URL + "/api/query")
	if err != nil {
		t.Fatalf("GET /api/query: %v", err)
	}
	if _, err := io.Copy(io.Discard, gateResp.Body); err != nil {
		t.Errorf("drain /api/query gate body: %v", err)
	}
	if err := gateResp.Body.Close(); err != nil {
		t.Errorf("close /api/query gate body: %v", err)
	}
	if gateResp.StatusCode == http.StatusOK || gateResp.StatusCode == http.StatusAccepted {
		t.Fatalf("GET /api/query: status = %d, want a non-2xx method-gate response", gateResp.StatusCode)
	}
}
