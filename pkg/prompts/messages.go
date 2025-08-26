package prompts

import (
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/types"
	"github.com/fatih/color"
)

// --- Config Prompts ---
func ConfigLoadFailed(err error) string {
	return fmt.Sprintf("Failed to load config: %v. Using default values.", err)
}

func EnterEditingModel(defaultModel string) string {
	return fmt.Sprintf("Enter your preferred editing model (e.g., %s): ", defaultModel)
}

func EnterSummaryModel(defaultModel string) string {
	return fmt.Sprintf("Enter your preferred summary model (e.g., %s): ", defaultModel)
}

func EnterWorkspaceModel(defaultModel string) string {
	return fmt.Sprintf("Enter your preferred workspace analysis model (e.g., %s): ", defaultModel)
}

func EnterOrchestrationModel(defaultModel string) string {
	return fmt.Sprintf("Enter your preferred orchestration model (e.g., %s): ", defaultModel)
}

func TrackGitPrompt() string {
	return "Automatically track changes with Git? (yes/no): "
}

func EnterLLMProvider(defaultProvider string) string {
	return fmt.Sprintf("Enter your preferred LLM provider (e.g., openai, gemini, ollama) (default: %s): ", defaultProvider)
}

func EnableSecurityChecksPrompt() string {
	return "Enable checking for leaked keys and passwords in all files?\n Note that this can take a long time when enabled (yes/no): "
}

func PotentialSecurityConcernsFound(filePath string, concerns string, snippetInQuestion string) string {
	return fmt.Sprintf("Potential security concerns detected in the file %s:\n- %s\nSnippet in question:\n%s\nIs this a valid concern that we should avoid summarization for this file?", filePath, concerns, snippetInQuestion)
}

func NoConfigFound() string {
	return "No config found. Creating a new one."
}

func ConfigSaved(path string) string {
	return fmt.Sprintf("Config saved to %s", path)
}

// --- Code Generation Prompts ---
func InstructionsRequired() string {
	return "Instructions are required for the 'code' command. Please provide a description of the changes you want to make."
}

func ProcessingCodeGeneration() string {
	return "Processing code generation..."
}

func CodeGenerationError(err error) string {
	return fmt.Sprintf("Error during code generation: %v", err)
}

func CodeGenerationFinished(duration time.Duration) string {
	return fmt.Sprintf("Code generation finished in %s\n", duration)
}

// --- Script Generation Messages ---

func GeneratingSetupScript() string {
	return "--- Generating setup script ---"
}

func NoSetupScriptFound() string {
	return "--- No setup.sh file found after generation. Skipping setup. ---"
}

func SetupScriptEmpty() string {
	return "--- setup.sh is empty after generation. Skipping setup. ---"
}

func GeneratedSetupScriptHeader() string {
	return "--- Generated setup script (setup.sh): ---"
}

func ScriptSeparator() string {
	return "---------------"
}

func RunningSetupScript() string {
	return "--- Running setup script ---"
}

func SetupScriptOutput(output []byte) string {
	return fmt.Sprintf("--- Setup Script Output ---\n%s\n--------------------------------", string(output))
}

func SetupSuccessful() string {
	return "--- Setup successful ---"
}

func GeneratingValidationScript() string {
	return "--- Generating validation script ---"
}

func NoValidationScriptFound() string {
	return "⚠️ No validate.sh file found after generation. Skipping validation."
}

func ValidationScriptEmpty() string {
	return "⚠️ validate.sh is empty after generation. Skipping validation."
}

func GeneratedValidationScriptHeader() string {
	return "--- Generated validation script (validate.sh): ---"
}

func RunningValidationScript() string {
	return "--- Running validation script ---"
}

func ValidationScriptOutput(output []byte) string {
	return fmt.Sprintf("--- Validation Script Output ---\n%s\n--------------------------------", string(output))
}

func ValidationSuccessful() string {
	return "--- Validation successful ---"
}

