package semantic

import (
	"os/exec"
	"testing"
)

func TestNewTypeScriptAdapter(t *testing.T) {
	adapter := NewTypeScriptAdapter()
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
}

func TestTypeScriptAdapterRun(t *testing.T) {
	_, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}

	adapter := NewTypeScriptAdapter()
	// Test basic diagnostics on a simple TS file
	result, err := adapter.Run(ToolInput{
		Method:        "diagnostics",
		Content:       "const x: number = 1;\nconsole.log(x);\n",
		FilePath:      "test.ts",
		WorkspaceRoot: ".",
	})
	if err != nil {
		// This can fail if typescript is not installed in the workspace
		t.Logf("TypeScript adapter run failed (expected without ts): %v", err)
		return
	}
	t.Logf("TypeScript diagnostics result: capabilities=%+v, diags=%d", result.Capabilities, len(result.Diagnostics))
}

func TestTypeScriptAdapterRunWithEmptyPaths(t *testing.T) {
	_, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}

	// Test with empty workspace — should not panic
	adapter := NewTypeScriptAdapter()
	_, err = adapter.Run(ToolInput{
		Method:        "diagnostics",
		Content:       "const x = 1",
		FilePath:      "",
		WorkspaceRoot: "",
	})
	// This may or may not error depending on environment, just ensure no panic
	_ = err
}

func TestRunTypeScriptToolBasicInput(t *testing.T) {
	// Verify runTypeScriptTool handles basic input without panicking
	_, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}

	_, _ = runTypeScriptTool(ToolInput{
		Method:        "diagnostics",
		Content:       "",
		FilePath:      "test.ts",
		WorkspaceRoot: ".",
	})
}
