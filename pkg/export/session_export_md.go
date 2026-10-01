package export

// session_export_md.go — the Markdown session exporter: the mdPrinter
// type and its document / turn / message / tool writers, plus the
// markdown-only escape helper. Split out of session_export.go.

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/secretdetect"
)

// ---------------------------------------------------------------------------
// Public types
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
