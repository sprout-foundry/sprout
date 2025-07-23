package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ledit/pkg/prompts" // Import the new prompts package
	"ledit/pkg/utils"   // Import workspace for logger

	"github.com/shirou/gopsutil/v3/mem"
)

const (
	LargeCoder  = "qwen2.5-coder:32b"
	MediumCoder = "qwen2.5-coder:14b"
	SmallCoder  = "qwen2.5-coder:7b"
	MicroCoder  = "qwen2.5-coder:3b"
)

type Config struct {
	EditingModel             string `json:"editing_model"`
	SummaryModel             string `json:"summary_model"`
	OrchestrationModel       string `json:"orchestration_model"` // new field for orchestration tasks
	WorkspaceModel           string `json:"workspace_model"`     // New field for workspace analysis
	EmbeddingModel           string `json:"embedding_model"`     // New field for embeddings
	LocalModel               string `json:"local_model"`
	TrackWithGit             bool   `json:"track_with_git"`
	EnableSecurityChecks     bool   `json:"enable_security_checks"` // New field for security checks
	SkipPrompt               bool   `json:"-"`                      // Internal use, not saved to config
	Interactive              bool   `json:"-"`                      // Internal use, not saved to config
	OllamaServerURL          string `json:"ollama_server_url"`
	OrchestrationMaxAttempts int    `json:"orchestration_max_attempts"` // New field for max attempts
}

func getHomeConfigPath() (string, string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", ""
	}
	configDir := filepath.Join(home, ".ledit")
	return configDir, filepath.Join(configDir, "config.json")
}

func getCurrentConfigPath() (string, string) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", ""
	}
	configDir := filepath.Join(cwd, ".ledit")
	return configDir, filepath.Join(configDir, "config.json")
}

func getLocalModel(skipPrompt bool) string {
	logger := utils.GetLogger(skipPrompt) // Get the logger instance
	v, err := mem.VirtualMemory()
	if err != nil {
		logger.Logf(prompts.MemoryDetectionError(MicroCoder, err)) // Use prompt
		return MicroCoder
	}
	gb := v.Total / (1024 * 1024 * 1024)
	if gb >= 48 {
		logger.Logf(prompts.SystemMemoryFallback(int(gb), LargeCoder)) // Use prompt
		return LargeCoder
	}
	if gb >= 38 {
		logger.Logf(prompts.SystemMemoryFallback(int(gb), MediumCoder)) // Use prompt
		return MediumCoder
	}
	if gb >= 20 {
		logger.Logf(prompts.SystemMemoryFallback(int(gb), SmallCoder)) // Use prompt
		return SmallCoder
	}
	logger.Logf(prompts.SystemMemoryFallback(int(gb), MicroCoder)) // Use prompt
	return MicroCoder
}

func (cfg *Config) setDefaultValues() {
	if cfg.SummaryModel == "" {
		cfg.SummaryModel = "lambda-ai:llama3.1-8b-instruct"
	}
	if cfg.WorkspaceModel == "" {
		cfg.WorkspaceModel = "lambda-ai:deepseek-llama3.3-70b"
	}
	if cfg.EditingModel == "" {
		cfg.EditingModel = "lambda-ai:deepseek-v3-0324" // Cheap decent model option would be "lambda-ai:qwen25-coder-32b-instruct"
	}
	if cfg.OrchestrationModel == "" {
		cfg.OrchestrationModel = cfg.EditingModel // Fallback to editing model if not specified
	}
	if cfg.EmbeddingModel == "" {
		cfg.EmbeddingModel = "mxbai-embed-large" // Default embedding model
	}
	if cfg.OllamaServerURL == "" {
		cfg.OllamaServerURL = "http://localhost:11434"
	}
	if cfg.OrchestrationMaxAttempts == 0 {
		cfg.OrchestrationMaxAttempts = 6 // Default max attempts for orchestration
	}
	if cfg.LocalModel == "" {
		cfg.LocalModel = getLocalModel(cfg.SkipPrompt) // Set local model based on system memory
	}
	// Ensure EnableSecurityChecks is explicitly set to false if not present in loaded config
	cfg.EnableSecurityChecks = true
}

