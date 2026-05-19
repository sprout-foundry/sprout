package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleAPIDriftResponse(t *testing.T) {
	ws, _ := newTestWebServer(t)

	tests := []struct {
		name           string
		requestBody    driftResponseRequest
		expectedStatus int
	}{
		{
			name: "started new chat",
			requestBody: driftResponseRequest{
				StartedNewChat: true,
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "dismissed notification",
			requestBody: driftResponseRequest{
				StartedNewChat: false,
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create request
			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest(http.MethodPost, "/api/drift-response", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(webClientIDHeader, "test-client")

			// Execute request
			rec := httptest.NewRecorder()
			ws.handleAPIDriftResponse(rec, req)

			// Verify status
			if rec.Code != tt.expectedStatus {
				t.Errorf("handleAPIDriftResponse() status = %v, want %v", rec.Code, tt.expectedStatus)
			}

			// Verify response body
			var resp map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Errorf("failed to decode response: %v", err)
			}
			if resp["status"] != "ok" {
				t.Errorf("expected status 'ok', got %q", resp["status"])
			}
		})
	}
}

func TestHandleAPIDriftResponse_InvalidJSON(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/drift-response", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	ws.handleAPIDriftResponse(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("handleAPIDriftResponse() status = %v, want %v", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleAPIDriftResponse_WrongMethod(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/drift-response", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIDriftResponse(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("handleAPIDriftResponse() status = %v, want %v", rec.Code, http.StatusMethodNotAllowed)
	}
}