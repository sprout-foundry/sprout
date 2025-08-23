//go:build !agent2refactor

package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/tools"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
)

// validateBuild runs build validation after todo execution with intelligent error recovery
func validateBuild(ctx *SimplifiedAgentContext) error {
	ctx.Logger.LogProcessStep("🔍 Validating build after changes...")

	// Get build command from workspace
	workspaceFile, err := workspace.LoadWorkspaceFile()
	if err != nil {
		ctx.Logger.LogProcessStep("⚠️ No workspace file found, skipping build validation")
		return nil
	}

	buildCmd := strings.TrimSpace(workspaceFile.BuildCommand)
	if buildCmd == "" {
		ctx.Logger.LogProcessStep("⚠️ No build command configured, skipping validation")
		return nil
	}

	ctx.Logger.LogProcessStep(fmt.Sprintf("🏗️ Running build: %s", buildCmd))

	cmd := exec.Command("sh", "-c", buildCmd)
	output, err := cmd.CombinedOutput()

	if err != nil {
		failureMsg := string(output)
		ctx.Logger.LogProcessStep("❌ Build failed, analyzing with LLM for recovery...")

		// Ask LLM to fix the build failure directly
		fixErr := fixBuildFailure(ctx, buildCmd, failureMsg)
		if fixErr != nil {
			ctx.Logger.LogError(fmt.Errorf("LLM fix attempt failed: %w", fixErr))
			return fmt.Errorf("build validation failed and fix attempt unsuccessful: %s", failureMsg)
		}

		// Try the build again after the fix attempt
		ctx.Logger.LogProcessStep("🔄 Retrying build after fix...")
		retryCmd := exec.Command("sh", "-c", buildCmd)
		if retryOutput, retryErr := retryCmd.CombinedOutput(); retryErr != nil {
			return fmt.Errorf("build still fails after fix attempt: %s", string(retryOutput))
		}

		ctx.Logger.LogProcessStep("✅ Build validation passed after LLM fix!")
		return nil
	}

	ctx.Logger.LogProcessStep("✅ Build validation passed")
	return nil
}

