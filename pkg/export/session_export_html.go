package export

// session_export_html.go — the HTML session exporter: the htmlPrinter
// type and its document / head / metadata / turn / message writers. Split
// out of session_export.go.

import (
	"fmt"
	"html"
	"io"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/secretdetect"
)

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------
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
