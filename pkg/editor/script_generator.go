package editor

import (
	"fmt"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts" // Import the new prompts package
	"os"
	"os/exec"
	"strings"
)

// createAndRunSetupScript generates, updates, and runs a setup.sh script.
// It now takes the instruction and filepath directly, rather than an OrchestrationRequirement.
func createAndRunSetupScript(instruction, filepath string, originalCfg *config.Config) error {
	fmt.Println(prompts.GeneratingSetupScript()) // Use prompt
	setupPrompt := fmt.Sprintf("You are an expert software developer. Your task is to write a setup script. "+
		"An instruction has just been executed to modify the codebase. The instruction was: \"%s\" which primarily affected the file: \"%s\". "+
		"Create or edit a shell script named setup.sh that can be run to do any setup needed for the project and prepare the environment for validation. "+
		"ENSURE the script is idempotent, meaning it will be run multiple times and should check before preforming 1 time setup actions. "+
		"Note that the script might already exist, so you should only add or modify the necessary parts. "+
		"The script should be self-contained, executable, and should exit with a non-zero status code on failure. "+
		"The script will be run from the root of the project. "+
		"If an autoformatter is available for the language, include a command to run it. "+
		"Do not include any validation steps in this script; it should only handle setup tasks. "+
		"Do include a build step if the project requires it, such as compiling code or generating assets, but don't include it before the project has enough scaffolding to compile. "+
		"If no setup is needed, output an empty script or a script with only comments. "+
		"Only output the raw script content inside a single markdown code block for `setup.sh`. Do not include any other text or explanation. #WS", instruction, filepath) // Use passed instruction and filepath

	// Process the setup prompt for search grounding or workspace context
	processedSetupPrompt, _, err := processInstructions(setupPrompt, originalCfg) // Use originalCfg for processing prompt
	if err != nil {
		return fmt.Errorf("failed to process setup script prompt: %w", err)
	}

	// Create a temporary config for this specific call to ProcessCodeGeneration
	// to allow editing of setup.sh if the user didn't explicitly skip prompts for orchestration.
	tempCfg := *originalCfg      // Make a copy
	if !originalCfg.SkipPrompt { // If the user *didn't* use --skip-prompt for orchestration
		tempCfg.SkipPrompt = false // Allow prompting for setup.sh
	}

	// Call ProcessCodeGeneration for setup.sh. This handles LLM call, parsing, diffing, user prompt (y/n/e), saving, and git tracking.
	// The returned diffForTargetFile is not directly used here, but ProcessCodeGeneration handles the file writing.
	_, err = ProcessCodeGeneration("setup.sh", processedSetupPrompt, &tempCfg)
	if err != nil {
		return fmt.Errorf("failed to generate/update setup.sh: %w", err)
	}

	// After ProcessCodeGeneration, the setup.sh file should be updated on disk (or not, if user chose 'n')
	// We need to check if the file exists and has content before trying to run it.
	setupScriptContent, err := os.ReadFile("setup.sh")
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println(prompts.NoSetupScriptFound()) // Use prompt
			return nil
		}
		return fmt.Errorf("failed to read setup.sh after generation: %w", err)
	}

	if strings.TrimSpace(string(setupScriptContent)) == "" {
		fmt.Println(prompts.SetupScriptEmpty()) // Use prompt
		return nil
	}

	fmt.Println(prompts.GeneratedSetupScriptHeader()) // Use prompt
	fmt.Println(string(setupScriptContent))           // Print the content that will be run
	fmt.Println(prompts.ScriptSeparator())            // Use prompt

	// Ensure the script is executable before running
	if err := os.Chmod("setup.sh", 0755); err != nil {
		return fmt.Errorf("failed to make setup.sh executable: %w", err)
	}

	fmt.Println(prompts.RunningSetupScript()) // Use prompt
	cmd := exec.Command("bash", "./setup.sh")

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not get current working directory to run setup: %w", err)
	}
	cmd.Dir = workDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(prompts.SetupScriptOutput(output)) // Use prompt
		return fmt.Errorf("setup script failed: %w\n%s", err, prompts.SetupScriptOutput(output))
	}

	fmt.Println(string(output))
	fmt.Println(prompts.SetupSuccessful()) // Use prompt
	return nil
}

