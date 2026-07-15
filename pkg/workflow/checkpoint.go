//go:build !js

package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

func NewWorkflowExecutionState() *WorkflowExecutionState {
	return &WorkflowExecutionState{
		Version:       1,
		NextStepIndex: 0,
	}
}

func LoadWorkflowExecutionState(cfg *AgentWorkflowConfig) (*WorkflowExecutionState, error) {
	if !cfg.OrchestrationEnabled() || !cfg.OrchestrationResumeEnabled() {
		return NewWorkflowExecutionState(), nil
	}

	path := cfg.Orchestration.StateFile
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewWorkflowExecutionState(), nil
		}
		return nil, fmt.Errorf("failed to read orchestration state %q: %w", path, err)
	}

	// Gracefully handle empty or whitespace-only files.
	if len(bytes.TrimSpace(data)) == 0 {
		fmt.Fprintln(os.Stderr, "Warning: workflow state file is empty — starting fresh")
		return NewWorkflowExecutionState(), nil
	}

	var state WorkflowExecutionState
	if err := json.Unmarshal(data, &state); err != nil {
		// Corrupt JSON — log a warning and start fresh rather than failing
		// the entire workflow.
		fmt.Fprintf(os.Stderr, "Warning: workflow state file %q is corrupt (%v) — starting fresh\n", path, err)
		return NewWorkflowExecutionState(), nil
	}
	if state.Version == 0 {
		state.Version = 1
	}
	if state.NextStepIndex < 0 {
		state.NextStepIndex = 0
	}
	if state.Complete {
		return NewWorkflowExecutionState(), nil
	}
	return &state, nil
}

// WriteFileAtomic writes data to path atomically by writing to a temp file
// in the same directory and then renaming. This prevents partial/corrupt
// state files if the process crashes mid-write.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %q: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp_*.write")
	if err != nil {
		return fmt.Errorf("create temp file in %q: %w", dir, err)
	}
	tmpName := tmp.Name()

	// Clean up temp file on any failure.
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file %q: %w", tmpName, err)
	}
	// Sync to ensure data is on disk before rename.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp file %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %q → %q: %w", tmpName, path, err)
	}
	// After rename, the file at tmpName is gone, so don't try to remove it.
	cleanup = false
	return nil
}

func PersistWorkflowExecutionState(cfg *AgentWorkflowConfig, state *WorkflowExecutionState) error {
	if state == nil || !cfg.OrchestrationEnabled() {
		return nil
	}
	path := cfg.Orchestration.StateFile
	if path == "" {
		return errors.New("orchestration state file path is empty")
	}

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize orchestration state: %w", err)
	}
	if err := WriteFileAtomic(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write orchestration state %q: %w", path, err)
	}
	return nil
}

func ShouldRestoreWorkflowConversationState(state *WorkflowExecutionState) bool {
	if state == nil {
		return false
	}
	return state.InitialCompleted || state.NextStepIndex > 0 || state.HasError || strings.TrimSpace(state.FirstError) != ""
}

func RestoreWorkflowConversationState(chatAgent *agent.Agent, cfg *AgentWorkflowConfig, state *WorkflowExecutionState) error {
	if chatAgent == nil || cfg == nil || !cfg.OrchestrationEnabled() || !cfg.OrchestrationResumeEnabled() {
		return nil
	}
	if !ShouldRestoreWorkflowConversationState(state) {
		return nil
	}
	sessionID := strings.TrimSpace(cfg.Orchestration.ConversationSessionID)
	if sessionID == "" {
		return nil
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to resolve current working directory for workflow restore: %w", err)
	}
	restoredState, err := chatAgent.LoadStateScoped(sessionID, workingDir)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil
		}
		return fmt.Errorf("failed to load orchestration conversation session %q: %w", sessionID, err)
	}
	chatAgent.ApplyState(restoredState)
	return nil
}

