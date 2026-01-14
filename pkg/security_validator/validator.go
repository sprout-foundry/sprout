package security_validator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/utils"
)

// RiskLevel represents the security risk level of an operation
type RiskLevel int

const (
	// RiskSafe means the operation is safe to execute immediately
	RiskSafe RiskLevel = 0
	// RiskCaution means the operation should be confirmed with the user
	RiskCaution RiskLevel = 1
	// RiskDangerous means the operation should be blocked or require explicit approval
	RiskDangerous RiskLevel = 2
)

// String returns the string representation of the risk level
func (r RiskLevel) String() string {
	switch r {
	case RiskSafe:
		return "SAFE"
	case RiskCaution:
		return "CAUTION"
	case RiskDangerous:
		return "DANGEROUS"
	default:
		return "UNKNOWN"
	}
}

// ValidationResult represents the result of a security validation
type ValidationResult struct {
	RiskLevel     RiskLevel `json:"risk_level"`
	Reasoning     string    `json:"reasoning"`
	Confidence    float64   `json:"confidence"`
	Timestamp     int64     `json:"timestamp"`
	ModelUsed     string    `json:"model_used"`
	LatencyMs     int64     `json:"latency_ms"`
	ShouldBlock   bool      `json:"should_block"`
	ShouldConfirm bool      `json:"should_confirm"`
}

// Validator handles LLM-based security validation using local llama.cpp
type Validator struct {
	config       *configuration.SecurityValidationConfig
	model        LLMModel
	modelPath    string
	logger       *utils.Logger
	interactive  bool
	debug        bool
}

// NewValidator creates a new security validator
func NewValidator(cfg *configuration.SecurityValidationConfig, logger *utils.Logger, interactive bool) (*Validator, error) {
	if cfg == nil {
		return nil, fmt.Errorf("security validation config is nil")
	}

	if !cfg.Enabled {
		return &Validator{
			config:     cfg,
			logger:     logger,
			interactive: interactive,
			debug:      false,
		}, nil
	}

	// Resolve model path
	modelPath := cfg.Model
	if modelPath == "" {
		// Use default model path if not specified
		configDir, err := configuration.GetConfigDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get config directory: %w", err)
		}
		modelPath = filepath.Join(configDir, "models", "qwen2.5-coder-0.5b-q4_k_m.gguf")
	}

	// Check if model file exists, if not, download it
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		if logger != nil {
			logger.Logf("Security validation model not found at %s", modelPath)
			logger.Logf("Downloading Qwen 2.5 Coder 0.5B Q4_K_M (~300MB)...")

			if err := downloadModel(modelPath, logger); err != nil {
				return nil, fmt.Errorf("failed to download security validation model: %w", err)
			}

			logger.Logf("✓ Model downloaded successfully")
		} else {
			return nil, fmt.Errorf("security validation model not found at %s and download not available (no logger)", modelPath)
		}
	}

	// Load the model
	var model LLMModel

	// Try to load the actual llama.cpp model
	// This will only work in production (when go-llama.cpp is built)
	llamaModel, loadErr := loadLlamaModel(modelPath)
	if loadErr != nil {
		return nil, fmt.Errorf("failed to load security validation model from %s: %w", modelPath, loadErr)
	}
	model = llamaModel

	return &Validator{
		config:     cfg,
		model:      model,
		modelPath:  modelPath,
		logger:     logger,
		interactive: interactive,
		debug:      false,
	}, nil
}

// downloadModel downloads the Qwen 2.5 Coder 0.5B model
func downloadModel(targetPath string, logger *utils.Logger) error {
	// Ensure target directory exists
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return fmt.Errorf("failed to create models directory: %w", err)
	}

	// Model URL from HuggingFace
	modelURL := "https://huggingface.co/Qwen/Qwen2.5-Coder-0.5B-GGUF/resolve/main/qwen2.5-coder-0.5b-q4_k_m.gguf"

	// Create temp file for download
	tempPath := targetPath + ".tmp"

	out, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer out.Close()

	// Download with progress tracking
	resp, err := http.Get(modelURL)
	if err != nil {
		return fmt.Errorf("failed to download model: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download model: HTTP %d", resp.StatusCode)
	}

	// Track progress
	totalSize := resp.ContentLength
	var downloaded int64
	buffer := make([]byte, 32*1024) // 32KB chunks
	lastLogTime := time.Now()

	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			written, err := out.Write(buffer[:n])
			if err != nil {
				return fmt.Errorf("failed to write to file: %w", err)
			}
			downloaded += int64(written)

			// Log progress every 2 seconds
			if time.Since(lastLogTime) > 2*time.Second {
				progress := float64(downloaded) / float64(totalSize) * 100
				logger.Logf("Downloading... %.1f%% (%d / %d bytes)", progress, downloaded, totalSize)
				lastLogTime = time.Now()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("download error: %w", err)
		}
	}

	// Rename temp file to final path
	if err := os.Rename(tempPath, targetPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to save downloaded model: %w", err)
	}

	return nil
}

