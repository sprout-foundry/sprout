package prompts

import "fmt"

// Config-related prompts - minimal set needed for configuration

func MemoryDetectionError(fallback string, err error) string {
	return fmt.Sprintf("⚠️  Could not detect system memory: %v. Using fallback model: %s", err, fallback)
}

func SystemMemoryFallback(memoryGB int, model string) string {
	return fmt.Sprintf("ℹ️  Detected %dGB system memory. Using model: %s", memoryGB, model)
}

func EnterAgentModel(defaultModel string) string {
	return fmt.Sprintf("Enter agent model (default: %s): ", defaultModel)
}

func EnterSummaryModel(defaultModel string) string {
	return fmt.Sprintf("Enter summary model (default: %s): ", defaultModel)
}

func EnterWorkspaceModel(defaultModel string) string {
	return fmt.Sprintf("Enter workspace model (default: %s): ", defaultModel)
}

func EnterOrchestrationModel(defaultModel string) string {
	return fmt.Sprintf("Enter orchestration model (default: %s): ", defaultModel)
}

func TrackGitPrompt() string {
	return "Track git changes? (y/n, default: y): "
}

func EnableSecurityChecksPrompt() string {
	return "Enable security checks? (y/n, default: y): "
}

func EnterLLMProvider(defaultProvider string) string {
	return fmt.Sprintf("Enter LLM provider (default: %s): ", defaultProvider)
}

func NoConfigFound() string {
	return "No configuration found. Creating new configuration..."
}

func ConfigSaved(path string) string {
	return fmt.Sprintf("✅ Configuration saved to: %s", path)
}
