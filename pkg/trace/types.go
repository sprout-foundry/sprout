package trace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// RunMetadata represents metadata for a single ledit invocation/run
type RunMetadata struct {
	RunID        string            `json:"run_id"`
	Timestamp     string            `json:"timestamp"`
	Provider      string            `json:"provider"`
	Model         string            `json:"model"`
	ReasoningMode string            `json:"reasoning_mode"`
	Persona       string            `json:"persona"`
	WorkflowName  string            `json:"workflow_name"`
	WorkflowIndex int               `json:"workflow_index"`
	EnvConfig    map[string]string `json:"env_config"`
}

// TurnRecord represents a single model turn (request + response)
type TurnRecord struct {
	RunID            string          `json:"run_id"`
	TurnIndex         int             `json:"turn_index"`
	SystemPrompt      string          `json:"system_prompt"`
	UserPrompt        string          `json:"user_prompt"`
	UserPromptOriginal string          `json:"user_prompt_original"` // Pre-truncation
	MessagesSent      []api.Message  `json:"messages_sent"`
	ToolSchemaPayload json.RawMessage `json:"tool_schema_payload"`
	RawResponse      string          `json:"raw_response"`
	ParsedToolCalls  []api.ToolCall `json:"parsed_tool_calls"`
	ParserErrors     []string        `json:"parser_errors"`
	FallbackUsed     bool            `json:"fallback_used"`
	FallbackOutput   string          `json:"fallback_output"`
	MachineLabels     []string        `json:"machine_labels"`
	Timestamp         string          `json:"timestamp"`
}

// ToolCallRecord represents a single tool execution
type ToolCallRecord struct {
	RunID             string                 `json:"run_id"`
	TurnIndex          int                    `json:"turn_index"` // Index within turn
	ToolIndex         int                    `json:"tool_index"` // Index within turn
	ToolName          string                 `json:"tool_name"`
	Args              map[string]interface{} `json:"args"`
	ArgsNormalized     map[string]interface{} `json:"args_normalized"`
	Success           bool                   `json:"success"`
	FullResult        string                 `json:"full_result"`   // Untruncated
	ModelResult       string                 `json:"model_result"`  // As model sees it
	ErrorCategory     string                 `json:"error_category"` // validation, unknown_tool, timeout, etc.
	ErrorMessage      string                 `json:"error_message"`
	MachineLabels      []string               `json:"machine_labels"`
	Timestamp         string                 `json:"timestamp"`
}

// ArtifactManifest represents filesystem outputs from a run
type ArtifactManifest struct {
	RunID      string         `json:"run_id"`
	RelativePath string         `json:"relative_path"`
	SizeBytes   int64          `json:"size_bytes"`
	Hash        string         `json:"hash"` // SHA-256
	ArtifactType string         `json:"artifact_type"` // file_edit, file_create, etc.
	MachineLabels []string       `json:"machine_labels"`
	Timestamp    string         `json:"timestamp"`
}

// Machine label constants
const (
	// Path violations
	LabelPathViolationAbsolute    = "path_violation_absolute"
	LabelPathViolationNested     = "path_violation_nested"
	LabelPathViolationDisallowed  = "path_violation_disallowed_prefix"

	// Schema violations
	LabelSchemaEnvelopeViolation = "schema_envelope_violation"
	LabelLayoutViolation         = "layout_violation"

	// Tool call issues
	LabelToolCallValidationFailure = "tool_call_validation_failure"
	LabelToolCallUnknownTool      = "tool_call_unknown_tool"
	LabelToolCallTimeout         = "tool_call_timeout"
	LabelToolCallExecutionError  = "tool_call_execution_error"
)

// TraceSession manages dataset collection for a single ledit run
type TraceSession struct {
	mu          sync.RWMutex
	RunID       string
	RunDir      string
	RunsFile    *jsonlWriter
	TurnsFile   *jsonlWriter
	ToolsFile   *jsonlWriter
	ArtifactsFile *jsonlWriter
	Metadata    RunMetadata
	IsEnabled   bool
	closed      bool
}

// GetRunID returns the run ID
func (s *TraceSession) GetRunID() string {
	return s.RunID
}

// GetRunDir returns the run directory path
func (s *TraceSession) GetRunDir() string {
	return s.RunDir
}