// ValidateToolCall evaluates whether a tool call is safe to execute
func (v *Validator) ValidateToolCall(ctx context.Context, toolName string, args map[string]interface{}) (*ValidationResult, error) {
	startTime := time.Now()

	// If validation is disabled, return safe immediately
	if v.config == nil || !v.config.Enabled {
		return &ValidationResult{
			RiskLevel:     RiskSafe,
			Reasoning:     "Security validation is disabled",
			Confidence:    1.0,
			Timestamp:     time.Now().Unix(),
			ModelUsed:     "none",
			LatencyMs:     0,
			ShouldBlock:   false,
			ShouldConfirm: false,
		}, nil
	}

	// Pre-filter: Skip LLM validation for obviously safe operations
	if isObviouslySafe(toolName, args) {
		return &ValidationResult{
			RiskLevel:     RiskSafe,
			Reasoning:     "Obviously safe operation (read-only or informational)",
			Confidence:    1.0,
			Timestamp:     time.Now().Unix(),
			ModelUsed:     "prefilter",
			LatencyMs:     0,
			ShouldBlock:   false,
			ShouldConfirm: false,
		}, nil
	}

	// Check if model is loaded
	if v.model == nil {
		return &ValidationResult{
			RiskLevel:     RiskCaution,
			Reasoning:     "Security validation model not loaded. Please ensure the model file exists.",
			Confidence:    0.0,
			Timestamp:     time.Now().Unix(),
			ModelUsed:     v.modelPath,
			LatencyMs:     0,
			ShouldBlock:   false,
			ShouldConfirm: false,
		}, nil
	}

	// Create prompt for the LLM
	prompt := v.buildValidationPrompt(toolName, args)

	// Call the LLM
	response, err := v.callLLM(ctx, prompt)
	if err != nil {
		// If LLM call fails, log it but default to cautious
		return &ValidationResult{
			RiskLevel:     RiskCaution,
			Reasoning:     fmt.Sprintf("Security validation failed: %v. Defaulting to caution.", err),
			Confidence:    0.0,
			Timestamp:     time.Now().Unix(),
			ModelUsed:     v.modelPath,
			LatencyMs:     time.Since(startTime).Milliseconds(),
			ShouldBlock:   false,
			ShouldConfirm: false,
		}, nil
	}

	// Parse the response
	result, err := v.parseValidationResponse(response, startTime)
	if err != nil {
		return nil, fmt.Errorf("failed to parse validation response: %w", err)
	}

	// Apply threshold logic and request user confirmation if needed
	result = v.applyThreshold(result)

	// If confirmation is needed and in interactive mode, ask the user
	if result.ShouldConfirm && v.interactive && v.logger != nil {
		argsJSON, _ := json.Marshal(args)
		prompt := fmt.Sprintf("⚠️  Security Validation Warning\n\nTool: %s\nArguments: %s\n\nRisk Level: %s\nReasoning: %s\n\nDo you want to proceed? (yes/no): ",
			toolName, string(argsJSON), result.RiskLevel, result.Reasoning)

		if v.logger.AskForConfirmation(prompt, false, false) {
			// User approved, clear the confirmation flag
			result.ShouldConfirm = false
			result.ShouldBlock = false
		} else {
			// User rejected, block the operation
			result.ShouldConfirm = false
			result.ShouldBlock = true
			result.Reasoning = "User rejected the operation based on security warning"
		}
	}

	return result, nil
}