// --- LLM Context Builder Messages ---

func UsingModel(modelName string) string {
	return fmt.Sprintf("Using model: %s\n", modelName)
}

func LLMContextRequest(reqType, query string) string {
	return fmt.Sprintf("LLM requested additional context via '%s': %s\n", reqType, query)
}

func LLMUserQuestion(query string) string {
	return fmt.Sprintf("LLM has a question for you:\n%s\n> ", query)
}

func LLMFileRequest(filename string) string {
	return fmt.Sprintf("LLM requested file content for: %s\n", filename)
}

func LLMShellCommandRequest(command string) string {
	return fmt.Sprintf("LLM requested to run shell command: %s\n", command)
}

func LLMShellWarning() string {
	return "WARNING: The LLM wants to run a shell command. This could be dangerous."
}

func LLMShellConfirmation() string {
	return "Do you want to allow this? (y/n): "
}

func LLMContextParseError(err error, rawResponse string) string {
	return fmt.Sprintf("Failed to parse context requests from response: %v\nRaw response: %s\n", err, rawResponse)
}

func LLMNoContextRequests() string {
	return "No context requests found in response. Continuing with code generation."
}

func LLMContextRequestsFound(count int) string {
	return fmt.Sprintf("Context requests found: %d\n", count)
}

func LLMContextRequestError(err error) string {
	return fmt.Sprintf("Error handling context request: %v\n", err)
}

func LLMAddingContext(context string) string {
	return fmt.Sprintf("Adding new context to conversation: %s\n", context)
}

func LLMMaxContextRequestsReached() string {
	return "Maximum number of context requests reached. Forcing code generation."
}

func LLMShellSkippingPrompt() string {
	return "--- Skipping user prompt for shell command due to --skip-prompt flag. Performing risk analysis... ---"
}

func LLMScriptAnalysisFailed(err error) string {
	return fmt.Sprintf("--- Script risk analysis failed: %v. Will not execute automatically. ---", err)
}

func LLMScriptNotRisky() string {
	return "--- Script analysis determined it is NOT risky. Executing automatically."
}

func LLMScriptRisky(analysis string) string {
	return fmt.Sprintf("--- Script analysis determined it IS risky. Analysis: %s. User confirmation required. ---", analysis)
}

// --- Workspace Prompts ---
func LoadingWorkspaceData() string {
	return "--- Loading in workspace data ---"
}

func URLFetchError(url string, err error) string {
	return fmt.Sprintf("Could not fetch content from URL %s: %v. Continuing without it.\n", url, err)
}

func FileLoadError(path string, err error) string {
	return fmt.Sprintf("Could not load content from path %s: %v. Continuing without it.\n", path, err)
}

// Renamed and updated prompt for security checks
func PerformingSecurityCheck() string {
	return "Performing regex-based security check for leaked credential patterns. This may take a moment..."
}

func SkippingLLMSummarizationDueToSecurity(filename string) string {
	return fmt.Sprintf("File %s contains confirmed security concerns. Skipping local summarization.", filename)
}

// --- Editor Prompts ---
func ModelReturned(modelName, content string) string {
	return fmt.Sprintf("%s model returned:\n%s\n", modelName, content)
}

func NoCodeBlocksParsed() string {
	return "⚠️ No code blocks parsed from the response."
}

func NoChangesDetected(filename string) string {
	return fmt.Sprintf("No changes detected in %s\n", filename)
}

func OriginalFileHeader(filename string) string {
	return fmt.Sprintf("--- Original %s\n", filename)
}

func UpdatedFileHeader(filename string) string {
	return fmt.Sprintf("+++ Updated %s\n", filename)
}

func ApplyChangesPrompt(filename string) string {
	return fmt.Sprintf("Do you want to apply the changes to %s? (y/n/e for edit): ", filename)
}

func ChangesApplied(filename string) string {
	return fmt.Sprintf("Changes applied to %s\n", filename)
}

