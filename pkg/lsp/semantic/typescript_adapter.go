package semantic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type typeScriptAdapter struct{}

// NewTypeScriptAdapter constructs a TS/JS semantic adapter.
func NewTypeScriptAdapter() Adapter {
	return typeScriptAdapter{}
}

func (a typeScriptAdapter) Run(input ToolInput) (ToolResult, error) {
	return runTypeScriptTool(input)
}

func runTypeScriptTool(input ToolInput) (ToolResult, error) {
	var out ToolResult

	in, err := json.Marshal(input)
	if err != nil {
		return out, err
	}

	cmd := exec.Command("node", "-e", typeScriptNodeScript)
	cmd.Stdin = bytes.NewReader(in)
	cmd.Dir = input.WorkspaceRoot

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return out, fmt.Errorf("ts/js semantic tool failed: %s", errMsg)
	}

	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return out, fmt.Errorf("ts/js semantic tool output parse failed: %w", err)
	}

	return out, nil
}