// buildValidationPrompt creates a prompt for security validation
func (v *Validator) buildValidationPrompt(toolName string, args map[string]interface{}) string {
	argsJSON, _ := json.Marshal(args)

	prompt := fmt.Sprintf(`You are a security validation assistant for an AI coding tool. Your job is to evaluate whether an operation is safe to execute without user supervision.

TOOL: %s
ARGUMENTS: %s

INSTRUCTIONS:
Evaluate this operation and classify it into one of three risk levels:

**RISK LEVEL 0 (SAFE)** - Execute immediately without user intervention:
- Read-only operations (reading files, listing directories)
- Informational commands (git status, ls, ps, git remote show)
- Building/testing in the workspace (go build, go test, make, go vet)
- Operations that have no side effects or are easily reversible
- Creating new files or directories in non-system locations
- Process listing and system information queries

**RISK LEVEL 1 (CAUTION)** - May need user confirmation:
- Modifications that could break things (git reset, git rebase, git amend)
- Single or multiple file deletions WITHOUT -rf (rm file.txt, rm file1 file2)
- Writing to configuration files in project directory
- Package management operations (npm install, pip install, go get, docker build)
- Build cleanup (make clean, git clean -fd)
- Deleting dependency directories (rm -rf node_modules, vendor/, target/) - recoverable via package manager
- Deleting lock files (package-lock.json, Podfile.lock, Gemfile.lock) - easily regenerated
- Permission changes (chmod +x, chmod 644) but NOT chmod 777
- Stopping services (systemctl stop) but NOT disabling (systemctl disable)
- In-place file editing (sed -i, tee to project files)
- Deleting build artifacts (rm -rf dist/, build/, out/) - easily rebuilt
- Operations that modify state but are recoverable

**RISK LEVEL 2 (DANGEROUS)** - Should require explicit user approval:
- ANY filesystem operations (mkfs, fdisk, parted - destroy data)
- Destructive system operations (dd if=/dev of=...)
- Writing to system directories (/usr, /etc, /bin, /sbin, /var, /opt)
- Force delete operations (git branch -D, git clean -ffd)
- Insecure permissions (chmod 777 - world-writable is security risk)
- Privilege escalation (sudo with destructive commands)
- Downloading executables to system directories (wget/curl to /usr/bin)
- Deleting git history (rm -rf .git) - permanent version history loss
- Deleting project source code (rm -rf src/, rm -rf *.go, rm -rf ~/project)
- Deleting user data or home directories (rm -rf ~/*)
- System persistence changes (systemctl disable/enable)
- Pipe to bash with curl/wget (arbitrary code execution)
- Recursive deletion of system directories or critical data
- Operations that could cause permanent data loss or security compromise

EXAMPLES FOR REFERENCE:
- "read_file main.go" → SAFE (read-only, no side effects)
- "shell_command: git status" → SAFE (informational)
- "shell_command: go test" → SAFE (standard operation)
- "shell_command: cat README.md" → SAFE (read file content)
- "shell_command: rm test.txt" → CAUTION (single file deletion, recoverable)
- "shell_command: rm file1.txt file2.txt" → CAUTION (multiple files but not recursive)
- "shell_command: rm -rf node_modules" → CAUTION (dependencies, recoverable via npm install)
- "shell_command: rm -rf vendor/" → CAUTION (dependencies, recoverable via bundle install)
- "shell_command: rm -rf dist/" → CAUTION (build artifacts, easily rebuilt)
- "shell_command: rm package-lock.json" → CAUTION (lock file, easily regenerated)
- "shell_command: git reset --hard HEAD" → CAUTION (destructive but recoverable)
- "shell_command: npm install express" → CAUTION (package management, recoverable)
- "shell_command: make clean" → CAUTION (build cleanup, recoverable)
- "shell_command: chmod +x script.sh" → CAUTION (permission change, reversible)
- "shell_command: rm -rf /tmp/test" → CAUTION (temp directory, not critical)
- "shell_command: rm -rf ~/important-project" → DANGEROUS (deletes user project/data)
- "shell_command: rm -rf .git" → DANGEROUS (deletes version history)
- "shell_command: rm -rf src/" → DANGEROUS (deletes source code)
- "shell_command: git branch -D feature" → DANGEROUS (force delete loses unmerged work)
- "shell_command: chmod 777 /file" → DANGEROUS (insecure permissions)
- "shell_command: mkfs.ext4 /dev/sdb1" → DANGEROUS (destroys filesystem)
- "shell_command: systemctl disable nginx" → DANGEROUS (persistent system change)
- "shell_command: rm -rf /usr/local/bin/app" → DANGEROUS (system directory + recursive)
- "write_file /usr/bin/app" → DANGEROUS (system directory, privilege escalation)

CRITICAL RULES:
- "-rf flag on dependencies" → CAUTION (node_modules, vendor/, bundle, pods) - recoverable via package manager
- "-rf flag on build artifacts" → CAUTION (dist/, build/, out/, target/, __pycache__) - easily rebuilt
- "-rf flag on lock files" → CAUTION (package-lock.json, Podfile.lock, Gemfile.lock) - easily regenerated
- "-rf flag on SOURCE CODE directories" → DANGEROUS (src/, lib/, include/, app/, components/) - permanent loss
- "-rf flag on .git" → DANGEROUS (version history) - permanent loss
- "-rf flag on home/project" → DANGEROUS (user data: ~/*, ~/project) - permanent loss
- "-rf flag on system dirs" → DANGEROUS (/usr, /etc, /var, /opt, /bin, /sbin) - system critical
- "-rf flag on temp" → CAUTION (/tmp/*, /var/tmp/*) - not critical
- Writing to /usr, /etc, /bin, /sbin, /var → Always DANGEROUS (2)
- "chmod 777" → Always DANGEROUS (2) (security risk)
- "git branch -D" → Always DANGEROUS (2) (force delete)
- mkfs/fdisk/parted → Always DANGEROUS (2) (filesystem ops)

IMPORTANT: The -rf flag alone doesn't determine risk - what's being deleted matters:
**CAUTION (recoverable):**
- Dependencies: node_modules, vendor, bundle, pods, .venv
- Build output: dist, build, out, target, bin, .next
- Cache: __pycache__, .cache, .gradle, node_modules/.cache
- Lock files: package-lock.json, Podfile.lock, Gemfile.lock, Cargo.lock

**DANGEROUS (permanent loss):**
- Source code: src/, lib/, include/, app/, components/, pages/
- Tests: tests/, spec/, test/, __tests__/
- Config: .git, .github/, config/, cfg/
- User data: ~/*, ~/Documents, ~/projects
- System: /usr, /etc, /var, /opt, /bin, /sbin

Be conservative but fair. Most operations in normal development workflows should be SAFE (0).

Return your response as JSON:
{
  "risk_level": 0,
  "reasoning": "Brief explanation of the risk level",
  "confidence": 0.95
}

Only return valid JSON, nothing else.`, toolName, string(argsJSON))

	return prompt
}