func ChangesNotApplied(filename string) string {
	return fmt.Sprintf("Changes not applied to %s\n", filename)
}

func EnterDescriptionPrompt(filename string) string {
	return fmt.Sprintf("Enter a brief description for the changes to %s: ", filename)
}

func ProcessedInstructionsSeparator(instructions string) string {
	return fmt.Sprintf("------\n%s\n-------\n", instructions)
}

func PerformingSearch(query string) string {
	return fmt.Sprintf("Performing Jina AI search for query: \"%s\"\n", query)
}

func SearchError(query string, err error) string {
	return fmt.Sprintf("Error fetching Jina AI search results for \"%s\": %v. Replacing with empty string.\n", query, err)
}

// --- LLM API Messages ---

func TokenEstimate(tokens int, modelName string) string {
	return fmt.Sprintf("Tokens estimated: %d (model: %s)\n", tokens, modelName)
}

func TokenUsage(promptTokens, completionTokens, totalTokens int, modelName string, cost float64) string {
	if cost > 0 {
		return fmt.Sprintf("Tokens used: %d input + %d output = %d total (model: %s, cost: $%.6f)\n",
			promptTokens, completionTokens, totalTokens, modelName, cost)
	}
	return fmt.Sprintf("Tokens used: %d input + %d output = %d total (model: %s)\n",
		promptTokens, completionTokens, totalTokens, modelName)
}

func TokenLimitWarning(currentTokens, defaultLimit int) string {
	return fmt.Sprintf("NOTE: This request at %d tokens is over the default token limit of %d, do you want to continue? (y/n): ", currentTokens, defaultLimit)
}

// --- General User Interaction Prompts ---
func OperationCancelled() string {
	return "Operation cancelled."
}

func ContinuingRequest() string {
	return "Continuing request..."
}

// --- LLM API Error Prompts ---
func APIKeyError(err error) string {
	return fmt.Sprintf("API Key error: %v", err)
}

func RequestMarshalError(err error) string {
	return fmt.Sprintf("Error marshaling request: %v\n", err)
}

func RequestCreationError(err error) string {
	return fmt.Sprintf("Error creating request: %v\n", err)
}

func HTTPRequestError(err error) string {
	return fmt.Sprintf("Error making HTTP request: %v\n", err)
}

func APIError(body string, statusCode int) string {
	return fmt.Sprintf("API error: %s, status code: %d", body, statusCode)
}

func ResponseBodyError(err error) string {
	return fmt.Sprintf("Error reading response body: %v\n", err)
}

func ResponseUnmarshalError(err error) string {
	return fmt.Sprintf("Error unmarshaling response body: %v\n", err)
}

func NoGeminiContent() string {
	return "No content in response from Gemini"
}

func NoOrchestrationModel(modelName string) string {
	return fmt.Sprintf("No orchestration model specified, falling back to editing model: %s\n", modelName)
}

func NoSummaryModelFallback(modelName string) string {
	return fmt.Sprintf("No summary model specified in config, falling back to editing model: %s for script analysis.\n", modelName)
}

func ProviderNotRecognized() string {
	return "LLM provider not recognized."
}

func LLMResponseError(err error) string {
	return fmt.Sprintf("Error getting LLM response: %v", err)
}

// --- System Info Prompts ---
func MemoryDetectionError(defaultModel string, err error) string {
	return fmt.Sprintf("Could not determine system memory, defaulting to %s: %v", defaultModel, err)
}

func SystemMemoryFallback(gb int, model string) string {
	return fmt.Sprintf("Detected %dGB of system memory. Falling back to %s.", gb, model)
}

// --- Orchestration Prompts ---
func OrchestrationAlphaWarning() string {
	boldYellow := color.New(color.FgYellow, color.Bold).SprintFunc()
	return boldYellow("WARNING: Orchestration is currently an early alpha feature and is NOT recommended for general use. For a more robust and controllable process, please see examples/generate_todos.sh and examples/process_todos.sh for how to implement similar functionality.")
}

