package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// usageServer answers chat requests: it refuses stream_options when refuse is
// set, streams a reply, and sends a usage block only when asked for one.
func usageServer(t *testing.T, refuse bool) (*httptest.Server, *[]string, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	bodies := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		asked := strings.Contains(body, `"stream_options"`)
		if refuse && asked {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"Unrecognized request argument supplied: stream_options"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello there, this is the answer.\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		if asked {
			fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":123,\"completion_tokens\":7,\"total_tokens\":130}}\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies, &mu
}

func usageProvider(t *testing.T, endpoint string, includeUsage *bool) *GenericProvider {
	t.Helper()
	p, err := NewGenericProvider(&ProviderConfig{
		Name:      "usage-test",
		Endpoint:  endpoint,
		Auth:      AuthConfig{Type: "none"},
		Defaults:  RequestDefaults{Model: "test-model"},
		Models:    ModelConfig{DefaultContextLimit: 64000},
		Streaming: StreamingConfig{IncludeUsage: includeUsage},
	})
	if err != nil {
		t.Fatalf("NewGenericProvider: %v", err)
	}
	return p
}

func streamOnce(t *testing.T, p *GenericProvider) *api.ChatResponse {
	t.Helper()
	resp, err := p.SendChatRequestStream(context.Background(),
		[]api.Message{{Role: "user", Content: "Please answer the question about the weather today."}}, nil, "", false, func(string, string) {})
	if err != nil {
		t.Fatalf("SendChatRequestStream: %v", err)
	}
	return resp
}

// A server that follows the strict spec reports usage because it is asked.
func TestStreamAsksForUsageByDefault(t *testing.T) {
	srv, bodies, _ := usageServer(t, false)
	resp := streamOnce(t, usageProvider(t, srv.URL, nil))
	if resp.Usage.PromptTokens != 123 || resp.Usage.CompletionTokens != 7 {
		t.Fatalf("usage = %+v, want the server's 123/7", resp.Usage)
	}
	if !strings.Contains((*bodies)[0], `"include_usage":true`) {
		t.Fatalf("request did not ask for usage: %s", (*bodies)[0])
	}
}

// A server that refuses stream_options is retried without it once, not asked
// again, and its counts are estimated rather than zero.
func TestStreamRetriesWithoutStreamOptionsAndEstimates(t *testing.T) {
	srv, bodies, mu := usageServer(t, true)
	p := usageProvider(t, srv.URL, nil)
	resp := streamOnce(t, p)
	if resp.Choices[0].Message.Content != "Hello there, this is the answer." {
		t.Fatalf("content = %q", resp.Choices[0].Message.Content)
	}
	if resp.Usage.PromptTokens == 0 || resp.Usage.CompletionTokens == 0 || resp.Usage.TotalTokens != resp.Usage.PromptTokens+resp.Usage.CompletionTokens {
		t.Fatalf("usage = %+v, want an estimate, not zero", resp.Usage)
	}
	mu.Lock()
	first := len(*bodies)
	mu.Unlock()
	if first != 2 {
		t.Fatalf("requests = %d, want the refused one and its retry", first)
	}
	streamOnce(t, p)
	mu.Lock()
	defer mu.Unlock()
	if len(*bodies) != 3 || strings.Contains((*bodies)[2], "stream_options") {
		t.Fatalf("a later request must leave stream_options out at once: %d requests, last %s", len(*bodies), (*bodies)[len(*bodies)-1])
	}
}

// A provider configured with include_usage false is not asked, and its
// counts are estimated.
func TestStreamIncludeUsageFalseOptsOut(t *testing.T) {
	srv, bodies, _ := usageServer(t, false)
	off := false
	resp := streamOnce(t, usageProvider(t, srv.URL, &off))
	if strings.Contains((*bodies)[0], "stream_options") {
		t.Fatalf("opted-out provider was asked for usage: %s", (*bodies)[0])
	}
	if resp.Usage.PromptTokens == 0 {
		t.Fatalf("usage = %+v, want an estimate", resp.Usage)
	}
}

func TestRejectsStreamOptions(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   bool
	}{
		{400, `Unrecognized request argument supplied: stream_options`, true},
		{422, `{"detail":"extra field: include_usage"}`, true},
		{400, `max_tokens is too large`, false},
		{500, `stream_options broke us`, false},
	} {
		if got := rejectsStreamOptions(tc.status, []byte(tc.body)); got != tc.want {
			t.Errorf("rejectsStreamOptions(%d, %q) = %v, want %v", tc.status, tc.body, got, tc.want)
		}
	}
}
