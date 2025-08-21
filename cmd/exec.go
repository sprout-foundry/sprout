package cmd

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/spf13/cobra"
)

var (
	execSkipPrompt bool
	execModel      string
)

func init() {
	execCmd.Flags().BoolVar(&execSkipPrompt, "skip-prompt", false, "Skip user prompt for executing the command")
	execCmd.Flags().StringVarP(&execModel, "model", "m", "", "Model name to use with the LLM")
}

var execCmd = &cobra.Command{
	Use:   "exec [description]",
	Short: "Infer and execute a shell command from your description",
	Long: `Executes a shell command by inferring it from your plain text description using an LLM.

Examples:
  ledit exec "list all files in the current directory in long format"
  ledit exec "find all go files in the project"`,
	Run: func(cmd *cobra.Command, args []string) {
		// Validate inputs
		if len(args) == 0 {
			cmd.Help()
			return
		}

		// Initialize logger and config
		logger := utils.GetLogger(execSkipPrompt)
		cfg, err := config.LoadOrInitConfig(execSkipPrompt)
		if err != nil {
			log.Fatalf("Failed to load configuration: %v. Please run 'ledit init'.", err)
		}
		model := cfg.WorkspaceModel
		if execModel != "" {
			model = execModel
		}

		// Build the user input to infer from
		userInput := strings.Join(args, " ")

		// Ask LLM to infer/confirm the shell command from the user's input (NL or raw)
		logger.LogProcessStep("Generating command from input...")
		prompt := fmt.Sprintf("You are an expert in shell commands. Convert the following user input into a single, executable shell command. If the input is already a valid shell command, return it unchanged. Only output the shell command itself, with no explanation, code fences, or extra text.\n\nUser input: '%s'", userInput)
		messages := []prompts.Message{
			{Role: "system", Content: "You are an expert at generating shell commands from plain text descriptions. You only output the raw command, with no additional text or explanation."},
			{Role: "user", Content: prompt},
		}
		commandToRun, _, err := llm.GetLLMResponse(model, messages, "exec_intent", cfg, 30*time.Second)
		if err != nil {
			log.Fatalf("Failed to get command from LLM: %v", err)
		}
		commandToRun = strings.TrimSpace(commandToRun)
		// Strip potential code fences
		commandToRun = strings.TrimPrefix(commandToRun, "```sh")
		commandToRun = strings.TrimPrefix(commandToRun, "```bash")
		commandToRun = strings.TrimPrefix(commandToRun, "```")
		commandToRun = strings.TrimSuffix(commandToRun, "```")
		commandToRun = strings.TrimSpace(commandToRun)

		// Confirm execution via logger's AskForConfirmation
		logger.LogProcessStep(fmt.Sprintf("Proposed command: %s", commandToRun))
		if !logger.AskForConfirmation("Execute this command?", true, false) {
			logger.LogProcessStep("Execution cancelled.")
			return
		}

		executeShellCommand(commandToRun)
	},
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
