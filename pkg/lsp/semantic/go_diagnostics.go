package semantic

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// runGoDiagnostics reports syntax errors (via gofmt -e) and vet issues
// (via go vet) by writing the current file content to a temp package.
func runGoDiagnostics(input ToolInput) (ToolResult, error) {
	tmpDir, err := os.MkdirTemp("", "sprout-go-diag-*")
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
	const goMod = "module sprout_temp\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0600); err != nil {
		return ToolResult{}, err
	}

	caps := Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true}

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
