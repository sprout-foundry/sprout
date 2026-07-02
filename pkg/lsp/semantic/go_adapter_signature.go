package semantic

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// runGoSignatureHelp retrieves signature help at a position using gopls.
func runGoSignatureHelp(input ToolInput) (ToolResult, error) {
	caps := Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: true, SignatureHelp: true}

	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true, InlayHints: true, SignatureHelp: false},
			Error:        "gopls_not_available",
		}, nil
	}

	pos := input.Position
	if pos == nil {
		pos = &Position{Line: 1, Column: 1}
	}
	posArg := fmt.Sprintf("%s:%d:%d", input.FilePath, pos.Line, pos.Column)

	cmd := exec.Command(goplsPath, "signature", posArg)
	cmd.Dir = input.WorkspaceRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// gopls signature-help returns non-zero when no signature is available.
		return ToolResult{
			Capabilities:  caps,
			SignatureHelp: &ToolSignatureHelp{},
		}, nil
	}

	sigHelp := parseGoplsSignatureHelp(stdout.String(), input)
	return ToolResult{
		Capabilities:  caps,
		SignatureHelp: &sigHelp,
	}, nil
}

// parseGoplsSignatureHelp parses gopls signature-help output.
// Output format is plain text like:
//
//	func (t *T) MethodName(param1 type1, param2 type2) (result)
//	param1 type1, param2 type2
//
// Line 1 is the full signature. Line 2 (if present) is just the params.
// We parse it into our structured type. Active parameter is computed from
// the cursor position by counting commas at the same nesting depth.
func parseGoplsSignatureHelp(output string, input ToolInput) ToolSignatureHelp {
	text := strings.TrimSpace(output)
	if text == "" {
		return ToolSignatureHelp{}
	}

	lines := strings.Split(text, "\n")
	sigLabel := strings.TrimSpace(lines[0])
	if sigLabel == "" {
		return ToolSignatureHelp{}
	}

	// Extract parameters from the signature label.
	// Find the first '(' and its matching ')'.
	params := extractParamsFromSignature(sigLabel)

	// Compute active parameter from cursor position in the source content.
	activeParam := computeActiveParameter(input)

	return ToolSignatureHelp{
		Signatures: []ToolSignatureHelpSignature{
			{
				Label:      sigLabel,
				Parameters: params,
			},
		},
		ActiveSignature: 0,
		ActiveParameter: activeParam,
	}
}

// computeActiveParameter determines which parameter the cursor is on by
// counting commas at depth 0 between the opening paren and the cursor position.
func computeActiveParameter(input ToolInput) int {
	if input.Position == nil {
		return 0
	}

	lines := strings.Split(input.Content, "\n")
	if input.Position.Line < 1 || input.Position.Line > len(lines) {
		return 0
	}

	// Build the text up to the cursor position
	textUpToCursor := ""
	for i := 0; i < input.Position.Line-1; i++ {
		textUpToCursor += lines[i] + "\n"
	}
	lineIdx := input.Position.Line - 1
	if input.Position.Column > 0 {
		lineRunes := []rune(lines[lineIdx])
		col := input.Position.Column - 1
		if col <= len(lineRunes) {
			textUpToCursor += string(lineRunes[:col])
		} else {
			textUpToCursor += lines[lineIdx]
		}
	}

	// Walk backward from cursor to find the most recent unmatched '('
	depth := 0
	commaCount := 0
	for i := len(textUpToCursor) - 1; i >= 0; i-- {
		ch := textUpToCursor[i]
		if ch == ')' {
			depth++
		} else if ch == '(' {
			if depth == 0 {
				return commaCount
			}
			depth--
		} else if ch == ',' && depth == 0 {
			commaCount++
		}
	}
	return commaCount
}

// extractParamsFromSignature parses "func(params) result" or "func (recv) Name(params) result"
// into individual parameters. For Go methods, the first paren group is the receiver;
// we skip it and use the parameter paren group instead.
func extractParamsFromSignature(sig string) []ToolSignatureHelpParameter {
	// Find all top-level paren groups.
	start := strings.Index(sig, "(")
	if start < 0 {
		return nil
	}

	// Collect all top-level paren groups.
	type parenGroup struct {
		open, close int
	}
	var groups []parenGroup
	i := start
	for i < len(sig) {
		if sig[i] != '(' {
			i++
			continue
		}
		depth := 1
		j := i + 1
		for j < len(sig) && depth > 0 {
			switch sig[j] {
			case '(':
				depth++
			case ')':
				depth--
			}
			j++
		}
		groups = append(groups, parenGroup{open: i, close: j - 1})
		i = j
	}

	if len(groups) == 0 {
		return nil
	}

	// For Go methods like "func (t *T) Method(a int, b string) error",
	// the parameter list is the second group (first is receiver).
	// For plain functions like "func foo(a int, b string) error",
	// the parameter list is the first group.
	// Heuristic: if there are multiple groups, check if the first one
	// looks like a receiver (short, contains '*' or a single identifier).
	paramIdx := 0
	if len(groups) > 1 {
		// If the first group is followed by an identifier (method name),
		// it's a receiver. Otherwise it's the parameter list.
		afterFirst := sig[groups[0].close+1:]
		// Trim leading space
		afterFirst = strings.TrimLeft(afterFirst, " ")
		// If the next non-space char is an identifier or '*', it's a method
		// with a receiver — use the second group.
		if len(afterFirst) > 0 && (isIdentRune(rune(afterFirst[0])) || afterFirst[0] == '*') {
			paramIdx = 1
		}
	}

	if paramIdx >= len(groups) {
		return nil
	}

	paramStr := sig[groups[paramIdx].open+1 : groups[paramIdx].close]

	if paramStr == "" {
		return nil
	}

	// Split by comma, respecting nested parens/brackets.
	var params []ToolSignatureHelpParameter
	d := 0
	current := strings.Builder{}
	for _, ch := range paramStr {
		switch ch {
		case '(', '[', '{':
			d++
			current.WriteRune(ch)
		case ')', ']', '}':
			d--
			current.WriteRune(ch)
		case ',':
			if d == 0 {
				p := strings.TrimSpace(current.String())
				if p != "" {
					params = append(params, ToolSignatureHelpParameter{Label: p})
				}
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}
	p := strings.TrimSpace(current.String())
	if p != "" {
		params = append(params, ToolSignatureHelpParameter{Label: p})
	}

	return params
}
