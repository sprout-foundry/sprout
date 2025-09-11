package commands

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/agent_tools"
)

// ExecCommand handles the /exec slash command
// Usage: /exec <shell-command-to-execute>
type ExecCommand struct{}

func (c *ExecCommand) Name() string {
	return "exec"
}

func (c *ExecCommand) Description() string {
	return "Execute a shell command directly"
}

func (c *ExecCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /exec <shell-command-to-execute>")
	}

	command := strings.Join(args, " ")
	
	// Execute the shell command
	result, err := tools.ExecuteShellCommand(command)
	if err != nil {
		return fmt.Errorf("command failed: %v\nOutput: %s", err, result)
	}

	fmt.Printf("✅ Command executed successfully:\n")
	fmt.Printf("Command: %s\n", command)
	fmt.Printf("Output:\n%s\n", result)
	
	return nil
}

// IsShellCommand checks if a prompt starts with common shell tools
func IsShellCommand(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	
	// Common shell command prefixes
	shellPrefixes := []string{
		"ls", "cd", "pwd", "cat", "echo", "grep", "find", "git",
		"mkdir", "rm", "cp", "mv", "touch", "chmod", "chown",
		"curl", "wget", "ssh", "scp", "rsync", "tar", "zip",
		"go ", "python", "node", "npm", "yarn", "docker", "kubectl",
		"make", "gcc", "g++", "clang", "javac", "java", "rustc",
		"cargo", "dotnet", "php", "ruby", "perl", "bash", "zsh",
		"fish", "pwsh", "powershell", "cmd", "winget", "scoop",
	}
	
	for _, prefix := range shellPrefixes {
		if strings.HasPrefix(prompt, prefix+" ") || prompt == prefix {
			return true
		}
	}
	
	return false
}

// ExecuteShellCommandDirectly executes a shell command directly and returns the result
func ExecuteShellCommandDirectly(command string) (string, error) {
	return tools.ExecuteShellCommand(command)
}