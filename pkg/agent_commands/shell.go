package commands

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/factory"
)

// ShellCommand handles the /shell slash command
// Usage: /shell <description-of-shell-script-to-generate>
// This command generates shell scripts from natural language descriptions
// with full environmental context. It does not execute the script - just generates it.

type ShellCommand struct {
	Provider string
	Model    string
}

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

	// Get a unified client wrapper using the current configuration
	configManager, err := configuration.NewManager()
	if err != nil {
		return fmt.Errorf("failed to initialize configuration: %v", err)
	}

	// Determine provider and model from flags, model override string, or config
	var clientType api.ClientType
	var model string

	provider := c.Provider
	modelOverride := c.Model

	if provider != "" && modelOverride != "" {
		// Both provider and model specified via flags
		clientType, err = api.DetermineProvider(provider, "")
		if err != nil {
			return fmt.Errorf("invalid provider '%s': %v", provider, err)
		}
		model = modelOverride
	} else if provider != "" {
		// Only provider specified, get default model for that provider
		clientType, err = api.DetermineProvider(provider, "")
		if err != nil {
			return fmt.Errorf("invalid provider '%s': %v", provider, err)
		}
		model = configManager.GetModelForProvider(clientType)
	} else if modelOverride != "" {
		// Only model specified, parse provider from model string (e.g., "openai:gpt-4")
		lastUsedProvider, _ := configManager.GetProvider()
		clientType, err = api.DetermineProvider(modelOverride, lastUsedProvider)
		if err != nil {
			return fmt.Errorf("failed to determine provider from model '%s': %v", modelOverride, err)
		}
		model = modelOverride
	} else {
		// Use default from config
		clientType, err = configManager.GetProvider()
		if err != nil {
			return fmt.Errorf("failed to get provider: %v", err)
		}
		model = configManager.GetModelForProvider(clientType)
	}

	clientWrapper, err := factory.CreateProviderClient(clientType, model)
	if err != nil {
		return fmt.Errorf("failed to create client: %v", err)
	}

	// Create a comprehensive prompt with environmental context
	systemPrompt := "You are a shell command generator. Output ONLY executable shell code. No explanations, no markdown, no commentary. Do not include <think> tags or any other XML tags. Output only the command or script itself."

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
		return fmt.Errorf("model did not generate a valid shell script")
	}

	// Clean up markdown code blocks if present
	generatedScript = c.cleanMarkdownCodeBlocks(generatedScript)

	// Validate that the output looks like executable code
	if !c.isValidShellCode(generatedScript) {
		// Debug: show what we got
		if os.Getenv("LEDIT_DEBUG") == "1" {
			fmt.Printf("DEBUG: Generated script failed validation:\n%s\n", generatedScript)
		}

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
			generatedScript = c.cleanMarkdownCodeBlocks(generatedScript)
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
		output, execErr = tools.ExecuteShellCommand(context.Background(), generatedScript)
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
		output, execErr = tools.ExecuteShellCommand(context.Background(), tmpFile.Name())
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

// cleanMarkdownCodeBlocks removes markdown code block formatting if present
func (c *ShellCommand) cleanMarkdownCodeBlocks(code string) string {
	// Remove <think> blocks first
	if strings.Contains(code, "<think>") && strings.Contains(code, "</think>") {
		startIdx := strings.Index(code, "<think>")
		endIdx := strings.Index(code, "</think>")
		if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
			code = code[:startIdx] + code[endIdx+8:] // +8 for length of "</think>"
			code = strings.TrimSpace(code)
		}
	}

	// Check if the code contains markdown code blocks
	if strings.Contains(code, "```") {
		lines := strings.Split(code, "\n")
		var cleanedLines []string
		inCodeBlock := false

		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inCodeBlock = !inCodeBlock
				continue
			}
			if inCodeBlock || !strings.HasPrefix(strings.TrimSpace(line), "```") {
				cleanedLines = append(cleanedLines, line)
			}
		}

		return strings.TrimSpace(strings.Join(cleanedLines, "\n"))
	}

	return code
}

// isValidShellCode checks if the output looks like executable shell code
func (c *ShellCommand) isValidShellCode(code string) bool {
	// Check for common indicators that this is NOT shell code
	lowerCode := strings.ToLower(code)

	// Remove markdown code blocks if present
	if strings.HasPrefix(code, "```") {
		lines := strings.Split(code, "\n")
		var cleanedLines []string
		inCodeBlock := false
		for _, line := range lines {
			if strings.HasPrefix(line, "```") {
				inCodeBlock = !inCodeBlock
				continue
			}
			if inCodeBlock {
				cleanedLines = append(cleanedLines, line)
			}
		}
		code = strings.Join(cleanedLines, "\n")
		lowerCode = strings.ToLower(code)
	}

	// If empty after cleanup, it's not valid
	code = strings.TrimSpace(code)
	if code == "" {
		return false
	}

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
		"i'll",
		"i will",
		"let me",
	}

	for _, start := range invalidStarts {
		if strings.HasPrefix(lowerCode, start) {
			return false
		}
	}

	// Check if it looks like a command (has common shell command patterns)
	shellPatterns := []string{
		"git ", "cd ", "ls ", "echo ", "find ", "grep ", "awk ", "sed ",
		"cat ", "mkdir ", "rm ", "cp ", "mv ", "chmod ", "chown ",
		"#!/", "if ", "for ", "while ", "function ", "export ",
	}

	hasShellPattern := false
	for _, pattern := range shellPatterns {
		if strings.Contains(lowerCode, pattern) {
			hasShellPattern = true
			break
		}
	}

	// If it has shell patterns, it's likely valid
	if hasShellPattern {
		return true
	}

	// Check if it's a simple command (no spaces means it might be a single command like "pwd")
	if !strings.Contains(code, " ") && len(code) < 20 {
		return true
	}

	// Otherwise, be more strict about sentence patterns
	lines := strings.Split(code, "\n")
	sentenceCount := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip comments and empty lines
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Check for sentence-like patterns
		if (strings.Contains(trimmed, ". ") || strings.HasSuffix(trimmed, ".")) &&
			!strings.Contains(trimmed, "./") && !strings.Contains(trimmed, "...") {
			sentenceCount++
		}
	}

	// If more than half the lines look like sentences, it's probably not code
	nonCommentLines := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			nonCommentLines++
		}
	}

	if nonCommentLines > 0 && float64(sentenceCount)/float64(nonCommentLines) > 0.5 {
		return false
	}

	return true
}
