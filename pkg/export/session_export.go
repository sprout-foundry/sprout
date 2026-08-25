// Package export provides functions to render Sprout session data as
// Markdown, HTML, or JSON documents.  All three formats accept a
// SessionSource (the minimal structured view of a session) and an
// ExportOptions block to control what gets included.
//
// Content is optionally redacted through pkg/secretdetect before any
// rendering happens so that secrets never leak into exported files.
package export

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"sort"
	"strings"
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

type mdPrinter struct {
	w    io.Writer
	s    SessionSource
	opts ExportOptions
	loc  *time.Location
}

func (p *mdPrinter) writeFrontMatter() error {
	totalTokens := p.s.InputTokens + p.s.OutputTokens
	turns := countTurns(p.s.Messages)
	tools := uniqueToolNames(p.s.Messages)

	fm := "---\n"
	fm += fmt.Sprintf("session_id: %s\n", p.s.ID)
	fm += fmt.Sprintf("name: %s\n", p.s.Name)
	fm += fmt.Sprintf("working_directory: %s\n", p.s.WorkingDirectory)
	fm += fmt.Sprintf("last_updated: %s\n", p.s.LastUpdated.In(p.loc).Format(time.RFC3339))
	fm += fmt.Sprintf("total_cost: %.4f\n", p.s.TotalCost)
	fm += fmt.Sprintf("total_tokens: %d\n", totalTokens)
	fm += fmt.Sprintf("turns: %d\n", turns)
	if len(tools) > 0 {
		fm += "tools_used:\n"
		for _, t := range tools {
			fm += fmt.Sprintf("  - %s\n", t)
		}
	}
	fm += "---\n"

	_, err := p.w.Write([]byte(fm))
	return err
}

func (p *mdPrinter) writeTitle() error {
	name := p.s.Name
	if name == "" {
		name = "Sprout Session"
	}
	_, err := fmt.Fprintf(p.w, "# %s\n\n", name)
	return err
}

func (p *mdPrinter) writeSummary() error {
	date := p.s.StartedAt.In(p.loc).Format("January 2, 2006")
	turns := countTurns(p.s.Messages)
	totalTokens := p.s.InputTokens + p.s.OutputTokens

	_, err := fmt.Fprintf(p.w, "> Session started %s. %d turns, %d tokens, $%.4f.\n\n",
		date, turns, totalTokens, p.s.TotalCost)
	return err
}

