package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestGatewayWiringEndToEnd proves the client half of the gateway seam
// (issue #115): a workspace started with llm_provider=gateway lists THE
// GATEWAY'S models through its model-picker API, because the runner wrote
// the gateway provider file into the workspace's scoped config dir and put
// the workspace-scoped key in its environment. It needs a built sprout
// binary (SPROUT_RUNNER_E2E_BIN) and a fake gateway:
//
//	SPROUT_RUNNER_E2E_BIN=/path/to/sprout go test -run TestGatewayWiring ./pkg/runner/
func TestGatewayWiringEndToEnd(t *testing.T) {
	bin := os.Getenv("SPROUT_RUNNER_E2E_BIN")
	if bin == "" {
		t.Skip("set SPROUT_RUNNER_E2E_BIN to a built sprout binary")
	}
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())

	// The fake gateway: OpenAI-compatible /v1/models behind a bearer key.
	models := []map[string]any{
		{"id": "gw-large", "name": "Gateway Large", "context_length": 200000},
		{"id": "gw-small", "name": "Gateway Small", "context_length": 32000},
	}
	gotKey := make(chan string, 4)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/models") {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		gotKey <- strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": models})
	}))
	t.Cleanup(gateway.Close)

	l := &HostLauncher{SproutBin: bin}
	task := WorkspaceTask{
		WorkspaceID:    "e2e-gw",
		Action:         "start",
		TxnSecret:      "e2e-secret",
		LLMProvider:    "gateway",
		LLMKey:         "gw-ws-key-123",
		PlatformAPIURL: gateway.URL,
	}
	ctx := context.Background()
	ws, err := l.Start(ctx, task)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = l.Destroy(ctx, task.WorkspaceID, ws) })

	// The daemon lists the gateway's models under the gateway provider —
	// the model picker reads exactly this.
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(ws.Port) + "/api/providers/models?provider=gateway")
	if err != nil {
		t.Fatalf("model list: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("model list: %d", resp.StatusCode)
	}
	var body struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode model list: %v", err)
	}
	ids := map[string]bool{}
	for _, m := range body.Models {
		ids[m.ID] = true
	}
	for _, want := range []string{"gw-large", "gw-small"} {
		if !ids[want] {
			t.Errorf("the gateway's model %q must be listed; got %v", want, ids)
		}
	}

	// The daemon authenticated to the gateway with the workspace-scoped
	// key from the start task (never the runner's own credentials).
	select {
	case key := <-gotKey:
		if key != "gw-ws-key-123" {
			t.Errorf("the daemon must present the workspace gateway key; got %q", key)
		}
	default:
		t.Error("the daemon never called the gateway's /v1/models")
	}

	// The provider file lives in the workspace's scoped config dir and
	// dies with it.
	provPath := filepath.Join(os.Getenv("SPROUT_STATE_DIR"), "runner", "workspaces", task.WorkspaceID, "config", "providers", "gateway.json")
	if _, err := os.Stat(provPath); err != nil {
		t.Errorf("gateway provider file must be in the workspace's scoped config dir: %v", err)
	}
}