func LeditDirCreationError(err error) string {
	return fmt.Sprintf("Could not create .ledit directory: %v", err)
}

func UnfinishedPlanAutoResume() string {
	return "An unfinished orchestration plan was found. Resuming automatically."
}

func UnfinishedPlanFound() string {
	return "An unfinished orchestration plan was found."
}

func ContinueOrchestrationPrompt() string {
	return "Do you want to continue where you left off?"
}

func ResumingOrchestration() string {
	return "Resuming orchestration..."
}

func GeneratingNewPlan() string {
	return "Generating a new orchestration plan..."
}

func GenerateRequirementsFailed(err error) string {
	return fmt.Sprintf("Failed to generate requirements: %v", err)
}

func OrchestrationError(err error) string {
	return fmt.Sprintf("Orchestration failed: %v", err)
}

func OrchestrationFinishedSuccessfully() string {
	return "Orchestration finished successfully!"
}

func GeneratedSearchQuery(query string) string {
	return fmt.Sprintf("Generated search query: \"%s\"", query)
}

func SearchQueryGenerationWarning(err error) string {
	return fmt.Sprintf("Warning: Failed to generate search query: %v", err)
}

func AddedSearchGrounding(query string) string {
	return fmt.Sprintf("--- Added search grounding to retry prompt with query: \"%s\" ---", query)
}

func AddingValidationFailureContext() string {
	return "Adding validation failure context to LLM request..."
}

func AllOrchestrationStepsCompleted() string {
	return "All orchestration steps completed."
}

// --- New prompts for orchestration changes ---

func GeneratingFileChanges(instruction string) string {
	return fmt.Sprintf("Generating file-specific changes for requirement: '%s'...", instruction)
}

func GenerateChangesFailed(instruction string, err error) string {
	return fmt.Sprintf("Failed to generate file-specific changes for requirement '%s': %v", instruction, err)
}

func SkippingCompletedFileChange(filepath, instruction string) string {
	return fmt.Sprintf("Skipping completed file change for '%s': '%s'", filepath, instruction)
}

func ExecutingFileChange(instruction string) string {
	return fmt.Sprintf("Executing file change: '%s'", instruction)
}

func FileChangeCompleted(filepath, instruction string) string {
	return fmt.Sprintf("File change for '%s' completed: '%s'", filepath, instruction)
}

func FileChangeFailedAfterAttempts(filepath, instruction string, attempts int, err error) string {
	return fmt.Sprintf("File change for '%s' ('%s') failed after %d attempts: %v", filepath, instruction, attempts, err)
}

// --- Application Error Prompts ---
func FatalError(err error) string {
	boldRed := color.New(color.FgRed, color.Bold).SprintFunc()
	return fmt.Sprintf("%s: %v\n\nThis is a fatal error. Please check .ledit/workspace.log for more details and consider reporting an issue on GitHub.", boldRed("A FATAL ERROR OCCURRED"), err)
}

// GracefulExitMessage represents the context for a graceful exit
type GracefulExitMessage struct {
	Context      string      // Context about what was happening when the error occurred
	Error        error       // The error that caused the exit
	TokenUsage   interface{} // Token usage information (optional)
	ModelName    string      // The model being used (optional)
	Accomplished []string    // What was accomplished before the failure (optional)
	Resolution   []string    // Manual resolution steps (optional)
}

