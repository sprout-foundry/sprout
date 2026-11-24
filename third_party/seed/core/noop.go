package core

import "context"

// NoopUI is a headless UI implementation that discards all output and never
// prompts. Use it when the agent runs without a terminal or interactive layer.
var NoopUI UI = &noopUI{}

type noopUI struct{}

func (n *noopUI) Prompt(_ string) (string, error) {
	return "", nil
}

func (n *noopUI) Confirm(_ string) (bool, error) {
	return true, nil
}

func (n *noopUI) Print(_ string) {}

func (n *noopUI) PrintLine(_ string) {}

// NoopExecutor is a ToolExecutor with no tools. Use it when the agent only
// needs to produce text responses without tool execution.
var NoopExecutor ToolExecutor = &noopExecutor{}

type noopExecutor struct{}

func (n *noopExecutor) GetTools() []Tool {
	return nil
}

func (n *noopExecutor) Execute(_ context.Context, _ []ToolCall) []Message {
	return nil
}
