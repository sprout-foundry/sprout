package agent

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestCountToolsExecuted(t *testing.T) {
	eh := &ErrorHandler{}

	tests := []struct {
		name     string
		messages []api.Message
		want     int
	}{
		{
			name:     "nil messages",
			messages: nil,
			want:     0,
		},
		{
			name:     "empty messages",
			messages: []api.Message{},
			want:     0,
		},
		{
			name: "messages with no tool role",
			messages: []api.Message{
				{Role: "system", Content: "You are helpful."},
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there"},
			},
			want: 0,
		},
		{
			name: "three tool role messages",
			messages: []api.Message{
				{Role: "tool", Content: "result1", ToolCallID: "call-1"},
				{Role: "tool", Content: "result2", ToolCallID: "call-2"},
				{Role: "tool", Content: "result3", ToolCallID: "call-3"},
			},
			want: 3,
		},
		{
			name: "mixed role messages counts only tool",
			messages: []api.Message{
				{Role: "system", Content: "Be concise."},
				{Role: "user", Content: "Run ls"},
				{Role: "assistant", Content: "Running ls"},
				{Role: "tool", Content: "file1\nfile2", ToolCallID: "call-1"},
				{Role: "assistant", Content: "Here are the files..."},
				{Role: "user", Content: "Now cat file1"},
				{Role: "assistant", Content: "Reading file1"},
				{Role: "tool", Content: "contents", ToolCallID: "call-2"},
			},
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eh.countToolsExecuted(tt.messages)
			if got != tt.want {
				t.Errorf("countToolsExecuted() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFormatTokenCount_ErrorHandler(t *testing.T) {
	eh := &ErrorHandler{}

	tests := []struct {
		name   string
		tokens int
		want   string
	}{
		{name: "zero", tokens: 0, want: "0"},
		{name: "fifty", tokens: 50, want: "50"},
		{name: "nine hundred ninety nine", tokens: 999, want: "999"},
		{name: "one thousand", tokens: 1000, want: "1k"},
		{name: "one thousand five hundred", tokens: 1500, want: "1k"},
		{name: "ten thousand", tokens: 10000, want: "10k"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eh.formatTokenCount(tt.tokens)
			if got != tt.want {
				t.Errorf("formatTokenCount(%d) = %q, want %q", tt.tokens, got, tt.want)
			}
		})
	}
}

func TestClassifyError_Timeout(t *testing.T) {
	eh := &ErrorHandler{}

	tests := []struct {
		name    string
		errMsg  string
		wantSub string
	}{
		{
			name:    "timeout keyword",
			errMsg:  "request timeout after 30s",
			wantSub: "timed out",
		},
		{
			name:    "deadline exceeded",
			errMsg:  "rpc error: deadline exceeded",
			wantSub: "timed out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eh.classifyError(errMock{msg: tt.errMsg})
			if !containsStr(t, got, tt.wantSub) {
				t.Errorf("classifyError(%q) = %q, want to contain %q", tt.errMsg, got, tt.wantSub)
			}
		})
	}
}

func TestClassifyError_ModelNotFound(t *testing.T) {
	state := NewAgentStateManager(false)
	state.SetSessionModel("gpt-4-nonexistent")

	eh := &ErrorHandler{agent: &Agent{state: state}}

	tests := []struct {
		name    string
		errMsg  string
		wantSub string
	}{
		{
			name:    "model not found",
			errMsg:  "error: model gpt-4-nonexistent not found",
			wantSub: "is not available",
		},
		{
			name:    "model does not exist",
			errMsg:  "error: model does not exist",
			wantSub: "is not available",
		},
		{
			name:    "invalid model",
			errMsg:  "invalid model: foo-bar",
			wantSub: "is not available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eh.classifyError(errMock{msg: tt.errMsg})
			if !containsStr(t, got, tt.wantSub) {
				t.Errorf("classifyError(%q) = %q, want to contain %q", tt.errMsg, got, tt.wantSub)
			}
		})
	}
}

func TestClassifyError_AuthError(t *testing.T) {
	eh := &ErrorHandler{}

	tests := []struct {
		name    string
		errMsg  string
		wantSub string
	}{
		{
			name:    "401 status code",
			errMsg:  "HTTP 401: unauthorized",
			wantSub: "API key was rejected",
		},
		{
			name:    "unauthorized keyword",
			errMsg:  "request unauthorized",
			wantSub: "API key was rejected",
		},
		{
			name:    "authentication error",
			errMsg:  "authentication failed for account",
			wantSub: "API key was rejected",
		},
		{
			name:    "api key error",
			errMsg:  "invalid api key provided",
			wantSub: "API key was rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eh.classifyError(errMock{msg: tt.errMsg})
			if !containsStr(t, got, tt.wantSub) {
				t.Errorf("classifyError(%q) = %q, want to contain %q", tt.errMsg, got, tt.wantSub)
			}
		})
	}
}

func TestClassifyError_ContextWindow(t *testing.T) {
	eh := &ErrorHandler{}

	tests := []struct {
		name    string
		errMsg  string
		wantSub string
	}{
		{
			name:    "context window",
			errMsg:  "context window exceeds limit",
			wantSub: "exceeded the model context window",
		},
		{
			name:    "available context size",
			errMsg:  "exceeds available context size",
			wantSub: "exceeded the model context window",
		},
		{
			name:    "exceed_context_size_error",
			errMsg:  "exceed_context_size_error: max tokens",
			wantSub: "exceeded the model context window",
		},
		{
			name:    "maximum context length",
			errMsg:  "maximum context length reached",
			wantSub: "exceeded the model context window",
		},
		{
			name:    "max context",
			errMsg:  "max context tokens exceeded",
			wantSub: "exceeded the model context window",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eh.classifyError(errMock{msg: tt.errMsg})
			if !containsStr(t, got, tt.wantSub) {
				t.Errorf("classifyError(%q) = %q, want to contain %q", tt.errMsg, got, tt.wantSub)
			}
		})
	}
}

func TestClassifyError_GenericError(t *testing.T) {
	eh := &ErrorHandler{}

	tests := []struct {
		name    string
		errMsg  string
		wantSub string
	}{
		{
			name:    "generic api error",
			errMsg:  "internal server error: something went wrong",
			wantSub: "API error:",
		},
		{
			name:    "connection refused",
			errMsg:  "dial tcp: connection refused",
			wantSub: "API error:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eh.classifyError(errMock{msg: tt.errMsg})
			if !containsStr(t, got, tt.wantSub) {
				t.Errorf("classifyError(%q) = %q, want to contain %q", tt.errMsg, got, tt.wantSub)
			}
			if !containsStr(t, got, tt.errMsg) {
				t.Errorf("classifyError(%q) = %q, want error message in output", tt.errMsg, got)
			}
		})
	}
}

// errMock is a minimal error implementation for testing classifyError.
type errMock struct {
	msg string
}

func (e errMock) Error() string { return e.msg }

// containsStr checks if s contains substr.
func containsStr(t *testing.T, s, substr string) bool {
	t.Helper()
	return strings.Contains(s, substr)
}
