// Package agent_audit defines the facts-only per-call audit events the agent
// emits for every model call and tool execution, and the process-wide sink
// that records them. Events carry digests and metadata only — never prompt
// text, response text, message content, or raw tool arguments — so a host can
// reconstruct what the agent did without storing what it said.
//
// The package is deliberately dependency-free (standard library only) so both
// the agent package and the provider package can import it without a cycle:
// the agent owns the sink and the call context, the provider emits the model
// call, and neither imports the other for auditing.
package agent_audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"time"
)

// Event kinds, written to the Kind field so a reader can discriminate a model
// call from a tool execution in a single JSONL stream.
const (
	KindModelCall = "model_call"
	KindToolCall  = "tool_call"
)

// Call outcomes.
const (
	OutcomeOK       = "ok"
	OutcomeError    = "error"
	OutcomeFailover = "failover"
)

// Triggers describe what caused a model call.
const (
	TriggerUserTurn     = "user_turn"
	TriggerToolFollowUp = "tool_call_follow_up"
	TriggerSubagent     = "subagent"
)

// CallEvent is one model call the agent made. It records the wire facts of the
// request and response (digests and byte counts), the token usage, the
// outcome, and the trigger. It never carries request or response content.
type CallEvent struct {
	Time             time.Time `json:"time"`
	Kind             string    `json:"kind"`
	ChatID           string    `json:"chat_id,omitempty"`
	SessionID        string    `json:"session_id,omitempty"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	EndpointHost     string    `json:"endpoint_host,omitempty"`
	RequestSHA256    string    `json:"request_sha256,omitempty"`
	RequestBytes     int       `json:"request_bytes"`
	ResponseSHA256   string    `json:"response_sha256,omitempty"`
	ResponseBytes    int       `json:"response_bytes"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	Outcome          string    `json:"outcome"`
	Trigger          string    `json:"trigger"`
	Streaming        bool      `json:"streaming,omitempty"`
	Failover         bool      `json:"failover,omitempty"`
}

// ToolEvent is one tool execution. It records the tool name, a digest of the
// arguments, the result status, and the file paths the call declared. It never
// carries the raw arguments or the tool output.
type ToolEvent struct {
	Time         time.Time `json:"time"`
	Kind         string    `json:"kind"`
	ChatID       string    `json:"chat_id,omitempty"`
	SessionID    string    `json:"session_id,omitempty"`
	Tool         string    `json:"tool"`
	ArgsSHA256   string    `json:"args_sha256,omitempty"`
	ArgsBytes    int       `json:"args_bytes"`
	Status       string    `json:"status"`
	FilesTouched []string  `json:"files_touched,omitempty"`
	Trigger      string    `json:"trigger,omitempty"`
}

// Sink records audit events. The agent installs one process-wide sink at
// startup; it fans events out to the local audit log and, when a host
// configures an audit endpoint, to that endpoint in batches.
type Sink interface {
	EmitCall(CallEvent)
	EmitTool(ToolEvent)
}

// callContextKey is the context key type for per-call audit metadata.
type callContextKey struct{}

// CallContext is the per-call audit metadata the agent attaches to the
// context it hands the provider. It travels with the context rather than the
// process-wide sink so concurrent agents (subagents, multi-chat daemons) never
// clobber each other's session identity.
type CallContext struct {
	ChatID    string
	SessionID string
	Subagent  bool
}

// WithCallContext attaches audit metadata to ctx.
func WithCallContext(ctx context.Context, cc CallContext) context.Context {
	return context.WithValue(ctx, callContextKey{}, cc)
}

// CallContextFrom reads the audit metadata attached to ctx.
func CallContextFrom(ctx context.Context) (CallContext, bool) {
	if ctx == nil {
		return CallContext{}, false
	}
	cc, ok := ctx.Value(callContextKey{}).(CallContext)
	return cc, ok
}

// ResolveTrigger derives the trigger for a model call. A subagent's calls are
// always subagent-triggered; otherwise a request whose last message is a tool
// result is a tool-call follow-up, and anything else is a user turn.
func ResolveTrigger(ctx context.Context, lastMessageRole string) string {
	if cc, ok := CallContextFrom(ctx); ok && cc.Subagent {
		return TriggerSubagent
	}
	if strings.EqualFold(strings.TrimSpace(lastMessageRole), "tool") {
		return TriggerToolFollowUp
	}
	return TriggerUserTurn
}

// Digest returns the lowercase hex SHA-256 of b and its byte length.
func Digest(b []byte) (string, int) {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), len(b)
}

// EndpointHost returns the host (host:port) of an endpoint URL. When the
// endpoint does not parse as a URL it falls back to the substring before the
// first '/', so a path is never returned. Credentials (userinfo) are never
// returned either.
func EndpointHost(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	u, err := url.Parse(endpoint)
	if err == nil && u.Host != "" {
		return u.Host
	}
	if i := strings.IndexByte(endpoint, '/'); i >= 0 {
		return endpoint[:i]
	}
	return endpoint
}
