package cmd

import (
	"bufio"
	"fmt"
	"io"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/workspace"
	"log"
	"os"
	"strings"
	"time"

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
	fmt.Println("\nAssistant: OK, I understand. I will use the context provided with each question to give my answer. What is your question?")

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

		// Load fresh workspace context for each question
		fmt.Println("\nAnalyzing workspace to answer your question...")
		workspaceContext := workspace.GetWorkspaceContext(question, cfg)
		if workspaceContext == "" {
			fmt.Println("Warning: Could not load workspace context for this question.")
		}
		fmt.Println("Workspace analysis complete.")

		// Combine question and context into a single user message
		userPrompt := fmt.Sprintf("My question is: '%s'\n\nHere is the relevant context from my workspace:\n\n--- Workspace Context ---\n%s\n\n--- End Workspace Context ---", question, workspaceContext)

		// Add combined user message to history
		messages = append(messages, prompts.Message{Role: "user", Content: userPrompt})

		// Check token limit and ask for confirmation if needed
		var totalInputTokens int
		for _, msg := range messages {
			totalInputTokens += llm.EstimateTokens(msg.Content)
		}

		if totalInputTokens > llm.DefaultTokenLimit && !cfg.SkipPrompt {
			fmt.Printf("\nThis request will take approximately %d tokens with model %s.\n\n", totalInputTokens, cfg.EditingModel)
			fmt.Printf("NOTE: This request at %d tokens is over the default token limit of %d, do you want to continue? (y/n): ", totalInputTokens, llm.DefaultTokenLimit)

			confirm, err := reader.ReadString('\n') // Use the same buffered reader
			if err != nil {
				log.Fatalf("Error reading input: %v", err)
			}

			if strings.TrimSpace(strings.ToLower(confirm)) != "y" {
				fmt.Println("Operation cancelled by user.")
				messages = messages[:len(messages)-1] // remove last question
				question = ""
				continue
			}
		}

		fmt.Print("\nAssistant: ")

		// Use a string builder to capture the response for history
		var responseBuilder strings.Builder

		// Use a multiwriter to write to both stdout and the builder
		// Note: Formatting of the LLM response content is handled by the LLM streaming function,
		// which is not part of this file. This file only directs the stream to stdout.
		writer := io.MultiWriter(os.Stdout, &responseBuilder)

		// Duplicate the config to avoid modifying the original and set the skipPrompt flag to true
		skipPromptConfig := &config.Config{}
		*skipPromptConfig = *cfg
		skipPromptConfig.SkipPrompt = true

		// Fix: Added 'false' for useSearchGrounding as it's a question about the workspace
		_, err := llm.GetLLMResponseStream(skipPromptConfig.EditingModel, messages, "question", skipPromptConfig, 3*time.Minute, writer, false)
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
		fmt.Println()

		question = "" // Reset for next loop
	}
	fmt.Println("\nGoodbye!")
}

// readUserInput reads a single line of input using a buffered reader.
// This replaces the complex raw mode handling with standard line input.
func readUserInput(reader *bufio.Reader) string {
	fmt.Print("\nYou: ")
	input, err := reader.ReadString('\n')
	if err != nil {
		// If there's an error (like EOF, Ctrl+D), treat it as an exit command
		if err == io.EOF {
			return "exit"
		}
		// Log other errors but still return "exit" to stop the loop
		log.Printf("Error reading input: %v", err)
		return "exit"
	}
	return strings.TrimSpace(input)
}

func init() {
	questionCmd.Flags().StringVarP(&model, "model", "m", "", "Model name to use with the LLM")
}
