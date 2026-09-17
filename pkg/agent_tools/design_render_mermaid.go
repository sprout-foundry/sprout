//go:build !js

package tools

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// design_render_mermaid.go — the mermaid branch of design_render (SP-140-2
// §2c). Mermaid has no Go renderer, so a flow .mmd source is rendered by
// generating a standalone HTML page that embeds the pinned vendored mermaid
// bundle plus the flow source, then rasterizing that page through the same
// browser path as SVG/HTML. The .mmd source is read-only: the generated HTML
// is a derived artifact in a temp file and the source is never mutated.

// mermaidScript is the pinned vendored mermaid UMD bundle (v11.4.1, MIT).
// Embedding it keeps flow rendering offline-safe — no CDN, no network — which
// is why the copy is vendored rather than fetched at render time. Provenance
// and the upgrade procedure live in design/NOTICE.
//
//go:embed design/mermaid.min.js
var mermaidScript string

// flowLayouts maps the flow_layout argument to a mermaid flowchart direction
// keyword. The keyword is applied to the flow's declaration line, so the
// caller can nudge layout without touching the .mmd source. Unknown values
// fall back to the source's own direction (no override).
//
// The values mirror mermaid's own direction keywords and SP-140-3's dagre
// orientation hints so the canvas and the renderer agree.
var flowLayouts = map[string]string{
	"top-down":   "TD",
	"td":         "TD",
	"tb":         "TB",
	"bottom-up":  "BT",
	"bt":         "BT",
	"left-right": "LR",
	"lr":         "LR",
	"right-left": "RL",
	"rl":         "RL",
}

// normaliseFlowLayout returns the mermaid direction keyword for a flow_layout
// argument, or "" when there is no override (empty/unknown).
func normaliseFlowLayout(layout string) string {
	return flowLayouts[strings.ToLower(strings.TrimSpace(layout))]
}

// applyFlowLayout rewrites a mermaid source's declaration line to carry the
// requested direction, leaving everything else byte-for-byte intact. It is a
// pure string transform — the caller writes the result to the generated HTML,
// never back to the .mmd.
//
// The declaration is the first `flowchart`/`graph` line (comments and blank
// lines may precede it). When direction is "" or no declaration is found, the
// source is returned unchanged. An existing direction keyword (TD/LR/…) is
// replaced; a bare `flowchart` gains the keyword.
func applyFlowLayout(source, direction string) string {
	if direction == "" {
		return source
	}
	lines := strings.Split(source, "\n")
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		lower := strings.ToLower(trimmed)
		if !strings.HasPrefix(lower, "flowchart") && !strings.HasPrefix(lower, "graph") {
			continue
		}
		// Preserve the original keyword casing by rewriting only the tail:
		// find the first whitespace after the keyword and replace the rest.
		idx := strings.IndexAny(trimmed, " \t")
		if idx < 0 {
			lines[i] = trimmed + " " + direction
			return strings.Join(lines, "\n")
		}
		lines[i] = trimmed[:idx] + " " + direction
		return strings.Join(lines, "\n")
	}
	return source
}

// buildMermaidHTML produces a self-contained HTML document that renders the
// given mermaid source. The vendored mermaid bundle and the (layout-adjusted)
// source are embedded inline; there are no external references, so the page
// renders offline in the headless browser exactly as it would in a browser.
//
// The source is embedded inside a JSON string in a <script> block, which
// mermaid reads via `mermaid.render`. JSON escaping handles quotes, newlines,
// and backslashes; `<` is additionally escaped so a source containing `</script>`
// cannot terminate the block early (the same defense the wrap-in-JSON gives for
// `-->` sequences, which are JSON-safe).
func buildMermaidHTML(source, direction string) (string, error) {
	adjusted := applyFlowLayout(source, direction)

	// json.Marshal escapes quotes/backslashes/newlines but not `<`, so we
	// escape `<` afterwards to prevent `</script>` from closing the block.
	encoded, err := json.Marshal(adjusted)
	if err != nil {
		return "", fmt.Errorf("encode mermaid source: %w", err)
	}
	sourceJSON := strings.ReplaceAll(string(encoded), "<", `\u003c`)

	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html><head><meta charset=\"utf-8\">\n")
	sb.WriteString("<style>html,body{margin:0;padding:0;background:#fff}svg{max-width:100%}</style>\n")
	sb.WriteString("</head><body>\n<div id=\"graph\"></div>\n")
	// The vendored bundle defines globalThis.mermaid. It is inline, so the
	// page needs no network access.
	sb.WriteString("<script>\n")
	sb.WriteString(mermaidScript)
	sb.WriteString("\n</script>\n")
	sb.WriteString("<script>\n")
	sb.WriteString("(function(){\n")
	sb.WriteString("  var src = ")
	sb.WriteString(sourceJSON)
	sb.WriteString(";\n")
	sb.WriteString("  try {\n")
	sb.WriteString("    mermaid.initialize({ startOnLoad: false });\n")
	sb.WriteString("    mermaid.render('design-render-graph', src).then(function(res){\n")
	sb.WriteString("      document.getElementById('graph').innerHTML = res.svg;\n")
	sb.WriteString("      document.body.setAttribute('data-render-ready', 'true');\n")
	sb.WriteString("    }).catch(function(e){\n")
	sb.WriteString("      document.body.setAttribute('data-render-error', String(e));\n")
	sb.WriteString("    });\n")
	sb.WriteString("  } catch (e) {\n")
	sb.WriteString("    document.body.setAttribute('data-render-error', String(e));\n")
	sb.WriteString("  }\n")
	sb.WriteString("})();\n")
	sb.WriteString("</script>\n</body></html>\n")
	return sb.String(), nil
}

// writeMermaidHTMLFile writes the standalone page generated by
// buildMermaidHTML to a temp .html file and returns its path plus a cleanup
// func that removes it. The browser rasterizes this file; the .mmd source it
// was derived from is never written.
func writeMermaidHTMLFile(source, direction string) (string, func(), error) {
	html, err := buildMermaidHTML(source, direction)
	if err != nil {
		return "", func() {}, err
	}
	tmp, err := os.CreateTemp("", "sprout-mermaid-*.html")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp mermaid html: %w", err)
	}
	path := tmp.Name()
	cleanup := func() { os.Remove(path) } // best-effort
	if _, err := tmp.WriteString(html); err != nil {
		tmp.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("write temp mermaid html: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("close temp mermaid html: %w", err)
	}
	return path, cleanup, nil
}