// callLLM calls the llama.cpp model for security validation
func (v *Validator) callLLM(ctx context.Context, prompt string) (string, error) {
	// Set timeout
	timeout := time.Duration(v.config.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	// Create a context with timeout for inference
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Run inference with llama.cpp through our interface
	tokens, err := v.model.Completion(ctx, prompt)

	if err != nil {
		return "", fmt.Errorf("llama.cpp inference failed: %w", err)
	}

	return tokens, nil
}

// parseValidationResponse parses the LLM response into a ValidationResult
func (v *Validator) parseValidationResponse(response string, startTime time.Time) (*ValidationResult, error) {
	// Try to extract JSON from the response
	response = strings.TrimSpace(response)

	// Remove markdown code blocks if present
	if strings.HasPrefix(response, "```json") {
		response = strings.TrimPrefix(response, "```json")
		response = strings.TrimSuffix(response, "```")
		response = strings.TrimSpace(response)
	} else if strings.HasPrefix(response, "```") {
		response = strings.TrimPrefix(response, "```")
		response = strings.TrimSuffix(response, "```")
		response = strings.TrimSpace(response)
	}

	// Parse JSON
	var result struct {
		RiskLevel  int     `json:"risk_level"`
		Reasoning  string  `json:"reasoning"`
		Confidence float64 `json:"confidence"`
	}

	if err := json.Unmarshal([]byte(response), &result); err != nil {
		// If JSON parsing fails, try to extract risk level from text
		return v.parseTextResponse(response, startTime)
	}

	// Validate risk level
	if result.RiskLevel < 0 || result.RiskLevel > 2 {
		return nil, fmt.Errorf("invalid risk level: %d", result.RiskLevel)
	}

	// Set default confidence if not provided
	if result.Confidence == 0 {
		result.Confidence = 0.8
	}

	return &ValidationResult{
		RiskLevel:     RiskLevel(result.RiskLevel),
		Reasoning:     result.Reasoning,
		Confidence:    result.Confidence,
		Timestamp:     time.Now().Unix(),
		ModelUsed:     v.modelPath,
		LatencyMs:     time.Since(startTime).Milliseconds(),
		ShouldBlock:   false,
		ShouldConfirm: false,
	}, nil
}

// parseTextResponse parses a non-JSON response
func (v *Validator) parseTextResponse(response string, startTime time.Time) (*ValidationResult, error) {
	responseLower := strings.ToLower(response)

	// Look for risk indicators
	riskLevel := RiskSafe
	if strings.Contains(responseLower, "dangerous") || strings.Contains(responseLower, "unsafe") ||
	   strings.Contains(responseLower, "risk: 2") || strings.Contains(responseLower, "block") {
		riskLevel = RiskDangerous
	} else if strings.Contains(responseLower, "caution") || strings.Contains(responseLower, "careful") ||
	   strings.Contains(responseLower, "risk: 1") || strings.Contains(responseLower, "confirm") {
		riskLevel = RiskCaution
	}

	return &ValidationResult{
		RiskLevel:     riskLevel,
		Reasoning:     response,
		Confidence:    0.6, // Lower confidence for text parsing
		Timestamp:     time.Now().Unix(),
		ModelUsed:     v.modelPath,
		LatencyMs:     time.Since(startTime).Milliseconds(),
		ShouldBlock:   false,
		ShouldConfirm: false,
	}, nil
}

// applyThreshold applies the configured threshold to the validation result
func (v *Validator) applyThreshold(result *ValidationResult) *ValidationResult {
	threshold := v.config.Threshold
	if threshold < 0 {
		threshold = 1 // Default to cautious
	} else if threshold > 2 {
		threshold = 2
	}

	// Determine if we should block or request confirmation based on risk level and threshold
	if int(result.RiskLevel) > threshold {
		// Risk level exceeds threshold - request user confirmation
		result.ShouldBlock = false
		result.ShouldConfirm = true
	} else if int(result.RiskLevel) == threshold {
		// Risk level equals threshold - request user confirmation
		result.ShouldBlock = false
		result.ShouldConfirm = true
	} else {
		// Risk level below threshold - allow
		result.ShouldBlock = false
		result.ShouldConfirm = false
	}

	return result
}

// isObviouslySafe checks if an operation is clearly safe without needing LLM validation
// This pre-filters read-only and informational operations to reduce latency
func isObviouslySafe(toolName string, args map[string]interface{}) bool {
	// List of obviously safe tools (read-only and informational)
	safeTools := map[string]bool{
		// Read operations
		"read_file":        true,
		"glob":             true,
		"grep":             true,
		"list_directory":   true,

		// Informational git commands
		"git_status":       true,
		"git_log":          true,
		"git_diff":         true,
		"git_show":         true,
		"git_branch":       true,

		// Informational system commands
		"list_processes":   true,
		"get_file_info":    true,

		// Build and test operations (in workspace)
		"build":            true,
		"test":             true,
	}

	// Check if tool is in the safe list
	if safeTools[toolName] {
		return true
	}

	// For shell_command, check if it's obviously safe
	if toolName == "shell_command" {
		command, ok := args["command"].(string)
		if !ok {
			return false
		}

		// List of safe shell commands
		safeCommands := []string{
			// Informational
			"git status",
			"git log",
			"git diff",
			"git show",
			"git branch",
			"git remote",
			"git config --get",

			// Listing
			"ls ",
			"ll ",
			"la ",
			"find ",
			"which ",
			"whereis ",

			// Build and test
			"go build",
			"go test",
			"go run",
			"go fmt",
			"go vet",
			"make ",
			"npm run build",
			"npm test",
			"npm run test",
			"cargo build",
			"cargo test",
			"cargo check",

			// Informational system
			"ps ",
			"top",
			"htop",
			"df ",
			"du ",
			"uname",
			"env",
		}

		commandLower := strings.ToLower(strings.TrimSpace(command))
		for _, safe := range safeCommands {
			if strings.HasPrefix(commandLower, strings.ToLower(safe)) {
				return true
			}
		}

		// Check for read-only file operations
		if strings.HasPrefix(commandLower, "cat ") ||
		   strings.HasPrefix(commandLower, "head ") ||
		   strings.HasPrefix(commandLower, "tail ") ||
		   strings.HasPrefix(commandLower, "less ") ||
		   strings.HasPrefix(commandLower, "more ") {
			return true
		}
	}

	return false
}

// SetDebug enables or disables debug mode
func (v *Validator) SetDebug(debug bool) {
	v.debug = debug
}