func (p *mdPrinter) writeTOC() error {
	turns := groupTurns(p.s.Messages)
	if len(turns) == 0 {
		return nil
	}

	_, err := fmt.Fprintf(p.w, "## Table of Contents\n\n")
	if err != nil {
		return err
	}

	for i, turn := range turns {
		ts := turn[0].Timestamp.In(p.loc).Format("15:04:05")
		anchor := fmt.Sprintf("turn-%d", i+1)
		_, err := fmt.Fprintf(p.w, "- [Turn %d — %s](#%s)\n", i+1, ts, anchor)
		if err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(p.w)
	return err
}

func (p *mdPrinter) writeTurns(redact bool) error {
	turns := groupTurns(p.s.Messages)
	for i, turn := range turns {
		if err := p.writeTurn(i+1, turn, redact); err != nil {
			return err
		}
	}
	return nil
}

func (p *mdPrinter) writeTurn(num int, msgs []MessageSource, redact bool) error {
	ts := msgs[0].Timestamp.In(p.loc).Format(time.RFC3339)
	if _, err := fmt.Fprintf(p.w, "\n---\n\n## Turn %d — %s\n\n", num, ts); err != nil {
		return err
	}

	for _, m := range msgs {
		if err := p.writeMessage(m, redact); err != nil {
			return err
		}
	}

	if p.opts.IncludeCost {
		return p.writeTurnFooter(msgs)
	}
	return nil
}

func (p *mdPrinter) writeMessage(m MessageSource, redact bool) error {
	content := m.Content
	if redact {
		content = secretdetect.RedactOpaque(content)
	}

	switch m.Role {
	case "user":
		if _, err := fmt.Fprintf(p.w, "**User:**\n\n> %s\n\n", escapeMDQuotes(content)); err != nil {
			return err
		}
	case "assistant":
		if _, err := fmt.Fprintf(p.w, "**Assistant:**\n\n%s\n\n", content); err != nil {
			return err
		}
		// Tool calls
		if p.opts.IncludeToolCalls {
			for _, tc := range m.ToolCalls {
				tcArgs := tc.Arguments
				if redact {
					tcArgs = secretdetect.RedactOpaque(tcArgs)
				}
				if err := p.writeToolCall(tc); err != nil {
					return err
				}
			}
		}
	case "tool":
		if p.opts.IncludeToolCalls {
			if err := p.writeToolResult(m, redact); err != nil {
				return err
			}
		}
	case "system":
		if _, err := fmt.Fprintf(p.w, "**System:**\n\n%s\n\n", content); err != nil {
			return err
		}
	default:
		if _, err := fmt.Fprintf(p.w, "**%s:**\n\n%s\n\n", m.Role, content); err != nil {
			return err
		}
	}
	return nil
}

func (p *mdPrinter) writeToolCall(tc ToolCallSource) error {
	if _, err := fmt.Fprintf(p.w, "<details>\n<summary>Tool call: <code>%s</code></summary>\n\n```json\n%s\n```\n\n</details>\n\n",
		tc.Name, tc.Arguments); err != nil {
		return err
	}
	return nil
}

func (p *mdPrinter) writeToolResult(m MessageSource, redact bool) error {
	name := "tool"
	if m.ToolResult != nil {
		name = m.ToolResult.ToolCallName
	}
	content := m.Content
	if m.ToolResult != nil {
		content = m.ToolResult.Content
	}
	if redact {
		content = secretdetect.RedactOpaque(content)
	}
	if _, err := fmt.Fprintf(p.w, "<details>\n<summary>Tool result: <code>%s</code></summary>\n\n%s\n\n</details>\n\n",
		name, content); err != nil {
		return err
	}
	return nil
}

func (p *mdPrinter) writeTurnFooter(msgs []MessageSource) error {
	var totalCost float64
	var totalTokens int
	for _, m := range msgs {
		totalCost += m.Cost
		totalTokens += m.Tokens
	}
	_, err := fmt.Fprintf(p.w, "*Cost: $%.4f | Tokens: %d*\n\n", totalCost, totalTokens)
	return err
}

// escapeMDQuotes replaces characters that could break markdown quoting.
// Wraps content in a blockquote-safe manner.
func escapeMDQuotes(content string) string {
	// Ensure each line starts with space for proper blockquote rendering.
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if line != "" && !strings.HasPrefix(line, " ") {
			lines[i] = " " + line
		}
	}
	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------------------
// HTML renderer
// ---------------------------------------------------------------------------

type htmlPrinter struct {
	w    io.Writer
	s    SessionSource
	opts ExportOptions
	loc  *time.Location
}

func (p *htmlPrinter) writeDocType() error {
	_, err := io.WriteString(p.w, "<!DOCTYPE html>\n<html lang=\"en\">\n")
	return err
}

func (p *htmlPrinter) writeHead() error {
	var sb strings.Builder
	sb.WriteString("<head>\n")
	sb.WriteString("  <meta charset=\"utf-8\">\n")
	sb.WriteString("  <meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")

	name := p.s.Name
	if name == "" {
		name = "Sprout Session"
	}
	sb.WriteString(fmt.Sprintf("  <title>%s — Sprout Session</title>\n", html.EscapeString(name)))

	sb.WriteString("  <style>\n")
	sb.WriteString("    *, *::before, *::after { box-sizing: border-box; }\n")
	sb.WriteString("    body {\n")
	sb.WriteString("      font-family: -apple-system, BlinkMacSystemFont, \"Segoe UI\", Roboto, Helvetica, Arial, sans-serif;\n")
	sb.WriteString("      max-width: 800px;\n")
	sb.WriteString("      margin: 2rem auto;\n")
	sb.WriteString("      padding: 0 1rem;\n")
	sb.WriteString("      line-height: 1.6;\n")
	sb.WriteString("      color: #1a1a1a;\n")
	sb.WriteString("      background: #fff;\n")
	sb.WriteString("    }\n")
	sb.WriteString("    h1 { border-bottom: 2px solid #e5e5e5; padding-bottom: 0.5rem; }\n")
	sb.WriteString("    h2 { border-bottom: 1px solid #eee; padding-bottom: 0.3rem; margin-top: 2rem; }\n")
	sb.WriteString("    .metadata { margin: 1.5rem 0; }\n")
	sb.WriteString("    .metadata table { border-collapse: collapse; width: 100%; }\n")
	sb.WriteString("    .metadata th, .metadata td { text-align: left; padding: 0.4rem 0.8rem; border-bottom: 1px solid #eee; }\n")
	sb.WriteString("    .metadata th { width: 160px; color: #666; font-weight: 600; }\n")
	sb.WriteString("    .summary { font-style: italic; color: #555; margin: 1rem 0; padding: 0.5rem 1rem; border-left: 3px solid #ddd; background: #f9f9f9; }\n")
	sb.WriteString("    .toc { margin: 1.5rem 0; padding: 0; list-style: none; }\n")
	sb.WriteString("    .toc li { padding: 0.2rem 0; }\n")
	sb.WriteString("    .toc a { text-decoration: none; color: #0066cc; }\n")
	sb.WriteString("    .turn { margin: 2rem 0; padding: 1rem; border: 1px solid #e5e5e5; border-radius: 6px; }\n")
	sb.WriteString("    .role { font-weight: 600; display: inline-block; min-width: 90px; }\n")
	sb.WriteString("    .role-user { color: #0066cc; }\n")
	sb.WriteString("    .role-assistant { color: #2e7d32; }\n")
	sb.WriteString("    .role-tool { color: #e65100; }\n")
	sb.WriteString("    .role-system { color: #6a1b9a; }\n")
	sb.WriteString("    .content { white-space: pre-wrap; margin: 0.5rem 0; }\n")
	sb.WriteString("    code, pre { font-family: \"SFMono-Regular\", Consolas, \"Liberation Mono\", Menlo, monospace; font-size: 0.9em; }\n")
	sb.WriteString("    pre { background: #f5f5f5; padding: 1rem; border-radius: 4px; overflow-x: auto; }\n")
	sb.WriteString("    pre code { background: none; padding: 0; }\n")
	sb.WriteString("    details { margin: 0.5rem 0; padding: 0.5rem; background: #f9f9f9; border-radius: 4px; border: 1px solid #eee; }\n")
	sb.WriteString("    summary { cursor: pointer; font-weight: 600; }\n")
	sb.WriteString("    .footer { font-size: 0.85em; color: #888; margin-top: 0.5rem; }\n")
	sb.WriteString("    a { color: #0066cc; }\n")
	sb.WriteString("    @media (prefers-color-scheme: dark) {\n")
	sb.WriteString("      body { background: #1e1e1e; color: #d4d4d4; }\n")
	sb.WriteString("      h1, h2 { border-color: #444; }\n")
	sb.WriteString("      .summary { background: #2a2a2a; border-left-color: #555; color: #bbb; }\n")
	sb.WriteString("      .metadata th { color: #aaa; }\n")
	sb.WriteString("      .metadata td, .metadata th { border-color: #333; }\n")
	sb.WriteString("      .turn { border-color: #444; background: #252525; }\n")
	sb.WriteString("      pre { background: #2a2a2a; }\n")
	sb.WriteString("      details { background: #2a2a2a; border-color: #444; }\n")
	sb.WriteString("      a { color: #6ab3ff; }\n")
	sb.WriteString("      .role-user { color: #6ab3ff; }\n")
	sb.WriteString("      .role-assistant { color: #81c784; }\n")
	sb.WriteString("      .role-tool { color: #ff8a65; }\n")
	sb.WriteString("      .role-system { color: #ce93d8; }\n")
	sb.WriteString("      .footer { color: #888; }\n")
	sb.WriteString("    }\n")
	sb.WriteString("  </style>\n")
	sb.WriteString("</head>\n")

	_, err := p.w.Write([]byte(sb.String()))
	return err
}

func (p *htmlPrinter) writeBodyStart() error {
	_, err := io.WriteString(p.w, "<body>\n")
	return err
}

func (p *htmlPrinter) writeMetadata() error {
	name := p.s.Name
	if name == "" {
		name = "Sprout Session"
	}
	date := p.s.StartedAt.In(p.loc).Format("January 2, 2006")
	turns := countTurns(p.s.Messages)
	totalTokens := p.s.InputTokens + p.s.OutputTokens
	tools := uniqueToolNames(p.s.Messages)
	toolList := strings.Join(tools, ", ")
	if toolList == "" {
		toolList = "none"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<h1>%s</h1>\n", html.EscapeString(name)))
	sb.WriteString("<div class=\"summary\">")
	sb.WriteString(fmt.Sprintf("Session started %s. %d turns, %d tokens, $%.4f.", date, turns, totalTokens, p.s.TotalCost))
	sb.WriteString("</div>\n")

	sb.WriteString("<div class=\"metadata\"><table>\n")
	sb.WriteString(p.htmlMetaRow("Session ID", p.s.ID))
	sb.WriteString(p.htmlMetaRow("Working Directory", p.s.WorkingDirectory))
	sb.WriteString(p.htmlMetaRow("Provider", p.s.Provider))
	sb.WriteString(p.htmlMetaRow("Model", p.s.Model))
	sb.WriteString(p.htmlMetaRow("Started", p.s.StartedAt.In(p.loc).Format(time.RFC3339)))
	sb.WriteString(p.htmlMetaRow("Last Updated", p.s.LastUpdated.In(p.loc).Format(time.RFC3339)))
	sb.WriteString(p.htmlMetaRow("Total Cost", fmt.Sprintf("$%.4f", p.s.TotalCost)))
	sb.WriteString(p.htmlMetaRow("Input Tokens", fmt.Sprintf("%d", p.s.InputTokens)))
	sb.WriteString(p.htmlMetaRow("Output Tokens", fmt.Sprintf("%d", p.s.OutputTokens)))
	sb.WriteString(p.htmlMetaRow("Tools Used", toolList))
	sb.WriteString("</table></div>\n")

	sb.WriteString("<h2>Table of Contents</h2>\n<ul class=\"toc\">\n")
	turnsList := groupTurns(p.s.Messages)
	for i, turn := range turnsList {
		ts := turn[0].Timestamp.In(p.loc).Format("15:04:05")
		sb.WriteString(fmt.Sprintf("<li><a href=\"#turn-%d\">Turn %d — %s</a></li>\n", i+1, i+1, html.EscapeString(ts)))
	}
	sb.WriteString("</ul>\n")

	_, err := p.w.Write([]byte(sb.String()))
	return err
}

func (p *htmlPrinter) htmlMetaRow(label, value string) string {
	return fmt.Sprintf("  <tr><th>%s</th><td>%s</td></tr>\n",
		html.EscapeString(label), html.EscapeString(value))
}

func (p *htmlPrinter) writeTurns(redact bool) error {
	turns := groupTurns(p.s.Messages)
	for i, turn := range turns {
		if err := p.writeTurn(i+1, turn, redact); err != nil {
			return err
		}
	}
	return nil
}

func (p *htmlPrinter) writeTurn(num int, msgs []MessageSource, redact bool) error {
	ts := msgs[0].Timestamp.In(p.loc).Format(time.RFC3339)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<div class=\"turn\" id=\"turn-%d\">\n", num))
	sb.WriteString(fmt.Sprintf("<h2>Turn %d — %s</h2>\n", num, html.EscapeString(ts)))

	for _, m := range msgs {
		if err := p.writeMessageHTML(&sb, m, redact); err != nil {
			return err
		}
	}

	if p.opts.IncludeCost {
		var totalCost float64
		var totalTokens int
		for _, m := range msgs {
			totalCost += m.Cost
			totalTokens += m.Tokens
		}
		sb.WriteString(fmt.Sprintf("<div class=\"footer\">Cost: $%.4f | Tokens: %d</div>\n", totalCost, totalTokens))
	}

	sb.WriteString("</div>\n")

	_, err := p.w.Write([]byte(sb.String()))
	return err
}

