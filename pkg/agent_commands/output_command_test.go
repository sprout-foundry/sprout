package commands

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

type writerAwareTestCommand struct {
	stdout io.Writer
}

func (c *writerAwareTestCommand) Name() string        { return "writer-test" }
func (c *writerAwareTestCommand) Description() string { return "writer test command" }
func (c *writerAwareTestCommand) SetOutput(w io.Writer) {
	c.stdout = w
}
func (c *writerAwareTestCommand) Execute(_ []string, _ *agent.Agent) error {
	w := c.stdout
	if w == nil {
		w = os.Stdout
	}
	_, err := io.WriteString(w, "captured output")
	return err
}

func TestCommandRegistryExecuteWiresOutputWriter(t *testing.T) {
	registry := NewCommandRegistry()
	cmd := &writerAwareTestCommand{}
	registry.Register(cmd)

	var output bytes.Buffer
	registry.SetOutput(&output)
	if err := registry.Execute("/writer-test", nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := output.String(); got != "captured output" {
		t.Fatalf("output = %q, want %q", got, "captured output")
	}
}

func TestCommandRegistrySetOutputNilClearsCommandWriter(t *testing.T) {
	registry := NewCommandRegistry()
	cmd := &writerAwareTestCommand{}
	registry.Register(cmd)

	registry.SetOutput(io.Discard)
	if err := registry.Execute("/writer-test", nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if cmd.stdout == nil {
		t.Fatal("expected writer to be set during execution")
	}

	registry.SetOutput(nil)
	if cmd.stdout != nil {
		t.Fatal("expected SetOutput(nil) to clear the command writer")
	}
}