func PersistWorkflowConversationState(chatAgent *agent.Agent, cfg *AgentWorkflowConfig) error {
	if chatAgent == nil || cfg == nil || !cfg.OrchestrationEnabled() {
		return nil
	}
	sessionID := strings.TrimSpace(cfg.Orchestration.ConversationSessionID)
	if sessionID == "" {
		return nil
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to resolve current working directory for workflow checkpoint: %w", err)
	}
	if err := chatAgent.SaveStateScoped(sessionID, workingDir); err != nil {
		return fmt.Errorf("failed to write orchestration conversation session %q: %w", sessionID, err)
	}
	return nil
}

func PersistWorkflowCheckpoint(cfg *AgentWorkflowConfig, state *WorkflowExecutionState, chatAgent *agent.Agent) error {
	if err := PersistWorkflowExecutionState(cfg, state); err != nil {
		return fmt.Errorf("failed to persist workflow checkpoint: %w", err)
	}
	return PersistWorkflowConversationState(chatAgent, cfg)
}

// LoopCheckpointFilePath returns the path to the lightweight fallback
// checkpoint file that stores just the TODO line number.
func LoopCheckpointFilePath(workDir string) string {
	return filepath.Join(workDir, ".sprout", "todo_loop_checkpoint.txt")
}

// PersistLoopCheckpoint writes just the line number to the fallback
// checkpoint file using an atomic write (temp file + rename).
func PersistLoopCheckpoint(workDir string, lineNum int) error {
	path := LoopCheckpointFilePath(workDir)
	data := []byte(fmt.Sprintf("%d\n", lineNum))
	if err := WriteFileAtomic(path, data, 0600); err != nil {
		return fmt.Errorf("failed to persist loop checkpoint: %w", err)
	}
	return nil
}

// RemoveLoopCheckpoint deletes the fallback checkpoint file, ignoring
// not-found errors.
func RemoveLoopCheckpoint(workDir string) {
	path := LoopCheckpointFilePath(workDir)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: failed to remove loop checkpoint %q: %v\n", path, err)
	}
}

// LoadLoopCheckpoint reads the fallback checkpoint file and returns
// the line number. Returns (0, nil) if the file doesn't exist.
func LoadLoopCheckpoint(workDir string) (int, error) {
	path := LoopCheckpointFilePath(workDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read loop checkpoint %q: %w", path, err)
	}

	lineStr := strings.TrimSpace(string(data))
	if lineStr == "" {
		return 0, nil
	}
	var lineNum int
	if _, err := fmt.Sscanf(lineStr, "%d", &lineNum); err != nil {
		// Corrupt file — log warning and treat as missing.
		fmt.Fprintf(os.Stderr, "Warning: loop checkpoint %q has invalid content %q — ignoring\n", path, lineStr)
		return 0, nil
	}
	if lineNum <= 0 {
		return 0, nil
	}
	return lineNum, nil
}

func EmitWorkflowOrchestrationEvent(cfg *AgentWorkflowConfig, eventType string, payload map[string]interface{}) error {
	if !cfg.OrchestrationEnabled() {
		return nil
	}
	path := cfg.Orchestration.EventsFile
	if path == "" {
		return errors.New("orchestration events file path is empty")
	}

	record := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"type":      strings.TrimSpace(eventType),
	}
	for k, v := range payload {
		record[k] = v
	}

	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to serialize orchestration event: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create orchestration events directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to open orchestration events file %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("failed to append orchestration event to %q: %w", path, err)
	}
	return nil
}

func WorkflowEffectiveStepProvider(chatAgent *agent.Agent, step AgentWorkflowStep) string {
	if strings.TrimSpace(step.Provider) != "" {
		return strings.TrimSpace(step.Provider)
	}
	return strings.TrimSpace(chatAgent.GetProvider())
}

func ShouldYieldBeforeWorkflowStep(cfg *AgentWorkflowConfig, state *WorkflowExecutionState, nextStep AgentWorkflowStep, chatAgent *agent.Agent) bool {
	if !cfg.OrchestrationEnabled() || !cfg.OrchestrationYieldOnProviderHandoff() {
		return false
	}
	lastProvider := strings.TrimSpace(state.LastProvider)
	if lastProvider == "" {
		return false
	}
	nextProvider := WorkflowEffectiveStepProvider(chatAgent, nextStep)
	if nextProvider == "" {
		return false
	}
	return !strings.EqualFold(lastProvider, nextProvider)
}
