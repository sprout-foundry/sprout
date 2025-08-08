package prompts

import (
	"fmt"
	"time"

	"github.com/fatih/color" // Import the color package
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
	return fmt.Sprintf("File %s contains confirmed security concerns. Skipping LLM summarization.", filename)
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
	return fmt.Sprintf("Estimated tokens for %s: %d\n", modelName, tokens)
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

func EmptyOrchestrationPlan() string {
	return "LLM returned an empty orchestration plan. Nothing to do."
}

func GeneratedPlanHeader() string {
	return "Generated Plan:"
}

func PlanStep(index int, filepath, instruction string) string {
	return fmt.Sprintf("  %d. File: %s, Instruction: %s", index+1, filepath, instruction)
}

func ApplyPlanPrompt() string {
	return "\nDo you want to apply these changes? (y/n): "
}

func OrchestrationCancelled() string {
	return "Orchestration cancelled by user."
}

func OrchestrationError(err error) string {
	return fmt.Sprintf("Orchestration failed: %v", err)
}

func OrchestrationFinishedSuccessfully() string {
	return "Orchestration finished successfully!"
}

func SkippingCompletedStep(instruction string) string {
	return fmt.Sprintf("Skipping completed step: %s", instruction)
}

func RetryingFailedStep(instruction string) string {
	return fmt.Sprintf("Retrying failed step: %s", instruction)
}

func ExecutingStep(instruction string) string {
	return fmt.Sprintf("Executing step: %s", instruction)
}

func ProcessingFile(filepath string) string {
	return fmt.Sprintf("Processing file: %s", filepath)
}

func RetryAttempt(attempt, maxAttempts int, instruction string) string {
	return fmt.Sprintf("Retry attempt %d/%d for instruction: %s", attempt, maxAttempts, instruction)
}

func ProcessInstructionFailed(filepath string, err error) string {
	return fmt.Sprintf("Failed to process instruction for file %s: %v", filepath, err)
}

func ProcessRequirementFailed(filepath string, err error) string {
	return fmt.Sprintf("Failed to process requirement for file %s: %v", filepath, err)
}

func SetupStepCompleted(instruction string) string {
	return fmt.Sprintf("Setup step completed: %s", instruction)
}

func SetupFailedAttempt(attempt int, err error) string {
	return fmt.Sprintf("Setup failed on attempt %d: %v", attempt, err)
}

func ValidationFailureContextSetupScriptFailed(err error) string {
	return fmt.Sprintf("The setup script failed, which may indicate an issue with the generated code (e.g., incorrect dependencies) or the setup script itself. Error: %s", err.Error())
}

func ValidationFailedAttempt(attempt int, err error) string {
	return fmt.Sprintf("Validation script failed on attempt %d: %v", attempt, err)
}

func ValidationFailureContextValidationScriptFailed(err error) string {
	return fmt.Sprintf("The validation script failed, which may indicate an issue with the generated code. Error: %s", err.Error())
}

func SaveProgressFailed(filepath string, err error) string {
	return fmt.Sprintf("Step for %s completed, but failed to save progress: %v", filepath, err)
}

func StepCompleted(instruction string) string {
	return fmt.Sprintf("--- Step Completed: %s ---", instruction)
}

func StepFailedAfterAttempts(instruction string, maxAttempts int, err error) string {
	return fmt.Sprintf("Step '%s' failed after %d attempts: %v", instruction, maxAttempts, err)
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

// --- Code Review Prompts ---
func CodeReviewStagedPrompt() string {
	return `You are an expert code reviewer. Please analyze the provided Git diff of staged changes and provide a comprehensive code review.

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

Please provide your feedback in the following format:

**Status**: [APPROVED/NEEDS_REVISION/REJECTED]

**Summary**: Brief overall assessment of the changes

**Detailed Feedback**:
- **Strengths**: What was done well
- **Issues**: Specific problems that need to be addressed (if any)
- **Suggestions**: Recommendations for improvement
- **Security Concerns**: Any security-related issues (if any)

**Action Items** (if status is not APPROVED):
List specific changes that should be made before approval

Be constructive and specific in your feedback. Focus on actionable suggestions that will improve the code quality and maintainability.`
}
