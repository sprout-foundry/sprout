package semantic

import (
	"bytes"
	"fmt"
	"os/exec"
)

// runGoDefinitionWithRemote resolves the definition at a position using gopls
// with an optional remote address for the gopls server.
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
