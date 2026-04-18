package semantic

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type goAdapter struct{}

// NewGoAdapter constructs a Go semantic adapter.
func NewGoAdapter() Adapter {
	return goAdapter{}
}

// Run dispatches to the appropriate Go analysis routine.
func (a goAdapter) Run(input ToolInput) (ToolResult, error) {
	switch input.Method {
	case "diagnostics":
		return runGoDiagnostics(input)
	case "definition":
		return runGoDefinition(input)
	default:
		return ToolResult{Capabilities: Capabilities{}}, nil
	}
}

// runGoDiagnostics reports syntax errors (via gofmt -e) and vet issues
// (via go vet) by writing the current file content to a temp package.
func runGoDiagnostics(input ToolInput) (ToolResult, error) {
	tmpDir, err := os.MkdirTemp("", "ledit-go-diag-*")
	if err != nil {
		return ToolResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	baseName := filepath.Base(input.FilePath)
	if baseName == "" || baseName == "." {
		baseName = "main.go"
	}
	tmpFile := filepath.Join(tmpDir, baseName)

	if err := os.WriteFile(tmpFile, []byte(input.Content), 0600); err != nil {
		return ToolResult{}, err
	}
	const goMod = "module ledit_temp\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0600); err != nil {
		return ToolResult{}, err
	}

	caps := Capabilities{Diagnostics: true, Definition: true}

	fmtCmd := exec.Command("gofmt", "-e", tmpFile)
	var fmtStderr bytes.Buffer
	fmtCmd.Stderr = &fmtStderr
	fmtCmd.Dir = tmpDir
	_ = fmtCmd.Run()
	diagnostics := parseGofmtErrors(fmtStderr.String(), input.Content)

	if len(diagnostics) == 0 && input.Trigger == "save" {
		vetCmd := exec.Command("go", "vet", "./...")
		var vetStderr bytes.Buffer
		vetCmd.Stderr = &vetStderr
		vetCmd.Dir = tmpDir
		_ = vetCmd.Run()
		diagnostics = append(diagnostics, parseGoVetErrors(vetStderr.String(), input.Content)...)
	}

	return ToolResult{Capabilities: caps, Diagnostics: diagnostics}, nil
}

// runGoDefinition resolves the definition at a position using gopls.
func runGoDefinition(input ToolInput) (ToolResult, error) {
	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: false},
			Error:        "gopls_not_available",
		}, nil
	}
	return runGoDefinitionWithRemote(input, goplsPath, "")
}

func runGoDefinitionWithRemote(input ToolInput, goplsPath, remoteAddr string) (ToolResult, error) {
	pos := input.Position
	if pos == nil {
		pos = &Position{Line: 1, Column: 1}
	}
	posArg := fmt.Sprintf("%s:%d:%d", input.FilePath, pos.Line, pos.Column)

	args := make([]string, 0, 3)
	if remoteAddr != "" {
		args = append(args, "-remote="+remoteAddr)
	}
	args = append(args, "definition", posArg)

	cmd := exec.Command(goplsPath, args...)
	cmd.Dir = input.WorkspaceRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()

	defPath, defLine, defCol, ok := parseGoplsDefinition(stdout.String())
	if !ok {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true},
		}, nil
	}
	return ToolResult{
		Capabilities: Capabilities{Diagnostics: true, Definition: true},
		Definition:   &ToolDefinition{Path: defPath, Line: defLine, Column: defCol},
	}, nil
}

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
