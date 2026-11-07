package providers

import (
	"encoding/json"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// buildChatRequest adds stream_options.include_usage to STREAMING requests
// unless the provider config opts out or the server refused it, and never to
// non-streaming requests. Strict-OpenAI backends (vLLM, SGLang, llama.cpp
// server) omit the usage block from SSE streams unless the client asks for
// it — without the flag every response would track as zero tokens.
func TestBuildChatRequestStreamOptionsIncludeUsage(t *testing.T) {
	off := false
	cases := []struct {
		name        string
		stream      bool
		includeUsg  *bool
		refused     bool
		wantPresent bool
	}{
		{"streaming asks for usage by default", true, nil, false, true},
		{"streaming with include_usage false omits stream_options", true, &off, false, false},
		{"streaming after the server refused it omits stream_options", true, nil, true, false},
		{"non-streaming never sends stream_options", false, nil, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &GenericProvider{
				config: &ProviderConfig{
					Name:     "stream-opts-test",
					Endpoint: "http://localhost:9/v1/chat/completions",
					Auth:     AuthConfig{Type: "bearer"},
				},
			}
			p.config.Streaming.IncludeUsage = tc.includeUsg
			p.streamUsageRefused.Store(tc.refused)
			// Stub the model so ensureModel's resolution is not needed.
			p.mu.Lock()
			p.model = "test-model"
			p.mu.Unlock()

			body, err := p.buildChatRequest(
				[]api.Message{{Role: "user", Content: "hi"}},
				nil, "", false, tc.stream,
			)
			if err != nil {
				t.Fatalf("buildChatRequest: %v", err)
			}

			var decoded map[string]interface{}
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatalf("decode request body: %v", err)
			}

			raw, present := decoded["stream_options"]
			if tc.wantPresent {
				if !present {
					t.Fatalf("stream_options missing from streaming request; body=%s", truncate(body))
				}
				so, ok := raw.(map[string]interface{})
				if !ok {
					t.Fatalf("stream_options is not an object: %T", raw)
				}
				if so["include_usage"] != true {
					t.Fatalf("stream_options.include_usage = %v, want true", so["include_usage"])
				}
			} else if present {
				t.Fatalf("stream_options unexpectedly present; body=%s", truncate(body))
			}

			// Sanity: the stream flag itself must round-trip.
			if decoded["stream"] != tc.stream {
				t.Fatalf("stream = %v, want %v", decoded["stream"], tc.stream)
			}
			// Non-streaming requests must never carry a stream_options key
			// (strict servers reject unknown/conditional fields in some
			// configurations; the flag is only meaningful with stream=true).
			if !tc.stream && strings.Contains(string(body), "stream_options") {
				t.Fatalf("non-streaming body contains stream_options: %s", truncate(body))
			}
		})
	}
}

func truncate(b []byte) string {
	const max = 400
	s := string(b)
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