// GracefulExit generates a positive, informative exit message
func GracefulExit(msg GracefulExitMessage) string {
	var output strings.Builder

	// Header with positive framing
	boldCyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	output.WriteString(fmt.Sprintf("\n\n%s\n\n", boldCyan("🤖 Looks like this is a task for a person, we tried, but we weren't able to move this forward.")))

	// Context about what happened
	if msg.Context != "" {
		output.WriteString(fmt.Sprintf("What we were trying to do: %s\n\n", msg.Context))
	}

	// Token usage and cost information
	if msg.TokenUsage != nil {
		// Check if it implements the TokenUsageInterface
		if usageInterface, ok := msg.TokenUsage.(types.TokenUsageInterface); ok {
			output.WriteString("📊 Session Summary:\n")
			output.WriteString(fmt.Sprintf("   • Total tokens used: %d\n", usageInterface.GetTotalTokens()))

			// Calculate cost if we have model information
			if msg.ModelName != "" {
				cost := calculateTotalCost(usageInterface, msg.ModelName)
				if cost > 0 {
					output.WriteString(fmt.Sprintf("   • Estimated cost: $%.4f\n", cost))
				}
			}

			// Try to show detailed breakdown if it's an AgentTokenUsage
			if agentUsage, ok := msg.TokenUsage.(types.AgentTokenUsage); ok {
				if agentUsage.IntentAnalysis > 0 {
					output.WriteString(fmt.Sprintf("   • Intent analysis: %d tokens\n", agentUsage.IntentAnalysis))
				}
				if agentUsage.Planning > 0 {
					output.WriteString(fmt.Sprintf("   • Planning: %d tokens\n", agentUsage.Planning))
				}
				if agentUsage.CodeGeneration > 0 {
					output.WriteString(fmt.Sprintf("   • Code generation: %d tokens\n", agentUsage.CodeGeneration))
				}
				if agentUsage.Validation > 0 {
					output.WriteString(fmt.Sprintf("   • Validation: %d tokens\n", agentUsage.Validation))
				}
			}
			output.WriteString("\n")
		}
	}

	// What was accomplished
	if len(msg.Accomplished) > 0 {
		output.WriteString("✅ What we accomplished:\n")
		for _, item := range msg.Accomplished {
			output.WriteString(fmt.Sprintf("   • %s\n", item))
		}
		output.WriteString("\n")
	}

	// The error that occurred
	if msg.Error != nil {
		red := color.New(color.FgRed).SprintFunc()
		output.WriteString(fmt.Sprintf("❌ What went wrong: %s\n\n", red(msg.Error.Error())))
	}

	// Manual resolution steps
	if len(msg.Resolution) > 0 {
		output.WriteString("🛠️  Next steps for manual resolution:\n")
		for i, step := range msg.Resolution {
			output.WriteString(fmt.Sprintf("   %d. %s\n", i+1, step))
		}
		output.WriteString("\n")
	}

	// Footer with helpful information
	yellow := color.New(color.FgYellow).SprintFunc()
	output.WriteString(fmt.Sprintf("%s\n", yellow("💡 Check .ledit/workspace.log for detailed logs and context.")))
	output.WriteString(fmt.Sprintf("%s\n", yellow("🐛 Consider reporting this issue on GitHub if you believe it's a bug.")))

	return output.String()
}

// calculateTotalCost calculates the total cost based on token usage and model pricing
// This is a simplified calculation that provides basic cost estimates
func calculateTotalCost(tokenUsage types.TokenUsageInterface, modelName string) float64 {
	if tokenUsage == nil || tokenUsage.GetTotalTokens() == 0 {
		return 0
	}

	totalTokens := tokenUsage.GetTotalTokens()

	// Simple cost estimation based on common model pricing ranges
	// This is a rough estimate and actual costs may vary
	var costPer1K float64

	// Very basic model-based pricing (rough estimates)
	if strings.Contains(strings.ToLower(modelName), "gpt-4") {
		costPer1K = 0.03 // GPT-4 average
	} else if strings.Contains(strings.ToLower(modelName), "gpt-3.5") {
		costPer1K = 0.002 // GPT-3.5 average
	} else if strings.Contains(strings.ToLower(modelName), "claude") {
		costPer1K = 0.015 // Claude average
	} else if strings.Contains(strings.ToLower(modelName), "deepseek") {
		costPer1K = 0.001 // DeepSeek average
	} else if strings.Contains(strings.ToLower(modelName), "gemini") {
		costPer1K = 0.00025 // Gemini average
	} else {
		costPer1K = 0.01 // Default fallback
	}

	return float64(totalTokens) * costPer1K / 1000
}

