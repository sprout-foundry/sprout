package semantic

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// runGoDefinition resolves the definition at a position using gopls.
func runGoDefinition(input ToolInput) (ToolResult, error) {
	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: false, Hover: false, Rename: false, CodeActions: true},
			Error:        "gopls_not_available",
		}, nil
	}
	return runGoDefinitionWithRemote(input, goplsPath, "")
}

// runGoHover retrieves hover documentation at a position using gopls.
func runGoHover(input ToolInput) (ToolResult, error) {
	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: false, Rename: false, CodeActions: true},
			Error:        "gopls_not_available",
		}, nil
	}

	pos := input.Position
	if pos == nil {
		pos = &Position{Line: 1, Column: 1}
	}
	posArg := fmt.Sprintf("%s:%d:%d", input.FilePath, pos.Line, pos.Column)

	cmd := exec.Command(goplsPath, "hover", posArg)
	cmd.Dir = input.WorkspaceRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()

	contents := strings.TrimSpace(stdout.String())
	if contents == "" {
		return ToolResult{
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, CodeActions: true},
			Hover:        nil,
		}, nil
	}

	return ToolResult{
		Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, CodeActions: true},
		Hover:        &ToolHover{Contents: contents},
	}, nil
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
			Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true},
		}, nil
	}
	return ToolResult{
		Capabilities: Capabilities{Diagnostics: true, Definition: true, Hover: true, Rename: true, References: true, CodeActions: true},
		Definition:   &ToolDefinition{Path: defPath, Line: defLine, Column: defCol},
	}, nil
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
