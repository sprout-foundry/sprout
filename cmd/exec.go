package cmd

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/spf13/cobra"
)

var (
	execSkipPrompt bool
	execModel      string
)

func init() {
	execCmd.Flags().BoolVar(&execSkipPrompt, "skip-prompt", false, "Skip user prompt (enhanced by safety analysis)")
	execCmd.Flags().StringVarP(&execModel, "model", "m", "", "Model name to use with the system")
}

var execCmd = &cobra.Command{
	Use:   "exec [description]",
	Short: "Command inference and execution",
	Long: `Command inference and execution using natural language processing.

Features:
- Command inference using reasoning
- Safety analysis and validation
- Real-time TUI integration with execution monitoring  
- Error handling and recovery mechanisms
- Shell detection and compatibility

The system uses language understanding to convert natural language
descriptions into precise shell commands with safety and reliability.

Examples:
  # Natural language to command conversion
  ledit exec "list all files in the current directory in long format"
  
  # Complex operations with better understanding
  ledit exec "find all go files in the project and show their sizes"
  
  # Enhanced safety with validation
  ledit exec "safely remove temporary files older than 7 days"`,
	Run: func(cmd *cobra.Command, args []string) {
		// Validate inputs
		if len(args) == 0 {
			cmd.Help()
			return
		}

		// Create agent directly
		chatAgent, err := agent.NewAgent()
		if err != nil {
			log.Fatalf("Failed to initialize agent: %v", err)
		}

		// Handle UI integration
		if ui.IsUIActive() {
			ui.PublishStatus("Analyzing command request")
		}

		userInput := strings.Join(args, " ")

		if ui.IsUIActive() {
			ui.Log(fmt.Sprintf("🔍 Analyzing request: %s", userInput))
		} else {
			fmt.Printf("🔍 Analyzing: %s\n", userInput)
		}

		// Use command inference system
		prompt := fmt.Sprintf("Convert the following user input into a safe, executable shell command. Analyze the request for safety and provide the most appropriate command. User input: '%s'", userInput)
		
		response, err := chatAgent.ProcessQueryWithContinuity(prompt)
		if err != nil {
			if ui.IsUIActive() {
				ui.Log(fmt.Sprintf("❌ Command inference failed: %v", err))
			}
			log.Fatalf("Command inference failed: %v", err)
		}

		// Extract command from response
		commandToRun := extractCommandFromResponse(response)
		
		if ui.IsUIActive() {
			ui.Log(fmt.Sprintf("💡 Proposed command: %s", commandToRun))
		} else {
			fmt.Printf("💡 Proposed command: %s\n", commandToRun)
		}

		// Confirmation using safety analysis
		if !execSkipPrompt {
			if ui.IsUIActive() {
				// Use user interaction via console
				fmt.Printf("Execute this command? %s [yes/no]: ", commandToRun)
				var confirmResponse string
				fmt.Scanln(&confirmResponse)
				if strings.ToLower(strings.TrimSpace(confirmResponse)) != "yes" {
					ui.Log("Execution cancelled by user")
					return
				}
			} else {
				fmt.Print("Execute this command? (y/n): ")
				var confirm string
				fmt.Scanln(&confirm)
				if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
					fmt.Println("Execution cancelled.")
					return
				}
			}
		}

		// Execute using shell execution
		if ui.IsUIActive() {
			ui.Log("🚀 Executing command...")
		}

		// Execute command directly
		shellCmd := exec.Command("sh", "-c", commandToRun)
		outputBytes, err := shellCmd.CombinedOutput()
		output := string(outputBytes)
		
		if ui.IsUIActive() {
			if err != nil {
				ui.Log(fmt.Sprintf("❌ Command failed: %v", err))
			} else {
				ui.Log("✅ Command executed successfully")
				if output != "" {
					ui.Log(fmt.Sprintf("Output: %s", output))
				}
			}
		} else {
			if err != nil {
				fmt.Printf("❌ Command failed: %v\n", err)
			} else {
				fmt.Printf("✅ Command executed successfully\n")
				if output != "" {
					fmt.Printf("Output:\n%s\n", output)
				}
			}
		}

		// Show statistics
		totalCost := chatAgent.GetTotalCost()
		if ui.IsUIActive() {
			if totalCost > 0 {
				ui.Log(fmt.Sprintf("💰 Total cost: $%.6f", totalCost))
			}
		} else {
			if totalCost > 0 {
				fmt.Printf("Cost: $%.6f\n", totalCost)
			}
		}
	},
}

// extractCommandFromResponse extracts the shell command from response
func extractCommandFromResponse(response string) string {
	lines := strings.Split(response, "\n")
	
	// Look for code blocks first
	inCodeBlock := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock && line != "" {
			return line
		}
	}
	
	// If no code block, look for lines that look like commands
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") && !strings.Contains(line, "command:") {
			// Simple heuristic: if it looks like a command, use it
			if strings.Contains(line, " ") || strings.Contains(line, "/") || strings.Contains(line, ".") {
				return line
			}
		}
	}
	
	// Fallback: return the whole response trimmed
	return strings.TrimSpace(response)
}

func detectCallerShell() string {
	// Allow explicit override via env
	if s := strings.TrimSpace(os.Getenv("LEDIT_SHELL")); s != "" {
		return s
	}
	// Try to infer from parent process on Linux
	if ppid := os.Getppid(); ppid > 1 {
		if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", ppid)); err == nil {
			name := strings.TrimSpace(string(b))
			if name != "" {
				return name
			}
		}
		if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", ppid)); err == nil {
			parts := strings.Split(string(b), "\x00")
			if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
				return parts[0]
			}
		}
	}
	// Fallback to login shell
	if s := strings.TrimSpace(os.Getenv("SHELL")); s != "" {
		return s
	}
	return "sh"
}

func executeShellCommand(command string) {
	shell := detectCallerShell()
	fmt.Printf("Executing with %s: %s\n", shell, command)
	cmd := exec.Command(shell, "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		fmt.Printf("Command finished with error: %v\n", err)
	}
}