// NewTraceSession creates a new trace session
func NewTraceSession(traceDir, provider, model string) (*TraceSession, error) {
	if traceDir == "" {
		return &TraceSession{IsEnabled: false}, nil
	}

	var err error
	now := time.Now()
	runID := now.Format("20060102_150405") + "_" + randomID(6)
	runDir := filepath.Join(traceDir, runID)

	if err = os.MkdirAll(runDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create trace run directory: %w", err)
	}

	// Track created writers for cleanup in case of partial initialization
	var runsWriter, turnsWriter, toolsWriter, artifactsWriter *jsonlWriter

	// Defer cleanup in case of error during initialization
	defer func() {
		if err != nil {
			// Close any writers that were successfully created
			if artifactsWriter != nil {
				artifactsWriter.Close()
			}
			if toolsWriter != nil {
				toolsWriter.Close()
			}
			if turnsWriter != nil {
				turnsWriter.Close()
			}
			if runsWriter != nil {
				runsWriter.Close()
			}
		}
	}()

	runsWriter, err = newJSONLWriter(filepath.Join(runDir, "runs.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("failed to create runs writer: %w", err)
	}

	turnsWriter, err = newJSONLWriter(filepath.Join(runDir, "turns.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("failed to create turns writer: %w", err)
	}

	toolsWriter, err = newJSONLWriter(filepath.Join(runDir, "tool_calls.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("failed to create tools writer: %w", err)
	}

	artifactsWriter, err = newJSONLWriter(filepath.Join(runDir, "artifacts_manifest.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("failed to create artifacts writer: %w", err)
	}

	metadata := RunMetadata{
		RunID:        runID,
		Timestamp:     now.UTC().Format(time.RFC3339),
		Provider:      provider,
		Model:         model,
		ReasoningMode: "",
		Persona:       "",
		WorkflowName:  "",
		WorkflowIndex: 0,
		EnvConfig:     collectEnvConfig(),
	}

	session := &TraceSession{
		RunID:        runID,
		RunDir:       runDir,
		RunsFile:     runsWriter,
		TurnsFile:    turnsWriter,
		ToolsFile:    toolsWriter,
		ArtifactsFile: artifactsWriter,
		Metadata:     metadata,
		IsEnabled:    true,
	}

	// Write run metadata - if this fails, close all writers and return error
	if err := session.RunsFile.Write(metadata); err != nil {
		// Clean up writers before returning error
		if artifactsWriter != nil {
			artifactsWriter.Close()
		}
		if toolsWriter != nil {
			toolsWriter.Close()
		}
		if turnsWriter != nil {
			turnsWriter.Close()
		}
		if runsWriter != nil {
			runsWriter.Close()
		}
		return nil, fmt.Errorf("failed to write run metadata: %w", err)
	}

	return session, nil
}

// Close closes all file writers
func (s *TraceSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsEnabled || s.closed {
		return nil
	}

	s.closed = true

	var errs []error
	if s.RunsFile != nil {
		if err := s.RunsFile.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.TurnsFile != nil {
		if err := s.TurnsFile.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.ToolsFile != nil {
		if err := s.ToolsFile.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.ArtifactsFile != nil {
		if err := s.ArtifactsFile.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// RecordTurn records a model turn
func (s *TraceSession) RecordTurn(record TurnRecord) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.IsEnabled || s.closed {
		return nil
	}
	return s.TurnsFile.Write(record)
}

// RecordToolCall records a tool execution
func (s *TraceSession) RecordToolCall(record ToolCallRecord) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.IsEnabled || s.closed {
		return nil
	}
	return s.ToolsFile.Write(record)
}

// RecordArtifact records a filesystem output
func (s *TraceSession) RecordArtifact(record ArtifactManifest) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.IsEnabled || s.closed {
		return nil
	}
	return s.ArtifactsFile.Write(record)
}

// Helper functions

func randomID(length int) string {
	const charset = "0123456789abcdef"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[i%len(charset)]
	}
	return string(b)
}

func collectEnvConfig() map[string]string {
	config := make(map[string]string)

	// Truncation limits
	if v := os.Getenv("LEDIT_INTERACTIVE_INPUT_MAX_CHARS"); v != "" {
		config["interactive_input_max_chars"] = v
	}
	if v := os.Getenv("LEDIT_AUTOMATION_INPUT_MAX_CHARS"); v != "" {
		config["automation_input_max_chars"] = v
	}
	if v := os.Getenv("LEDIT_USER_INPUT_MAX_CHARS"); v != "" {
		config["user_input_max_chars"] = v
	}
	if v := os.Getenv("LEDIT_READ_FILE_MAX_BYTES"); v != "" {
		config["read_file_max_bytes"] = v
	}
	if v := os.Getenv("LEDIT_SHELL_HEAD_TOKENS"); v != "" {
		config["shell_head_tokens"] = v
	}
	if v := os.Getenv("LEDIT_SHELL_TAIL_TOKENS"); v != "" {
		config["shell_tail_tokens"] = v
	}
	if v := os.Getenv("LEDIT_VISION_MAX_TEXT_CHARS"); v != "" {
		config["vision_max_text_chars"] = v
	}
	if v := os.Getenv("LEDIT_SEARCH_MAX_BYTES"); v != "" {
		config["search_max_bytes"] = v
	}
	if v := os.Getenv("LEDIT_FETCH_URL_MAX_CHARS"); v != "" {
		config["fetch_url_max_chars"] = v
	}

	// Token caps
	if v := os.Getenv("LEDIT_SUBAGENT_MAX_TOKENS"); v != "" {
		config["subagent_max_tokens"] = v
	}

	// Feature flags
	if v := os.Getenv("LEDIT_SELF_REVIEW_MODE"); v != "" {
		config["self_review_mode"] = v
	}
	if v := os.Getenv("LEDIT_NO_SUBAGENT_MODE"); v != "" {
		config["no_subagent_mode"] = v
	}
	if v := os.Getenv("LEDIT_ISOLATED_CONFIG"); v != "" {
		config["isolated_config"] = v
	}

	return config
}

// hashFile computes SHA-256 hash of a file
func hashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}