func loadConfig(filePath string) (*Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var cfg Config
	// Provide default values for new fields if they are missing in older configs
	// These defaults will be overridden if the fields exist in the JSON.
	cfg.WorkspaceModel = ""                        // Default to empty, will fall back to SummaryModel
	cfg.OllamaServerURL = "http://localhost:11434" // Default Ollama URL
	cfg.EnableSecurityChecks = false               // Default to false for existing configs

	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	// Use setDefaultValues to ensure all fields have a value, especially new ones not in older configs.
	cfg.setDefaultValues()
	return &cfg, nil
}

func saveConfig(filePath string, cfg *Config) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

func createConfig(filePath string, skipPrompt bool) (*Config, error) {
	reader := bufio.NewReader(os.Stdin)
	// No logger needed here, as these are direct prompts for user input

	fmt.Print(prompts.EnterEditingModel("lambda-ai:deepseek-v3-0324")) // Use prompt
	editingModel, _ := reader.ReadString('\n')
	editingModel = strings.TrimSpace(editingModel)
	if editingModel == "" {
		editingModel = "lambda-ai:deepseek-v3-0324"
	}

	fmt.Print(prompts.EnterSummaryModel("lambda-ai:hermes3-8b")) // Use prompt
	summaryModel, _ := reader.ReadString('\n')
	summaryModel = strings.TrimSpace(summaryModel)
	if summaryModel == "" {
		summaryModel = "lambda-ai:hermes3-8b"
	}

	fmt.Print(prompts.EnterWorkspaceModel("lambda-ai:qwen25-coder-32b-instruct")) // Use prompt
	workspaceModel, _ := reader.ReadString('\n')
	workspaceModel = strings.TrimSpace(workspaceModel)
	if workspaceModel == "" {
		workspaceModel = "lambda-ai:qwen25-coder-32b-instruct"
	}

	fmt.Print(prompts.EnterOrchestrationModel("same as editing model")) // Use prompt
	orchestrationModel, _ := reader.ReadString('\n')
	orchestrationModel = strings.TrimSpace(orchestrationModel)
	if orchestrationModel == "" {
		orchestrationModel = editingModel
	}

	fmt.Print(prompts.TrackGitPrompt()) // Use prompt
	autoTrackGitStr, _ := reader.ReadString('\n')
	autoTrackGit := strings.TrimSpace(strings.ToLower(autoTrackGitStr)) == "yes"

	fmt.Print(prompts.EnableSecurityChecksPrompt()) // New prompt for security checks
	enableSecurityChecksStr, _ := reader.ReadString('\n')
	enableSecurityChecks := strings.TrimSpace(strings.ToLower(enableSecurityChecksStr)) == "yes"

	cfg := &Config{
		EditingModel:             editingModel,
		SummaryModel:             summaryModel,
		WorkspaceModel:           workspaceModel,
		OrchestrationModel:       orchestrationModel,
		LocalModel:               getLocalModel(skipPrompt),
		TrackWithGit:             autoTrackGit,
		EnableSecurityChecks:     enableSecurityChecks, // Set from user input
		OllamaServerURL:          "http://localhost:11434",
		EmbeddingModel:           "mxbai-embed-large", // Default embedding model
		OrchestrationMaxAttempts: 6,                   // Default max attempts for orchestration
	}

	cfg.setDefaultValues()

	if err := saveConfig(filePath, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func LoadOrInitConfig(skipPrompt bool) (*Config, error) {
	logger := utils.GetLogger(skipPrompt) // Get the logger instance

	_, currentConfigPath := getCurrentConfigPath()
	_, homeConfigPath := getHomeConfigPath()

	if _, err := os.Stat(currentConfigPath); err == nil {
		return loadConfig(currentConfigPath)
	}
	if _, err := os.Stat(homeConfigPath); err == nil {
		return loadConfig(homeConfigPath)
	}

	logger.LogUserInteraction(prompts.NoConfigFound()) // Use prompt
	_, homeConfigPath = getHomeConfigPath()
	cfg, err := createConfig(homeConfigPath, skipPrompt)
	if err != nil {
		return nil, fmt.Errorf("could not create initial config: %w", err)
	}
	logger.LogUserInteraction(prompts.ConfigSaved(homeConfigPath)) // Use prompt
	return cfg, nil
}

func InitConfig(skipPrompt bool) error {
	logger := utils.GetLogger(skipPrompt) // Get the logger instance

	_, currentConfigPath := getCurrentConfigPath()
	_, err := createConfig(currentConfigPath, skipPrompt)
	if err != nil {
		return err
	}
	logger.LogUserInteraction(prompts.ConfigSaved(currentConfigPath)) // Use prompt
	return nil
}