func (p *htmlPrinter) writeMessageHTML(sb *strings.Builder, m MessageSource, redact bool) error {
	content := m.Content
	if redact {
		content = secretdetect.RedactOpaque(content)
	}

	roleClass := map[string]string{
		"user":      "role-user",
		"assistant": "role-assistant",
		"tool":      "role-tool",
		"system":    "role-system",
	}[m.Role]
	if roleClass == "" {
		roleClass = "role"
	}

	sb.WriteString(fmt.Sprintf("<div><span class=\"role %s\">%s:</span></div>\n",
		roleClass, html.EscapeString(m.Role)))
	sb.WriteString(fmt.Sprintf("<div class=\"content\">%s</div>\n", html.EscapeString(content)))

	// Tool calls in assistant messages
	if p.opts.IncludeToolCalls && len(m.ToolCalls) > 0 {
		for _, tc := range m.ToolCalls {
			tcArgs := tc.Arguments
			if redact {
				tcArgs = secretdetect.RedactOpaque(tcArgs)
			}
			sb.WriteString("<details>\n")
			sb.WriteString(fmt.Sprintf("<summary>Tool call: <code>%s</code></summary>\n", html.EscapeString(tc.Name)))
			sb.WriteString("<pre><code>")
			sb.WriteString(html.EscapeString(tcArgs))
			sb.WriteString("</code></pre>\n")
			sb.WriteString("</details>\n")
		}
	}

	// Tool results
	if p.opts.IncludeToolCalls && m.Role == "tool" {
		name := "tool"
		resContent := content
		if m.ToolResult != nil {
			name = m.ToolResult.ToolCallName
			resContent = m.ToolResult.Content
			if redact {
				resContent = secretdetect.RedactOpaque(resContent)
			}
		}
		sb.WriteString("<details>\n")
		sb.WriteString(fmt.Sprintf("<summary>Tool result: <code>%s</code></summary>\n", html.EscapeString(name)))
		sb.WriteString("<pre><code>")
		sb.WriteString(html.EscapeString(resContent))
		sb.WriteString("</code></pre>\n")
		sb.WriteString("</details>\n")
	}

	return nil
}

func (p *htmlPrinter) writeBodyEnd() error {
	_, err := io.WriteString(p.w, "</body>\n</html>\n")
	return err
}

// ---------------------------------------------------------------------------
// JSON redaction
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
