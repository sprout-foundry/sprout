package prompts

import (
	"fmt"
	"time"
)

// --- General CLI Messages ---

func ConfigLoadFailed(err error) string {
	return fmt.Sprintf("Failed to load configuration: %v. Please run 'ledit init'.", err)
}

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
	return "--------------------------------------------"
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

// --- Editor Messages ---

func LoadingWorkspaceData() string {
	return "--- Loading in workspace data ---"
}

func URLFetchError(url string, err error) string {
	return fmt.Sprintf("Could not fetch content from URL %s: %v. Continuing without it.\n", url, err)
}

func FileLoadError(path string, err error) string {
	return fmt.Sprintf("Error loading content from path %s: %v. Continuing without it.\n", path, err)
}

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
	return fmt.Sprintf("This request will take approximately %d tokens with model %s. \n", tokens, modelName)
}

func TokenLimitWarning(currentTokens, defaultLimit int) string {
	return fmt.Sprintf("NOTE: This request at %d tokens is over the default token limit of %d, do you want to continue? (y/n): ", currentTokens, defaultLimit)
}

func OperationCancelled() string {
	return "Operation cancelled by user."
}

func ContinuingRequest() string {
	return "Ok, continuing with request\n:"
}

func APIKeyError(err error) string {
	return fmt.Sprintf("Error getting API key: %v\n", err)
}

func RequestMarshalError(err error) string {
	return fmt.Sprintf("Error marshaling request body: %v\n", err)
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

func ProviderNotRecognized() string {
	return "Provider not recognized, falling back to local Ollama model."
}

func LLMResponseError(err error) string {
	return fmt.Sprintf("\nError getting LLM response: %v\n", err)
}

// --- Config Messages ---

func MemoryDetectionError(defaultModel string, err error) string {
	return fmt.Sprintf("Could not determine system memory, defaulting to %s: %v", defaultModel, err)
}

func SystemMemoryFallback(gb int, model string) string {
	return fmt.Sprintf("System memory: %d GB, using %s for local fallback", gb, model)
}

func EnterEditingModel(defaultModel string) string {
	return fmt.Sprintf("Enter the editing model or press enter to use default (e.g., openai:gpt-4o) [default: %s]: ", defaultModel)
}

func EnterSummaryModel(defaultModel string) string {
	return fmt.Sprintf("Enter the summary model or press enter to use default (used for file summaries) [default: %s]: ", defaultModel)
}

func EnterWorkspaceModel(defaultModel string) string {
	return fmt.Sprintf("Enter the workspace model or press enter to use default (used for overall workspace analysis) [default: %s]: ", defaultModel)
}

func EnterOrchestrationModel(defaultModel string) string {
	return fmt.Sprintf("Enter the orchestration model or press enter to use default (used for planning complex changes) [default: %s]: ", defaultModel)
}

func TrackGitPrompt() string {
	return "Automatically track changes to git when files are modified? (yes/no): "
}

func NoConfigFound() string {
	return "No configuration file found. Initializing..."
}

func ConfigSaved(path string) string {
	return fmt.Sprintf("Configuration saved to %s", path)
}

// --- Orchestrator Messages ---

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
	return "Do you want to continue where you left off? (y/n): "
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
	return "The orchestration plan is empty. Nothing to do."
}

func GeneratedPlanHeader() string {
	return "Generated Plan:"
}

func PlanStep(index int, filepath, instruction string) string {
	return fmt.Sprintf("%d. File: %s\n   Instruction: %s", index+1, filepath, instruction)
}

func ApplyPlanPrompt() string {
	return "\nDo you want to apply these changes? (y/n): "
}

func OrchestrationCancelled() string {
	return "Orchestration cancelled by user."
}

func OrchestrationError(err error) string {
	return fmt.Sprintf("Error during orchestration: %v", err)
}

func OrchestrationFinishedSuccessfully() string {
	return "Orchestration finished successfully."
}

// --- Requirement Processor Messages ---

func SkippingCompletedStep(instruction string) string {
	return fmt.Sprintf("Skipping completed step: %s", instruction)
}

func RetryingFailedStep(instruction string) string {
	return fmt.Sprintf("\n--- Retrying Failed Step: %s ---", instruction)
}

func ExecutingStep(instruction string) string {
	return fmt.Sprintf("\n--- Executing Step: %s ---", instruction)
}

func ProcessingFile(filepath string) string {
	return fmt.Sprintf("File: %s", filepath)
}

func RetryAttempt(attempt, maxAttempts int, instruction string) string {
	return fmt.Sprintf("\n--- Retry Attempt %d/%d for: %s ---", attempt, maxAttempts, instruction)
}

func ProcessInstructionFailed(filepath string, err error) string {
	return fmt.Sprintf("Failed to process instruction for file %s: %v", filepath, err)
}

func ProcessRequirementFailed(filepath string, err error) string {
	return fmt.Sprintf("Failed to process requirement for file %s: %v", filepath, err)
}

func SetupStepCompleted(instruction string) string {
	return fmt.Sprintf("--- Setup Step Completed: %s ---", instruction)
}

func SetupFailedAttempt(attempt int, err error) string {
	return fmt.Sprintf("--- Setup failed on attempt %d. Error: %v ---", attempt, err)
}

func ValidationFailureContextSetupScriptFailed(err error) string {
	return fmt.Sprintf("The setup script failed, which may indicate an issue with the generated code (e.g., incorrect dependencies) or the setup script itself. Error: %s", err.Error())
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
	return fmt.Sprintf("--- Generated search query for grounding: \"%s\" ---", query)
}

func SearchQueryGenerationWarning(err error) string {
	return fmt.Sprintf("Warning: Failed to generate search query for grounding: %v", err)
}

func AddedSearchGrounding(query string) string {
	return fmt.Sprintf("--- Added search grounding to retry prompt with query: \"%s\" ---", query)
}

func AddingValidationFailureContext() string {
	return "--- Adding validation failure context to retry prompt ---"
}

func RetryPromptWithDiff(originalInstruction, filepath, validationError, lastLLMResponse string) string {
	return fmt.Sprintf(
		"The previous attempt to apply the instruction \"%s\" to file \"%s\" failed validation. The validation error was:\n\n%s\n\nHere is the diff of the changes from the last attempt that produced the failing code:\n\n---\n%s\n---\n\nPlease analyze the error and the previous diff, then provide a corrected version of the code, the setup script, or the validation script. The original instruction was: \"%s\"",
		originalInstruction, filepath, validationError, lastLLMResponse, originalInstruction,
	)
}

func RetryPromptWithoutDiff(originalInstruction, filepath, validationError string) string {
	return fmt.Sprintf(
		"The previous attempt to apply the instruction \"%s\" to file \"%s\" failed validation. The validation error was:\n\n%s\n\nPlease analyze the error, then provide a corrected version of the code, the setup script, or the validation script. The original instruction was: \"%s\"",
		originalInstruction, filepath, validationError, originalInstruction,
	)
}

func AllOrchestrationStepsCompleted() string {
	return "All orchestration steps completed successfully."
}