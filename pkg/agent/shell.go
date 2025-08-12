package agent

import (
	"fmt"
	"os/exec"
)

// executeShellCommands runs the specified shell commands
func executeShellCommands(context *AgentContext, commands []string) error {
	context.Logger.LogProcessStep(fmt.Sprintf("🔧 Executing %d shell commands...", len(commands)))

	for i, command := range commands {
		context.Logger.LogProcessStep(fmt.Sprintf("Running command %d: %s", i+1, command))

		if command == "" {
			continue
		}

		// Use shell to execute command to handle pipes, redirects, etc.
		cmd := exec.Command("sh", "-c", command)
		output, err := cmd.CombinedOutput()

		// Truncate output immediately to prevent huge outputs from overwhelming the system
		outputStr := string(output)
		const maxOutputSize = 10000 // 10KB limit
		if len(outputStr) > maxOutputSize {
			outputStr = outputStr[:maxOutputSize] + "\n... (output truncated - limit 10KB)"
		}

		if err != nil {
			errorMsg := fmt.Sprintf("Command failed: %s (output: %s)", err.Error(), outputStr)
			context.Errors = append(context.Errors, errorMsg)
			context.Logger.LogProcessStep(fmt.Sprintf("❌ Command %d failed: %s", i+1, errorMsg))
		} else {
			result := fmt.Sprintf("Command %d succeeded: %s", i+1, outputStr)
			context.ExecutedOperations = append(context.ExecutedOperations, result)
			context.Logger.LogProcessStep(fmt.Sprintf("✅ Command %d: %s", i+1, outputStr))
		}
	}

	return nil
}
