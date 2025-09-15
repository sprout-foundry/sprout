package commands

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	config "github.com/alantheprice/ledit/pkg/agent_config"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

// ShellCommand handles the /shell slash command
// Usage: /shell <description-of-shell-script-to-generate>
// This command uses the fast model to generate shell scripts from natural language descriptions
// with full environmental context. It does not execute the script - just generates it.

type ShellCommand struct{}

func (c *ShellCommand) Name() string {
	return "shell"
}

func (c *ShellCommand) Description() string {
	return "Generate shell scripts from natural language descriptions with full environmental context"
}

func (c *ShellCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /shell <description-of-shell-script-to-generate>")
	}

	description := strings.Join(args, " ")

	// Gather environmental context
	envContext, err := c.gatherEnvironmentalContext()
	if err != nil {
		return fmt.Errorf("failed to gather environmental context: %v", err)
	}

	// Create a simple client using the unified provider system
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	// Get a unified client wrapper
	clientWrapper, err := api.NewUnifiedClientWithModel(cfg.LastUsedProvider, api.FastModel)
	if err != nil {
		return fmt.Errorf("failed to create client: %v", err)
	}

	// Create a comprehensive prompt with environmental context
	systemPrompt := "You are a shell command generator. Output ONLY executable shell code. No explanations, no markdown, no commentary."

	userPrompt := fmt.Sprintf(`Generate a shell command or script for: "%s"

Environmental Context:
%s

Requirements:
- For simple tasks: output a single command line
- For complex tasks: output a complete script with shebang line (#!/bin/bash)
- Include error handling for complex scripts
- Use commands appropriate for the detected environment
- Add shell comments (starting with #) only when necessary

Generate the command/script now:`, description, envContext)

	fmt.Printf("🤖 Generating shell script with environmental context...\n")

	// Send chat request directly without tools
	messages := []api.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	response, err := clientWrapper.SendChatRequest(messages, nil, "") // nil tools = no tool usage
	if err != nil {
		return fmt.Errorf("failed to generate shell script: %v", err)
	}

	// Clean up the result
	generatedScript := ""
	if len(response.Choices) > 0 {
		generatedScript = strings.TrimSpace(response.Choices[0].Message.Content)
	}

	if generatedScript == "" {
		return fmt.Errorf("fast model did not generate a valid shell script")
	}

	// Validate that the output looks like executable code
	if !c.isValidShellCode(generatedScript) {
		// Try one more time with a more explicit prompt
		retryMessages := []api.Message{
			{Role: "system", Content: "Output ONLY executable shell code. No text, no explanation."},
			{Role: "user", Content: fmt.Sprintf(`Output the shell command for: "%s"
Example format: find . -name "*.go" | wc -l`, description)},
		}

		response, err = clientWrapper.SendChatRequest(retryMessages, nil, "")
		if err != nil {
			return fmt.Errorf("failed to regenerate shell script: %v", err)
		}

		if len(response.Choices) > 0 {
			generatedScript = strings.TrimSpace(response.Choices[0].Message.Content)
		}

		if !c.isValidShellCode(generatedScript) {
			return fmt.Errorf("failed to generate valid executable shell code")
		}
	}

	// Check if it's a single command or a script
	isSingleCommand := !strings.Contains(generatedScript, "\n") || !strings.HasPrefix(generatedScript, "#!")

	if isSingleCommand {
		fmt.Printf("\n📜 Generated Command:\n")
		fmt.Println("─" + strings.Repeat("─", 60))
		fmt.Printf("%s\n", generatedScript)
		fmt.Println("─" + strings.Repeat("─", 60))
	} else {
		fmt.Printf("\n📜 Generated Shell Script:\n")
		fmt.Println("═" + strings.Repeat("═", 60))
		fmt.Printf("%s\n", generatedScript)
		fmt.Println("═" + strings.Repeat("═", 60))
	}

	// Ask user for confirmation
	fmt.Printf("\n🤔 Do you want to execute this %s? (yes/no): ", c.getScriptType(isSingleCommand))

	reader := bufio.NewReader(os.Stdin)
	userResponse, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read user response: %v", err)
	}

	userResponse = strings.ToLower(strings.TrimSpace(userResponse))
	if userResponse != "yes" && userResponse != "y" {
		fmt.Printf("❌ Execution cancelled.\n")
		return nil
	}

	// Execute the command/script
	fmt.Printf("\n🚀 Executing %s...\n\n", c.getScriptType(isSingleCommand))

	var execErr error
	var output string

	if isSingleCommand {
		// Execute single command directly
		output, execErr = tools.ExecuteShellCommand(generatedScript)
	} else {
		// For scripts, save to temporary file and execute
		tmpFile, err := os.CreateTemp("", "ledit-script-*.sh")
		if err != nil {
			return fmt.Errorf("failed to create temporary script file: %v", err)
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.WriteString(generatedScript); err != nil {
			return fmt.Errorf("failed to write script to temporary file: %v", err)
		}

		if err := tmpFile.Close(); err != nil {
			return fmt.Errorf("failed to close temporary file: %v", err)
		}

		// Make script executable
		if err := os.Chmod(tmpFile.Name(), 0755); err != nil {
			return fmt.Errorf("failed to make script executable: %v", err)
		}

		// Execute the script
		output, execErr = tools.ExecuteShellCommand(tmpFile.Name())
	}

	// Display results
	if execErr != nil {
		fmt.Printf("❌ Execution failed: %v\n", execErr)
		if output != "" {
			fmt.Printf("\nOutput:\n%s\n", output)
		}
		return nil
	}

	fmt.Printf("✅ %s executed successfully!\n", c.getScriptType(isSingleCommand))
	if output != "" {
		fmt.Printf("\nOutput:\n%s\n", output)
	}

	return nil
}

