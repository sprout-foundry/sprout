package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// visionLearnedTestEnv isolates the capability cache file per test and
// satisfies the bearer-auth env check.
func visionLearnedTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())
	t.Setenv("VISION_TEST_KEY", "test-key")
}

func chatServer(t *testing.T, statusFirstCall int, imageErrors bool) *httptest.Server {
	t.Helper()
	calls := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		hadImages := containsBytes(body, []byte("image_url"))
		if imageErrors && hadImages && calls == 1 {
			w.WriteHeader(statusFirstCall)
			_, _ = w.Write([]byte(`{"error":{"message":"image input is not supported by this model"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"estimated_cost":0}}`))
	}))
}

func containsBytes(haystack, needle []byte) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func imageMessages() []api.Message {
	return []api.Message{
		{Role: "user", Content: "describe", Images: []api.ImageData{{Base64: "aGk=", Type: "image/png"}}},
	}
}

func TestReconcileVisionCapability_SuccessRecordsPositive(t *testing.T) {
	visionLearnedTestEnv(t)
	server := chatServer(t, 0, false)
	defer server.Close()

	config := visionTestConfig()
	config.Endpoint = server.URL
	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	if _, err := provider.SendChatRequest(context.Background(), imageMessages(), nil, "", false); err != nil {
		t.Fatalf("chat: %v", err)
	}

	if learned := api.LearnedVisionAcceptance(provider.GetProvider(), provider.GetModel()); learned == nil || !*learned {
		t.Fatalf("expected positive learning, got %v", learned)
	}
}

func TestReconcileVisionCapability_RejectionRecordsNegativeAndRetriesTextOnly(t *testing.T) {
	visionLearnedTestEnv(t)
	server := chatServer(t, http.StatusBadRequest, true)
	defer server.Close()

	config := visionTestConfig()
	config.Endpoint = server.URL
	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	resp, err := provider.SendChatRequest(context.Background(), imageMessages(), nil, "", false)
	if err != nil {
		t.Fatalf("expected text-only retry to complete the turn, got error: %v", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "ok" {
		t.Fatalf("expected retried response, got %+v", resp)
	}

	if learned := api.LearnedVisionAcceptance(provider.GetProvider(), provider.GetModel()); learned == nil || *learned {
		t.Fatalf("expected negative learning after rejection, got %v", learned)
	}
}

func TestReconcileVisionCapability_TransientErrorLearnsNothing(t *testing.T) {
	visionLearnedTestEnv(t)

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"temporarily unavailable"}}`))
	}))
	defer server.Close()

	config := visionTestConfig()
	config.Endpoint = server.URL
	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	if _, err := provider.SendChatRequest(context.Background(), imageMessages(), nil, "", false); err == nil {
		t.Fatal("expected the 503 to surface")
	}
	if calls != 1 {
		t.Fatalf("transient errors must not trigger a retry, got %d calls", calls)
	}
	if learned := api.LearnedVisionAcceptance(provider.GetProvider(), provider.GetModel()); learned != nil {
		t.Fatalf("transient errors must not be recorded, got %v", *learned)
	}
}

func TestResolveVisionCapability_LearnedOverridesDeclaration(t *testing.T) {
	visionLearnedTestEnv(t)

	provider, err := NewGenericProvider(visionTestConfig())
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if !provider.SupportsVision() {
		t.Fatal("precondition: declared vision true")
	}
	// "vision-test" provider is not in the registry catalog, so probe is
	// nil and the learned layer decides the resolver's answer.
	api.RecordVisionAcceptance(provider.GetProvider(), provider.GetModel(), false)

	capability := api.ResolveVisionCapability(provider)
	if capability.AcceptsImages {
		t.Fatal("learned false must override declared true")
	}
	if capability.Source != "runtime" {
		t.Fatalf("source should be runtime, got %q", capability.Source)
	}
}
