// question.go provides utilities for the cmd package. It contains functions and helpers used across
// the codebase. This comment summarizes the file’s purpose and typical usage.

package cmd

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/workspace"

	"github.com/spf13/cobra"
	// Removed golang.org/x/term as we are no longer using raw mode for input
)

var questionCmd = &cobra.Command{
	Use:     "question [initial_question]",
	Aliases: []string{"q"},
	Short:   "Ask a question about the workspace in an interactive chat",
	Long:    `Loads workspace information and starts an interactive chat session. You can ask questions about your codebase, and an LLM will provide answers based on the workspace context.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.LoadOrInitConfig(skipPrompt)
		if err != nil {
			log.Fatalf("Failed to load configuration: %v. Please run 'ledit init'.", err)
		}

		if model != "" {
			cfg.EditingModel = model
		}

		initialQuestion := strings.Join(args, " ")
		interactiveQuestionLoop(cfg, initialQuestion)
	},
}

func interactiveQuestionLoop(cfg *config.Config, initialQuestion string) {
	// Keep chat history for context
	var messages []prompts.Message
	messages = append(messages, prompts.Message{
		Role: "system",
		Content: "You are a helpful AI assistant that answers questions about a software project. " +
			"For each question, I will provide what I believe to be the relevant context from the codebase. " +
			"Provide clear and concise answers. The user is in an interactive chat, so keep responses to a reasonable length.",
	})
	messages = append(messages, prompts.Message{
		Role:    "assistant",
		Content: "OK, I understand. I will use the context provided with each question to give my answer. What is your question?",
	})

	// Print the initial assistant message
	ui.Out().Print("\nAssistant: OK, I understand. I will use the context provided with each question to give my answer. What is your question?\n")

	question := initialQuestion

	// Use a buffered reader for standard line input, which handles tabs and editing correctly.
	// Removed raw mode handling as it was causing issues with terminal formatting like tabs.
	reader := bufio.NewReader(os.Stdin)

	for {
		if question == "" {
			// Use the simplified readUserInput function
			question = readUserInput(reader)
		}

		if question == "exit" || question == "quit" { // Special command to exit
			break
		}

		var workspaceContext string
		var userPrompt string

		// Load fresh workspace context for each question, unless skipping is requested
		// Default behavior: include workspace context (feature flag can be added later)
		if true { // Always include context for now
			ui.ShowProgressWithDetails("🔍 Analyzing workspace...", "Analyzing workspace to answer your question")
			workspaceContext = workspace.GetWorkspaceContext(question, cfg)
			if workspaceContext == "" {
				ui.Out().Print("Warning: Could not load workspace context for this question.\n")
			}
			ui.ShowProgressWithDetails("✅ Ready to answer", "Workspace analysis complete")
			// Combine question and context into a single user message
			userPrompt = fmt.Sprintf("My question is: '%s'\n\nHere is the relevant context from my workspace:\n\n--- Workspace Context ---\n%s\n\n--- End Workspace Context ---", question, workspaceContext)
		}

		// Add combined user message to history
		messages = append(messages, prompts.Message{Role: "user", Content: userPrompt})

		// Check token limit and ask for confirmation if needed
		var totalInputTokens int
		for _, msg := range messages {
			totalInputTokens += llm.EstimateTokens(llm.GetMessageText(msg.Content))
		}

		if totalInputTokens > llm.DefaultTokenLimit && !cfg.SkipPrompt {
			ui.Out().Printf("\nThis request will take approximately %d tokens with model %s.\n\n", totalInputTokens, cfg.EditingModel)
			ui.Out().Printf("NOTE: This request at %d tokens is over the default token limit of %d, do you want to continue? (y/n): ", totalInputTokens, llm.DefaultTokenLimit)

			confirm, err := reader.ReadString('\n') // Use the same buffered reader
			if err != nil {
				log.Fatalf("Error reading input: %v", err)
			}

			if strings.TrimSpace(strings.ToLower(confirm)) != "y" {
				ui.Out().Print("Operation cancelled by user.\n")
				messages = messages[:len(messages)-1] // remove last question
				question = ""
				continue
			}
		}

		ui.Out().Print("\nAssistant: ")

		// Use a string builder to capture the response for history
		var responseBuilder strings.Builder

		// Stream to UI when enabled so users see live tokens; fallback to stdout otherwise
		var writer io.Writer
		if ui.IsUIActive() {
			writer = io.MultiWriter(ui.NewStreamWriter(), &responseBuilder)
		} else {
			writer = io.MultiWriter(os.Stdout, &responseBuilder)
		}

		// Duplicate the config to avoid modifying the original and set the skipPrompt flag to true
		skipPromptConfig := &config.Config{}
		*skipPromptConfig = *cfg
		skipPromptConfig.SkipPrompt = true

		_, err := llm.GetLLMResponseStream(skipPromptConfig.EditingModel, messages, "question", skipPromptConfig, 3*time.Minute, writer)
		if err != nil {
			// Error is already printed by the LLM function
			// Remove the failed user message from history
			if len(messages) > 0 {
				messages = messages[:len(messages)-1]
			}
		} else {
			// Add assistant response to history
			messages = append(messages, prompts.Message{Role: "assistant", Content: responseBuilder.String()})
		}
		ui.Out().Print("\n")

		question = "" // Reset for next loop
	}
	ui.Out().Print("\nGoodbye!\n")
}

// readUserInput reads a single line from the provided reader and trims whitespace.
// Returns "exit" on EOF or read error to gracefully terminate the loop.
func readUserInput(reader *bufio.Reader) string {
	input, err := reader.ReadString('\n')
	if err == io.EOF {
		return "exit"
	}
	if err != nil {
		log.Printf("Error reading input: %v", err)
		return "exit"
	}
	return strings.TrimSpace(input)
}

func init() {
	questionCmd.Flags().StringVarP(&model, "model", "m", "", "Model name to use with the LLM")
}