// createAndRunValidationScript generates, updates, and runs a validate.sh script.
// It now takes the instruction and filepath directly, rather than an OrchestrationRequirement.
func createAndRunValidationScript(instruction, filepath string, originalCfg *config.Config) error {
	fmt.Println(prompts.GeneratingValidationScript()) // Use prompt
	validationPrompt := fmt.Sprintf("You are an expert software developer. Your task is to write a validation script. "+
		"An instruction has just been executed to modify the codebase. The instruction was: \"%s\" which primarily affected the file(s): \"%s\". "+
		"A setup script has already been run to prepare the environment (e.g., install dependencies). "+
		"Create, or update a shell script named validate.sh that can be run to verify the change was successful and didn't break anything. "+
		"Note that the script might already exist, so you should only add or modify the necessary parts. "+
		"The script should focus on validation steps like running tests, or build commands. Do not include setup steps like installing dependencies, but do start virtual environments if they are required"+
		"Ensure you are not adding placeholder content that will be executed, but you can add comments for what will need to be done later. "+
		"For instance, don't assume you can start a server if the files for the server have not yet been created, but you could add a comment for future functionality. "+
		"The script should be self-contained, executable, and should exit with a non-zero status code on failure. "+
		"The script will be run from the root of the project. "+
		"Only output the raw script content inside a single markdown code block for `validate.sh`. Do not include any other text or explanation. #WS", instruction, filepath) // Use passed instruction and filepath

	// Process the validation prompt for search grounding or workspace context
	processedValidationPrompt, _, err := processInstructions(validationPrompt, originalCfg) // Use originalCfg for processing prompt
	if err != nil {
		return fmt.Errorf("failed to process validation script prompt: %w", err)
	}

	// Create a temporary config for this specific call to ProcessCodeGeneration
	tempCfg := *originalCfg      // Make a copy
	if !originalCfg.SkipPrompt { // If the user *didn't* use --skip-prompt for orchestration
		tempCfg.SkipPrompt = false // Allow prompting for validate.sh
	}

	// Call ProcessCodeGeneration for validate.sh. This handles LLM call, parsing, diffing, user prompt (y/n/e), saving, and git tracking.
	_, err = ProcessCodeGeneration("validate.sh", processedValidationPrompt, &tempCfg)
	if err != nil {
		return fmt.Errorf("failed to generate/update validate.sh: %w", err)
	}

	// After ProcessCodeGeneration, the validate.sh file should be updated on disk (or not, if user chose 'n')
	// We need to check if the file exists and has content before trying to run it.
	validationScriptContent, err := os.ReadFile("validate.sh")
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println(prompts.NoValidationScriptFound()) // Use prompt
			return nil
		}
		return fmt.Errorf("failed to read validate.sh after generation: %w", err)
	}

	if strings.TrimSpace(string(validationScriptContent)) == "" {
		fmt.Println(prompts.ValidationScriptEmpty()) // Use prompt
		return nil
	}

	fmt.Println(prompts.GeneratedValidationScriptHeader()) // Use prompt
	fmt.Println(string(validationScriptContent))           // Print the content that will be run
	fmt.Println(prompts.ScriptSeparator())                 // Use prompt

	// Ensure the script is executable before running
	if err := os.Chmod("validate.sh", 0755); err != nil {
		return fmt.Errorf("failed to make validate.sh executable: %w", err)
	}

	fmt.Println(prompts.RunningValidationScript()) // Use prompt
	cmd := exec.Command("bash", "./validate.sh")

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not get current working directory to run validation: %w", err)
	}
	cmd.Dir = workDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(prompts.ValidationScriptOutput(output)) // Use prompt
		return fmt.Errorf("validation script failed: %w\n%s", err, prompts.ValidationScriptOutput(output))
	}

	fmt.Println(string(output))
	fmt.Println(prompts.ValidationSuccessful()) // Use prompt
	return nil
}