// gatherEnvironmentalContext collects information about the current environment
func (c *ShellCommand) gatherEnvironmentalContext() (string, error) {
	var context strings.Builder

	// Operating System
	context.WriteString(fmt.Sprintf("Operating System: %s\n", runtime.GOOS))
	context.WriteString(fmt.Sprintf("Architecture: %s\n", runtime.GOARCH))

	// Current user
	if currentUser, err := user.Current(); err == nil {
		context.WriteString(fmt.Sprintf("Current User: %s (%s)\n", currentUser.Username, currentUser.Uid))
		context.WriteString(fmt.Sprintf("Home Directory: %s\n", currentUser.HomeDir))
	}

	// Working directory
	if wd, err := os.Getwd(); err == nil {
		context.WriteString(fmt.Sprintf("Working Directory: %s\n", wd))
	}

	// Shell information
	shell := os.Getenv("SHELL")
	if shell == "" {
		if runtime.GOOS == "windows" {
			shell = "cmd.exe"
		} else {
			shell = "/bin/sh"
		}
	}
	context.WriteString(fmt.Sprintf("Shell: %s\n", shell))

	// Key environment variables
	context.WriteString("Environment Variables:\n")
	keyEnvVars := []string{"PATH", "HOME", "USER", "TERM", "EDITOR", "LANG", "PWD"}
	for _, envVar := range keyEnvVars {
		if value := os.Getenv(envVar); value != "" {
			// Truncate very long PATH variables for readability
			if envVar == "PATH" && len(value) > 200 {
				value = value[:200] + "... [truncated]"
			}
			context.WriteString(fmt.Sprintf("  %s=%s\n", envVar, value))
		}
	}

	// Check for common tools/package managers
	context.WriteString("Available Tools: ")
	commonTools := []string{"git", "docker", "kubectl", "npm", "yarn", "go", "python", "pip", "brew", "apt-get", "yum"}
	availableTools := []string{}

	for _, tool := range commonTools {
		if _, err := exec.LookPath(tool); err == nil {
			availableTools = append(availableTools, tool)
		}
	}

	if len(availableTools) > 0 {
		context.WriteString(strings.Join(availableTools, ", "))
	} else {
		context.WriteString("Standard system utilities")
	}
	context.WriteString("\n")

	return context.String(), nil
}

// getScriptType returns a user-friendly description of what's being executed
func (c *ShellCommand) getScriptType(isSingleCommand bool) string {
	if isSingleCommand {
		return "command"
	}
	return "script"
}

// isValidShellCode checks if the output looks like executable shell code
func (c *ShellCommand) isValidShellCode(code string) bool {
	// Check for common indicators that this is NOT shell code
	lowerCode := strings.ToLower(code)

	// If it starts with explanation phrases, it's not valid shell code
	invalidStarts := []string{
		"the previous",
		"i have",
		"here is",
		"this is",
		"to count",
		"you can",
		"output:",
		"result:",
		"error:",
		"note:",
	}

	for _, start := range invalidStarts {
		if strings.HasPrefix(lowerCode, start) {
			return false
		}
	}

	// If it contains sentences with periods (except in comments), it's probably not shell code
	lines := strings.Split(code, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip comments and empty lines
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Check for sentence-like patterns
		if strings.Contains(trimmed, ". ") || strings.HasSuffix(trimmed, ".") {
			// Exception for commands that might have dots (e.g., file.txt)
			if !strings.Contains(trimmed, "=") && !strings.Contains(trimmed, "/") && !strings.Contains(trimmed, "\\") {
				return false
			}
		}
	}

	return true
}
