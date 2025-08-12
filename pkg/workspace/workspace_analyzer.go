package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
)

// generateFileHash creates a SHA256 hash of the file content.
func generateFileHash(content string) string {
	hasher := sha256.New()
	hasher.Write([]byte(content))
	return hex.EncodeToString(hasher.Sum(nil))
}

// getSummary uses an LLM to generate a summary, exports, and references for a given file content.
func getSummary(content, filename string, cfg *config.Config) (string, string, string, error) {
	log := utils.GetLogger(cfg.SkipPrompt)
	// Check if the file is a text file
	if !isTextFile(filename) {
		return "", "", "", fmt.Errorf("file type not supported for analysis")
	}

	// Tweak the prompt for better results and explicit JSON format
	prompt := fmt.Sprintf("Analyze the following code from the file '%s'.\n", filename)
	prompt += `Your task is to provide three pieces of information in JSON format:
1.  A CONCISE summary of the file's overall purpose and functionality.
2.  A list of all exported (publicly accessible) functions, types, and variables. For each exported item, include its name, or method signature and a very brief description when needed.
3.  List of referenced files with their workspace relative path and extension.
The output MUST be a JSON object with three keys: 'summary' (string), 'exports' (string), and 'references' (array of strings).
Example JSON structure:
{
  "summary": "This file manages user authentication and session handling.",
  "exports": "Login(username, password) - Authenticates a user; Logout() - Ends the current session; User struct - Represents a user profile.",
  "references": ["file-path1", "file-path2"]
}

`

	finalPrompt := fmt.Sprintf("%s```\n%s\n```", prompt, content)
	messages := []prompts.Message{
		{
			Role:    "system",
			Content: "You are an expert code analyst. Provide your analysis in the requested raw JSON format, without any markdown formatting.",
		},
		{
			Role:    "user",
			Content: finalPrompt,
		},
	}

	// Set 40-second timeout for workspace summary requests
	response, _, err := llm.GetLLMResponse(cfg.SummaryModel, messages, filename, cfg, 40*time.Second)
	if err != nil {
		return "", "", "", fmt.Errorf("LLM request failed: %w", err)
	}

	// Log the raw LLM response for troubleshooting
	log.Logf("DEBUG: Raw LLM Response for %s:\n%s\n", filename, response)

	// Attempt to extract JSON from markdown code blocks if present
	if strings.Contains(response, "```json") {
		re := regexp.MustCompile("(?s)```json\n(.*?)\n```")
		matches := re.FindStringSubmatch(response)
		if len(matches) > 1 {
			response = matches[1]
		}
	} else if strings.HasPrefix(response, "```") && strings.HasSuffix(response, "```") {
		// Handle cases where it's just ``` and then the JSON
		response = strings.TrimPrefix(response, "```")
		response = strings.TrimSuffix(response, "```")
	}
	response = strings.TrimSpace(response)

	var result struct {
		Summary    string   `json:"summary"`
		Exports    string   `json:"exports"`
		References []string `json:"references"`
	}
	err = json.Unmarshal([]byte(response), &result)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to unmarshal LLM JSON response: %w. Response: %s", err, response)
	}

	// Convert references array to a string if needed
	referencesStr := strings.Join(result.References, ", ")

	return result.Summary, result.Exports, referencesStr, nil
}

// isTextFile checks if a file has a common text-based extension.
func isTextFile(filename string) bool {
	textExtensions := []string{".txt", ".go", ".py", ".js", ".java", ".c", ".cpp", ".h", ".hpp", ".md", ".json", ".yaml", ".yml", ".sh", ".bash", ".sql", ".html", ".css", ".xml", ".csv", ".ts", ".tsx", ".php", ".rb", ".swift", ".kt", ".scala", ".rust", ".rs", ".dart", ".perl", ".pl", ".pm", ".lua", ".vim", ".toml"}
	ext := filepath.Ext(filename)
	for _, te := range textExtensions {
		if ext == te {
			return true
		}
	}
	return false
}

// GetProjectGoals uses an LLM to autogenerate project goals based on the workspace summary.
func GetProjectGoals(cfg *config.Config, workspaceSummary string) (ProjectGoals, error) {
	log := utils.GetLogger(cfg.SkipPrompt)

	messages := prompts.BuildProjectGoalsMessages(workspaceSummary)

	modelName := cfg.WorkspaceModel // Use the workspace model for generating project goals

	response, _, err := llm.GetLLMResponse(modelName, messages, "", cfg, 2*time.Minute)
	if err != nil {
		return ProjectGoals{}, fmt.Errorf("failed to get project goals from LLM: %w", err)
	}
	// Log the raw LLM response for troubleshooting
	log.Logf("DEBUG: Raw LLM Response for project goals:\n%s\n", response)

	// Clean the response from markdown code blocks
	if strings.Contains(response, "```json") {
		parts := strings.SplitN(response, "```json", 2)
		if len(parts) > 1 {
			response = strings.Split(parts[1], "```")[0]
		} else if strings.HasPrefix(response, "```") && strings.HasSuffix(response, "```") {
			response = strings.TrimPrefix(response, "```")
			response = strings.TrimSuffix(response, "```")
		}
	}

	log.Log(fmt.Sprintf("DEBUG: Cleaned LLM Response for project goals:\n%s\n", response))

	var goals ProjectGoals
	if err := json.Unmarshal([]byte(response), &goals); err != nil {
		log.Logf("DEBUG: Failed to unmarshal project goals JSON: %s\n", response)
		return ProjectGoals{}, fmt.Errorf("failed to parse project goals JSON from LLM response: %w\nResponse was: %s", err, response)
	}

	return goals, nil
}