// fixBuildFailure asks the LLM to fix the build failure directly using available tools
func fixBuildFailure(ctx *SimplifiedAgentContext, buildCmd, failureMsg string) error {
	ctx.Logger.LogProcessStep("🔧 Asking LLM to fix build failure...")

	maxIterations := 12
	messages := []prompts.Message{
		{Role: "system", Content: fmt.Sprintf(`You are an expert software engineer troubleshooting a build failure.

%s

Use these tools to diagnose and fix build issues. Read files to understand errors, edit files to fix syntax problems, and test your changes.`, llm.FormatToolsForPrompt())},
		{Role: "user", Content: fmt.Sprintf(`The build command '%s' failed with this error:

BUILD ERROR:
%s

Please fix this build failure by using the available tools. Read files to understand the error, edit files to fix syntax issues, and test your fixes.`, buildCmd, failureMsg)},
	}

	for iteration := 1; iteration <= maxIterations; iteration++ {
		ctx.Logger.LogProcessStep(fmt.Sprintf("🔄 Build fix attempt %d/%d", iteration, maxIterations))

		response, tokenUsage, err := llm.GetLLMResponse(ctx.Config.OrchestrationModel, messages, "", ctx.Config, 60*time.Second)
		if err != nil {
			return fmt.Errorf("LLM fix request failed: %w", err)
		}

		// Track token usage and cost
		trackTokenUsage(ctx, tokenUsage, ctx.Config.OrchestrationModel)

		ctx.Logger.LogProcessStep(fmt.Sprintf("LLM response: %s", response))

		// Parse and execute tool calls from the response
		toolCalls, err := llm.ParseToolCalls(response)
		if err != nil || len(toolCalls) == 0 {
			ctx.Logger.LogProcessStep("⚠️ No tool calls found in response")

			// Check if the response indicates the build is fixed
			responseLower := strings.ToLower(response)
			if strings.Contains(responseLower, "build is now fixed") ||
				strings.Contains(responseLower, "build has been successfully fixed") ||
				strings.Contains(responseLower, "build is fixed") ||
				strings.Contains(responseLower, "successfully fixed") {
				ctx.Logger.LogProcessStep("🔍 Model indicates build is fixed, testing...")

				// Test the build
				cmd := exec.Command("sh", "-c", buildCmd)
				if output, err := cmd.CombinedOutput(); err == nil {
					ctx.Logger.LogProcessStep("✅ Build confirmed working! Issue resolved.")
					return nil
				} else {
					newFailureMsg := string(output)
					ctx.Logger.LogProcessStep(fmt.Sprintf("❌ Build still failing despite model's claim: %s", newFailureMsg))
					messages = append(messages, prompts.Message{Role: "assistant", Content: response})
					messages = append(messages, prompts.Message{Role: "user", Content: fmt.Sprintf("You said the build is fixed, but it's still failing: %s\nPlease continue fixing.", newFailureMsg)})
					continue
				}
			}

			// Add the response to conversation and continue
			messages = append(messages, prompts.Message{Role: "assistant", Content: response})
			continue
		}

		// Execute the tool calls and collect results
		var toolResults []string
		allToolsSucceeded := true

		for _, toolCall := range toolCalls {
			ctx.Logger.LogProcessStep(fmt.Sprintf("🔧 Executing tool: %s", toolCall.Function.Name))

			// Execute the tool call using enhanced executor
			result, err := executeEnhancedTool(toolCall, ctx.Config, ctx.Logger)
			if err != nil {
				ctx.Logger.LogError(fmt.Errorf("tool execution failed: %w", err))
				result = fmt.Sprintf("Tool execution failed: %v", err)
				allToolsSucceeded = false
			}

			ctx.Logger.LogProcessStep(fmt.Sprintf("✅ Tool result: %s", result))
			toolResults = append(toolResults, fmt.Sprintf("Tool %s result: %s", toolCall.Function.Name, result))
		}

		// Add tool results to conversation
		toolResultsText := strings.Join(toolResults, "\n")
		messages = append(messages, prompts.Message{Role: "assistant", Content: response})
		messages = append(messages, prompts.Message{Role: "user", Content: fmt.Sprintf("Tool execution results:\n%s\n\nIf the build is now fixed, respond with 'BUILD_FIXED'. If you need to make more changes, continue with additional tool calls.", toolResultsText)})

		// Test the build after tool execution
		if allToolsSucceeded {
			ctx.Logger.LogProcessStep("🏗️ Testing build after fixes...")
			cmd := exec.Command("sh", "-c", buildCmd)
			if output, err := cmd.CombinedOutput(); err == nil {
				ctx.Logger.LogProcessStep("✅ Build succeeded! Issue resolved.")
				return nil
			} else {
				newFailureMsg := string(output)
				ctx.Logger.LogProcessStep(fmt.Sprintf("❌ Build still failing: %s", newFailureMsg))
				messages = append(messages, prompts.Message{Role: "user", Content: fmt.Sprintf("Build still failing with: %s\nPlease continue fixing.", newFailureMsg)})
			}
		}
	}

	return fmt.Errorf("build fix failed after %d attempts", maxIterations)
}

// executeEnhancedTool executes a tool call using the unified tool executor
func executeEnhancedTool(toolCall llm.ToolCall, cfg *config.Config, logger *utils.Logger) (string, error) {
	// Debug logging
	logger.LogProcessStep(fmt.Sprintf("Debug: Tool name: %s", toolCall.Function.Name))
	logger.LogProcessStep(fmt.Sprintf("Debug: Arguments string: '%s'", toolCall.Function.Arguments))

	// Convert llm.ToolCall to types.ToolCall for the unified executor
	typesToolCall := types.ToolCall{
		ID:   toolCall.ID,
		Type: toolCall.Type,
		Function: types.ToolCallFunction{
			Name:      toolCall.Function.Name,
			Arguments: toolCall.Function.Arguments,
		},
	}

	// Use the unified tool executor
	result, err := tools.ExecuteToolCall(context.Background(), typesToolCall)
	if err != nil {
		return "", err
	}

	if !result.Success {
		return "", fmt.Errorf("tool execution failed: %v", strings.Join(result.Errors, "; "))
	}

	// Convert result to string format expected by existing code
	if output, ok := result.Output.(string); ok {
		return output, nil
	}

	// Fallback for non-string outputs
	return fmt.Sprintf("%v", result.Output), nil
}
