package export

// session_export.go — the session-export core: the source / options /
// format types, the Export / ExportMarkdown / ExportHTML / ExportJSON
// dispatchers, the shared turn-grouping + timezone / redaction helpers, and
// the Markdown / HTML printer types live in session_export_md.go and
// session_export_html.go.

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/sprout-foundry/sprout/pkg/secretdetect"
)

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------
// ExportOptions controls an export operation.
type ExportOptions struct {
	IncludeToolCalls bool           // include tool calls/results blocks in the output
	IncludeCost      bool           // include cost/tokens in the rendered output
	RedactSecrets    bool           // apply pkg/secretdetect redaction before rendering (default true)
	PrettyPrintJSON  bool           // pretty-print JSON output (default true)
	Timezone         *time.Location // for date formatting; nil = UTC
}

// ExportFormat selects an output format.
type ExportFormat string

const (
	FormatMarkdown ExportFormat = "markdown"
	FormatHTML     ExportFormat = "html"
	FormatJSON     ExportFormat = "json"
)

// SessionSource is the minimal data needed to export a session.
type SessionSource struct {
	ID               string          `json:"session_id"`
	Name             string          `json:"name"`
	WorkingDirectory string          `json:"working_directory"`
	StartedAt        time.Time       `json:"started_at"`
	LastUpdated      time.Time       `json:"last_updated"`
	Provider         string          `json:"provider"`
	Model            string          `json:"model"`
	TotalCost        float64         `json:"total_cost"`
	InputTokens      int             `json:"input_tokens"`
	OutputTokens     int             `json:"output_tokens"`
	Messages         []MessageSource `json:"messages"`
}

// MessageSource is a single message in the session.
type MessageSource struct {
	Role       string // "user" | "assistant" | "system" | "tool"
	Content    string
	Timestamp  time.Time
	ToolCalls  []ToolCallSource  // optional, for assistant messages
	ToolResult *ToolResultSource // optional, set if Role=="tool"
	Cost       float64           // optional, per-message cost
	Tokens     int               // optional, per-message tokens
}

// ToolCallSource records a single tool invocation by the assistant.
type ToolCallSource struct {
	Name      string
	Arguments string // raw JSON or string
	Result    string // raw JSON or string
	Timestamp time.Time
}

// ToolResultSource records the result of a tool call (role=="tool").
type ToolResultSource struct {
	ToolCallName string
	Content      string
	Timestamp    time.Time
}

// ---------------------------------------------------------------------------
// Export functions
// ---------------------------------------------------------------------------

// Export dispatches to the appropriate format-specific exporter.
func Export(w io.Writer, s SessionSource, format ExportFormat, opts ExportOptions) error {
	switch format {
	case FormatMarkdown:
		return ExportMarkdown(w, s, opts)
	case FormatHTML:
		return ExportHTML(w, s, opts)
	case FormatJSON:
		return ExportJSON(w, s, opts)
	default:
		return fmt.Errorf("unsupported export format: %s", format)
	}
}

// ExportMarkdown writes a Markdown rendering of s to w.
func ExportMarkdown(w io.Writer, s SessionSource, opts ExportOptions) error {
	loc := opts.resolveTimezone()
	redact := opts.RedactSecrets

	p := &mdPrinter{w: w, s: s, opts: opts, loc: loc}

	// Front-matter
	if err := p.writeFrontMatter(); err != nil {
		return fmt.Errorf("write front-matter: %w", err)
	}

	// Title
	if err := p.writeTitle(); err != nil {
		return fmt.Errorf("write title: %w", err)
	}

	// Summary blockquote
	if err := p.writeSummary(); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}

	// Table of contents
	if err := p.writeTOC(); err != nil {
		return fmt.Errorf("write table of contents: %w", err)
	}

	// Turns
	if err := p.writeTurns(redact); err != nil {
		return fmt.Errorf("write turns: %w", err)
	}

	return nil
}

