package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/agent_tools"
)

// ShellCommand handles the /shell slash command
// Usage: /shell <description-of-shell-command-to-generate>
// This command uses the fast model to generate shell commands from natural language descriptions
// and asks for user approval before execution

type ShellCommand struct{}

func (c *ShellCommand) Name() string {
	return "shell"
}

func (c *ShellCommand) Description() string {
	return "Generate and execute shell commands using the fast model with user approval"
}

func (c *ShellCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /shell <description-of-shell-command-to-generate>")
	}

	commandDescription := strings.Join(args, " ")
	
	// Create a new agent with the fast model
	fastAgent, err := agent.NewAgentWithModel(api.FastModel)
	if err != nil {
		return fmt.Errorf("failed to initialize fast model agent: %v", err)
	}

	// Create a prompt for the fast model to generate a shell command
	prompt := fmt.Sprintf(`
Please generate a shell command that matches this description:
"%s"

IMPORTANT:
- Output ONLY the shell command itself, nothing else
- Do not include any explanations, comments, or additional text
- The command should be executable as-is
- Use bash shell syntax
- Return ONLY the command string

Example:
If the description is "list all files in current directory", output: "ls -la"
If the description is "show disk usage", output: "df -h"
`, commandDescription)

	fmt.Printf("🤖 Generating shell command with fast model...\n")
	
	// Process the query with the fast model
	result, err := fastAgent.ProcessQuery(prompt)
	if err != nil {
		return fmt.Errorf("failed to generate shell command: %v", err)
	}

	// Clean up the result - remove any quotes or extra whitespace
	generatedCommand := strings.TrimSpace(result)
	generatedCommand = strings.Trim(generatedCommand, `"'`)

	if generatedCommand == "" {
		return fmt.Errorf("fast model did not generate a valid shell command")
	}

	fmt.Printf("✅ Generated command:\n")
	fmt.Printf("Command: %s\n", generatedCommand)
	fmt.Printf("\n")

	// Ask for user approval
	fmt.Printf("⚠️  Do you want to execute this command? (y/N): ")
	
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read user input: %v", err)
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		fmt.Printf("❌ Command execution cancelled by user\n")
		return nil
	}

	fmt.Printf("✅ Executing command...\n")
	fmt.Printf("=====================================\n")

	// Execute the shell command
	resultOutput, err := tools.ExecuteShellCommand(generatedCommand)
	if err != nil {
		return fmt.Errorf("command failed: %v\nOutput: %s", err, resultOutput)
	}

	fmt.Printf("✅ Command executed successfully:\n")
	fmt.Printf("Command: %s\n", generatedCommand)
	fmt.Printf("Output:\n%s\n", resultOutput)
	
	return nil
}