// NewGracefulExitWithTokenUsage creates a graceful exit message with token usage information
func NewGracefulExitWithTokenUsage(context string, err error, tokenUsage interface{}, modelName string) string {
	var accomplished []string
	var resolution []string

	// Set appropriate context-specific messages
	switch {
	case strings.Contains(strings.ToLower(context), "agent"):
		accomplished = []string{
			"Initialized AI agent",
			"Analyzed user intent",
			"Set up workspace context",
		}
		resolution = []string{
			"Check your intent description for clarity",
			"Verify the workspace has the necessary files",
			"Ensure the AI model is properly configured",
			"Review the workspace log for detailed error information",
		}
	case strings.Contains(strings.ToLower(context), "code"):
		accomplished = []string{
			"Loaded code files",
			"Analyzed existing code structure",
			"Prepared code editing tools",
		}
		resolution = []string{
			"Verify the file path and permissions",
			"Check that the target file exists and is readable",
			"Ensure the instructions are clear and specific",
		}
	case strings.Contains(strings.ToLower(context), "build") || strings.Contains(strings.ToLower(context), "validation"):
		accomplished = []string{
			"Generated code changes",
			"Applied modifications to files",
			"Started build validation process",
		}
		resolution = []string{
			"Check for syntax errors in the generated code",
			"Verify build dependencies are installed",
			"Review the build output for specific error messages",
			"Consider running the build manually for more details",
		}
	default:
		accomplished = []string{
			"Started processing your request",
			"Initialized necessary systems",
		}
		resolution = []string{
			"Check the command syntax and arguments",
			"Verify file permissions and access",
			"Review the workspace log for more details",
		}
	}

	exitMsg := GracefulExitMessage{
		Context:      context,
		Error:        err,
		TokenUsage:   tokenUsage,
		ModelName:    modelName,
		Accomplished: accomplished,
		Resolution:   resolution,
	}

	return GracefulExit(exitMsg)
}

// --- Code Review Prompts ---
func CodeReviewStagedPrompt() string {
	return `You are an expert code reviewer. Please analyze the provided Git diff of staged changes and provide a comprehensive code review in JSON format.

Your review should include:

1. **Code Quality Assessment**:
   - Check for code clarity, readability, and maintainability
   - Identify any code smells or anti-patterns
   - Assess adherence to best practices and conventions

2. **Potential Issues**:
   - Look for bugs, logic errors, or edge cases
   - Identify performance concerns
   - Check for security vulnerabilities
   - Look for potential race conditions or concurrency issues

3. **Architecture & Design**:
   - Evaluate if the changes fit well with the existing codebase
   - Check for proper separation of concerns
   - Assess if the implementation is consistent with the project's patterns

4. **Testing & Documentation**:
   - Check if tests are included or updated appropriately
   - Verify if documentation needs to be updated
   - Assess test coverage for the changes

5. **Dependencies & Compatibility**:
   - Review any new dependencies or version changes
   - Check for breaking changes or compatibility issues

CRITICAL: You MUST respond with a valid JSON object only. No markdown formatting, no explanatory text before or after the JSON.

Required JSON format:
{
  "status": "approved|needs_revision|rejected",
  "feedback": "Your comprehensive assessment and feedback",
  "detailed_guidance": "Specific guidance for improvements if needed (optional)",
  "patch_resolution": "Complete updated file content if direct patch is provided (optional)",
  "new_prompt": "Suggested new prompt if changes are rejected (optional)"
}

Example response:
{
  "status": "needs_revision",
  "feedback": "The code has good structure but needs error handling improvements",
  "detailed_guidance": "Add proper error handling in the main function and validate input parameters"
}

Remember: Return ONLY the JSON object, nothing else.`
}
