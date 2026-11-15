//go:build darwin && arm64 && cgo

package gpulink

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// The sinter engine end to end on this machine: a real model loaded
// in-process answers /v1/models and a chat completion through the agent's
// relay handler. Set SPROUT_GPU_SINTER_MODEL to a model directory to run it
// (it loads weights, so it is off by default).
func TestSinterEngine_ServesThroughTheAgent(t *testing.T) {
	dir := os.Getenv("SPROUT_GPU_SINTER_MODEL")
	if dir == "" {
		t.Skip("set SPROUT_GPU_SINTER_MODEL to a sinter model directory")
	}
	handler, name, unload, err := SinterHandler(dir)
	if err != nil {
		t.Fatalf("loading %s: %v", dir, err)
	}
	defer unload()
	h := (&Agent{State: &State{Engine: EngineSinter, Model: dir}, Local: handler}).Handler()

	rec := httptest.NewRecorder()
	h("", rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), name) {
		t.Fatalf("models = %d %s, want %s listed", rec.Code, rec.Body.String(), name)
	}

	body := `{"model":"` + name + `","max_tokens":16,"messages":[{"role":"user","content":"Say hello."}]}`
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer platform-key")
	h("", rec, req)
	out, _ := io.ReadAll(rec.Result().Body)
	if rec.Code != http.StatusOK || !strings.Contains(string(out), `"content"`) {
		t.Fatalf("chat = %d %s", rec.Code, out)
	}
	t.Logf("reply: %s", out)

	rec = httptest.NewRecorder()
	h("", rec, httptest.NewRequest(http.MethodPost, "/api/pull", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/api/pull = %d, want 404", rec.Code)
	}
}
