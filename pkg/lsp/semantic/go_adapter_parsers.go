package semantic

import (
	"regexp"
	"strconv"
	"strings"
)

// goLineColToOffset converts a 1-based line:col to a 0-based byte offset in content.
func goLineColToOffset(content string, line, col int) int {
	if line <= 0 {
		line = 1
	}
	if col <= 0 {
		col = 1
	}
	currentLine := 1
	lineStart := 0
	for i, ch := range content {
		if currentLine == line {
			offset := lineStart + col - 1
			if offset > len(content) {
				return len(content)
			}
			return offset
		}
		if ch == '\n' {
			currentLine++
			lineStart = i + 1
		}
	}
	return len(content)
}

func isIdentRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

var goErrorRE = regexp.MustCompile(`^[^:]+:(\d+):(\d+): (.+)$`)

func parseGofmtErrors(output, content string) []ToolDiagnostic {
	var diags []ToolDiagnostic
	for _, raw := range strings.Split(output, "\n") {
		m := goErrorRE.FindStringSubmatch(strings.TrimSpace(raw))
		if m == nil {
			continue
		}
		lineNum, _ := strconv.Atoi(m[1])
		colNum, _ := strconv.Atoi(m[2])
		from := goLineColToOffset(content, lineNum, colNum)
		diags = append(diags, ToolDiagnostic{
			From:     from,
			To:       from + 1,
			Severity: "error",
			Message:  m[3],
			Source:   "gofmt",
		})
	}
	return diags
}

func parseGoVetErrors(output, content string) []ToolDiagnostic {
	var diags []ToolDiagnostic
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			continue
		}
		m := goErrorRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lineNum, _ := strconv.Atoi(m[1])
		colNum, _ := strconv.Atoi(m[2])
		from := goLineColToOffset(content, lineNum, colNum)
		diags = append(diags, ToolDiagnostic{
			From:     from,
			To:       from + 1,
			Severity: "warning",
			Message:  m[3],
			Source:   "go vet",
		})
	}
	return diags
}

var goplsDefRE = regexp.MustCompile(`^(.+?):(\d+):(\d+)`)

func parseGoplsDefinition(output string) (path string, line, col int, ok bool) {
	for _, raw := range strings.Split(output, "\n") {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		m := goplsDefRE.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		lineNum, _ := strconv.Atoi(m[2])
		colNum, _ := strconv.Atoi(m[3])
		return m[1], lineNum, colNum, true
	}
	return "", 0, 0, false
}
