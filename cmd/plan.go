// Plan command for ledit - Seamless planning and execution using agent framework
package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	planModel       string
	planProvider    string
	planOutputFile  string
	planContinue    bool
	planCreateTodos bool
)

func init() {
	planCmd.Flags().StringVarP(&planModel, "model", "m", "", "Model name for planning")
	planCmd.Flags().StringVarP(&planProvider, "provider", "p", "", "Provider to use")
	planCmd.Flags().StringVarP(&planOutputFile, "output", "o", "", "Output file for the plan (default: plan.md)")
	planCmd.Flags().BoolVarP(&planContinue, "continue", "c", false, "Continue from an existing plan file")
	planCmd.Flags().BoolVarP(&planCreateTodos, "todos", "t", true, "Create todos from plan items during planning")
}

var planCmd = &cobra.Command{
	Use:   "plan [initial_idea]",
	Short: "Seamless planning and execution mode",
	Long: `Planning mode that combines collaborative planning with autonomous execution.

The agent will:
1. Collaboratively plan with you (ask questions, explore codebase)
2. Present a detailed plan for approval
3. Once approved, autonomously execute using subagent delegation
4. Report progress and complete all tasks

WORKFLOW:
  Phase 1 - Planning (Interactive):
  • Understand requirements through questions
  • Explore codebase with tools (read_file, search_files)
  • Create detailed implementation plan
  • Present plan for your approval
  • Wait for your go-ahead

  Phase 2 - Execution (Autonomous):
  • Execute plan using subagent delegation
  • Validate each task (build, test, review)
  • Report progress regularly
  • Complete all tasks automatically

Examples:
  # Start planning session
  ledit plan "Add user authentication"

  # Continue from existing plan
  ledit plan --continue plan.md

  # Use specific model
  ledit plan -p openrouter -m "qwen/qwen3-coder-30b" "Build REST API"

The agent will seamlessly transition from planning to execution upon your approval.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPlanMode(args)
	},
}

// runPlanMode executes the seamless planning and execution session
func runPlanMode(args []string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("interactive planning mode requires a terminal (TTY)")
	}

	fmt.Print(`
╔══════════════════════════════════════════════════════════════════════╗
║              LEDIT PLANNING & EXECUTION MODE                         ║
║                                                                      ║
║  Phase 1: Collaborative planning (ask questions, explore code)        ║
║  Phase 2: Autonomous execution (subagent delegation after approval)   ║
╚══════════════════════════════════════════════════════════════════════╝
`)

	// Create agent with planning system prompt
	chatAgent, err := createPlanningAgent()
	if err != nil {
		return fmt.Errorf("failed to initialize planning agent: %w", err)
	}

	// Set environment for agent
	_ = os.Setenv("LEDIT_FROM_AGENT", "1")

	// Determine initial query
	var query string

	if planContinue {
		// Load existing plan and refine it
		if len(args) > 0 && filesystem.FileExists(args[0]) {
			planContent, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("failed to load plan: %w", err)
			}
			query = fmt.Sprintf("Continue this planning session. Here is the current state:\n\n%s", string(planContent))
			fmt.Printf("📄 Loaded existing plan from: %s\n\n", args[0])
		} else if planOutputFile != "" && filesystem.FileExists(planOutputFile) {
			planContent, err := os.ReadFile(planOutputFile)
			if err != nil {
				return fmt.Errorf("failed to load plan: %w", err)
			}
			query = fmt.Sprintf("Continue this planning session. Here is the current state:\n\n%s", string(planContent))
			fmt.Printf("📄 Loaded existing plan from: %s\n\n", planOutputFile)
		} else {
			return fmt.Errorf("no existing plan file found. Path specified but file doesn't exist.")
		}
	} else if len(args) > 0 {
		query = args[0]
	} else {
		// No initial query - prompt user
		fmt.Print("What would you like to plan? Describe your idea or feature:\n\n")
		reader := bufio.NewReader(os.Stdin)
		userQuery, _ := reader.ReadString('\n')
		query = strings.TrimSpace(userQuery)
		if query == "" {
			return fmt.Errorf("no idea provided")
		}
		fmt.Println()
	}

	// Run the seamless planning and execution loop
	ctx := context.Background()
	if err := runSeamlessPlanning(ctx, chatAgent, query); err != nil {
		return fmt.Errorf("planning session failed: %w", err)
	}

	return nil
}

// createPlanningAgent creates an agent configured for planning
func createPlanningAgent() (*agent.Agent, error) {
	var chatAgent *agent.Agent
	var err error

	if planProvider != "" && planModel != "" {
		modelWithProvider := fmt.Sprintf("%s:%s", planProvider, planModel)
		chatAgent, err = agent.NewAgentWithModel(modelWithProvider)
	} else if planModel != "" {
		chatAgent, err = agent.NewAgentWithModel(planModel)
	} else {
		chatAgent, err = agent.NewAgent()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to initialize agent: %w", err)
	}

	// Set planning-focused system prompt (now includes execution workflow)
	planningPrompt, err := agent.GetEmbeddedPlanningPrompt(planCreateTodos)
	if err != nil {
		return nil, fmt.Errorf("failed to load planning prompt: %w", err)
	}

	chatAgent.SetSystemPrompt(planningPrompt)
	chatAgent.SetMaxIterations(1000)

	return chatAgent, nil
}

// runSeamlessPlanning runs the seamless planning and execution loop
func runSeamlessPlanning(ctx context.Context, chatAgent *agent.Agent, initialQuery string) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Printf("🎯 Starting planning session: %s\n\n", initialQuery)
	fmt.Println("💡 I'll collaborate with you to create a plan, then execute it once you approve.")
	fmt.Println("   Just type your responses. When ready, I'll ask for your approval.")

	currentQuery := initialQuery

	for {
		fmt.Println("\n" + strings.Repeat("─", 60))
		fmt.Println("🤖 Processing...")

		// Process query using the agent (which handles tools, streaming, etc.)
		_, err := chatAgent.ProcessQueryWithContinuity(currentQuery)
		if err != nil {
			fmt.Printf("⚠️  Agent error: %v\n", err)
		}

		// Print summary after response
		summary := chatAgent.GenerateConversationSummary()
		if summary != "" {
			fmt.Printf("\n%s\n", summary)
		}

		// Get user input for next iteration
		fmt.Println("\n" + strings.Repeat("─", 60))
		fmt.Print("💬 Your response: ")

		userInput, _ := reader.ReadString('\n')
		userInput = strings.TrimSpace(userInput)

		// Handle special commands
		switch strings.ToLower(userInput) {
		case "exit", "quit", "q":
			fmt.Println("\n👋 Planning session ended.")
			return nil
		case "":
			// Continue without additional input - agent should decide next action
			currentQuery = "Please continue. If we're in the planning phase, continue gathering information or present the plan if ready. If in execution phase, continue with the next task."
		default:
			// Set user input as next query
			currentQuery = userInput
		}

		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}
