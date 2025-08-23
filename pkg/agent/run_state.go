package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/alantheprice/ledit/pkg/types"
)

// serializableAgentState captures resumable parts of AgentContext
type serializableAgentState struct {
	UserIntent         string                 `json:"user_intent"`
	IterationCount     int                    `json:"iteration_count"`
	MaxIterations      int                    `json:"max_iterations"`
	IsCompleted        bool                   `json:"is_completed"`
	IntentAnalysis     *IntentAnalysis        `json:"intent_analysis,omitempty"`
	CurrentPlan        *EditPlan              `json:"current_plan,omitempty"`
	ExecutedOperations []string               `json:"executed_operations"`
	Errors             []string               `json:"errors"`
	ValidationResults  []string               `json:"validation_results"`
	ValidationFailed   bool                   `json:"validation_failed"`
	TokenUsage         *types.AgentTokenUsage `json:"token_usage,omitempty"`
	SavedAt            time.Time              `json:"saved_at"`
}

const runStatePath = ".ledit/run_state.json"

func toSerializable(ctx *AgentContext) *serializableAgentState {
	return &serializableAgentState{
		UserIntent:         ctx.UserIntent,
		IterationCount:     ctx.IterationCount,
		MaxIterations:      ctx.MaxIterations,
		IsCompleted:        ctx.IsCompleted,
		IntentAnalysis:     ctx.IntentAnalysis,
		CurrentPlan:        ctx.CurrentPlan,
		ExecutedOperations: append([]string{}, ctx.ExecutedOperations...),
		Errors:             append([]string{}, ctx.Errors...),
		ValidationResults:  append([]string{}, ctx.ValidationResults...),
		ValidationFailed:   ctx.ValidationFailed,
		TokenUsage:         ctx.TokenUsage,
		SavedAt:            time.Now(),
	}
}

func (s *serializableAgentState) applyTo(ctx *AgentContext) {
	ctx.UserIntent = s.UserIntent
	ctx.IterationCount = s.IterationCount
	ctx.MaxIterations = s.MaxIterations
	ctx.IsCompleted = s.IsCompleted
	ctx.IntentAnalysis = s.IntentAnalysis
	ctx.CurrentPlan = s.CurrentPlan
	ctx.ExecutedOperations = append([]string{}, s.ExecutedOperations...)
	ctx.Errors = append([]string{}, s.Errors...)
	ctx.ValidationResults = append([]string{}, s.ValidationResults...)
	ctx.ValidationFailed = s.ValidationFailed
	if s.TokenUsage != nil && ctx.TokenUsage != nil {
		*ctx.TokenUsage = *s.TokenUsage
	}
}

// SaveAgentState writes the current agent state to disk
func SaveAgentState(ctx *AgentContext) error {
	if err := os.MkdirAll(filepath.Dir(runStatePath), 0755); err != nil {
		return err
	}
	f, err := os.Create(runStatePath)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(toSerializable(ctx))
}

// LoadAgentState loads the agent state from disk
func LoadAgentState() (*serializableAgentState, error) {
	b, err := os.ReadFile(runStatePath)
	if err != nil {
		return nil, err
	}
	var s serializableAgentState
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// HasSavedAgentState returns true if a saved state exists
func HasSavedAgentState() bool {
	if st, err := os.Stat(runStatePath); err == nil && !st.IsDir() {
		return true
	}
	return false
}

// ClearAgentState removes the saved state file
func ClearAgentState() error {
	if !HasSavedAgentState() {
		return nil
	}
	if err := os.Remove(runStatePath); err != nil {
		return fmt.Errorf("failed to remove run state: %w", err)
	}
	return nil
}