// ExportHTML writes a self-contained HTML document with embedded CSS.
func ExportHTML(w io.Writer, s SessionSource, opts ExportOptions) error {
	loc := opts.resolveTimezone()
	redact := opts.RedactSecrets

	p := &htmlPrinter{w: w, s: s, opts: opts, loc: loc}

	if err := p.writeDocType(); err != nil {
		return fmt.Errorf("write doctype: %w", err)
	}
	if err := p.writeHead(); err != nil {
		return fmt.Errorf("write head: %w", err)
	}
	if err := p.writeBodyStart(); err != nil {
		return fmt.Errorf("write body start: %w", err)
	}
	if err := p.writeMetadata(); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	if err := p.writeTurns(redact); err != nil {
		return fmt.Errorf("write turns: %w", err)
	}
	if err := p.writeBodyEnd(); err != nil {
		return fmt.Errorf("write body end: %w", err)
	}
	return nil
}

// ExportJSON writes a lossless JSON encoding of s.
func ExportJSON(w io.Writer, s SessionSource, opts ExportOptions) error {
	var data []byte
	var err error

	if opts.RedactSecrets {
		// Deep-copy and redact all string content before serializing.
		s = redactSession(s)
	}

	if opts.PrettyPrintJSON {
		data, err = json.MarshalIndent(s, "", "  ")
	} else {
		data, err = json.Marshal(s)
	}
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}

	// Trailing newline for POSIX text files.
	data = append(data, '\n')

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers shared across renderers
// ---------------------------------------------------------------------------

func (o ExportOptions) resolveTimezone() *time.Location {
	if o.Timezone != nil {
		return o.Timezone
	}
	return time.UTC
}

// countTurns returns the number of logical turns (matching what groupTurns produces).
func countTurns(msgs []MessageSource) int {
	return len(groupTurns(msgs))
}

// uniqueToolNames returns the sorted set of distinct tool names across
// all messages' tool calls.
func uniqueToolNames(msgs []MessageSource) []string {
	seen := make(map[string]bool)
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			if tc.Name != "" {
				seen[tc.Name] = true
			}
		}
		if m.ToolResult != nil && m.ToolResult.ToolCallName != "" {
			seen[m.ToolResult.ToolCallName] = true
		}
	}
	if len(seen) == 0 {
		return nil
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	// Deterministic sort.
	sort.Strings(names)
	return names
}

// groupTurns pairs consecutive user/assistant messages (plus any
// interstitial tool messages) into logical turns.
//
// Each element is a slice of MessageSource representing one turn:
// typically [user, assistant] or [user, tool, assistant].
func groupTurns(msgs []MessageSource) [][]MessageSource {
	if len(msgs) == 0 {
		return nil
	}

	var turns [][]MessageSource

	// Find user message indices.
	userIndices := make([]int, 0)
	for i, m := range msgs {
		if m.Role == "user" {
			userIndices = append(userIndices, i)
		}
	}

	// Build turns: each turn starts at a user message and includes everything
	// until the next user message (or the end).
	for i, ui := range userIndices {
		end := len(msgs)
		if i+1 < len(userIndices) {
			end = userIndices[i+1]
		}
		turn := make([]MessageSource, end-ui)
		copy(turn, msgs[ui:end])
		turns = append(turns, turn)
	}

	return turns
}

// ---------------------------------------------------------------------------
// Markdown renderer
// ---------------------------------------------------------------------------

// redactSession creates a deep copy of s with all string fields redacted.
func redactSession(s SessionSource) SessionSource {
	s2 := s // shallow copy of the struct
	// Redact top-level string fields.
	if s.WorkingDirectory != "" {
		s2.WorkingDirectory = secretdetect.RedactOpaque(s.WorkingDirectory)
	}

	// Deep copy and redact messages.
	msgs := make([]MessageSource, len(s.Messages))
	for i, m := range s.Messages {
		msgs[i] = MessageSource{
			Role:      m.Role,
			Content:   secretdetect.RedactOpaque(m.Content),
			Timestamp: m.Timestamp,
			Cost:      m.Cost,
			Tokens:    m.Tokens,
		}
		if len(m.ToolCalls) > 0 {
			tcs := make([]ToolCallSource, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				tcs[j] = ToolCallSource{
					Name:      tc.Name,
					Arguments: secretdetect.RedactOpaque(tc.Arguments),
					Result:    secretdetect.RedactOpaque(tc.Result),
					Timestamp: tc.Timestamp,
				}
			}
			msgs[i].ToolCalls = tcs
		}
		if m.ToolResult != nil {
			tr := *m.ToolResult
			tr.Content = secretdetect.RedactOpaque(m.ToolResult.Content)
			msgs[i].ToolResult = &tr
		}
	}
	s2.Messages = msgs
	return s2
}
