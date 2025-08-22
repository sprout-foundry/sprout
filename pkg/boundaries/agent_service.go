package boundaries

import (
	"context"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/utils"
)

// AgentService defines the core agent functionality interface
// This provides a clean boundary between agent orchestration and external consumers
type AgentService interface {
	// ExecuteAgent runs an agent with the given intent and configuration
	ExecuteAgent(ctx context.Context, intent string, cfg *config.Config, logger *utils.Logger) error

	// GetAgentStatus returns the current status of an agent execution
	GetAgentStatus(executionID string) (*AgentStatus, error)

	// CancelAgent cancels a running agent execution
	CancelAgent(executionID string) error

	// ListAgentExecutions returns a list of recent agent executions
	ListAgentExecutions(limit int) ([]*AgentExecutionSummary, error)

	// GetExecutionResult returns the result of a completed agent execution
	GetExecutionResult(executionID string) (*AgentExecutionResult, error)
}

// AgentStatus represents the current status of an agent execution
type AgentStatus struct {
	ExecutionID   string
	State         AgentExecutionState
	Progress      float64 // 0.0 to 1.0
	CurrentStep   string
	StartTime     time.Time
	EstimatedTime time.Duration
	Error         error
}

// AgentExecutionState represents the state of an agent execution
type AgentExecutionState int

const (
	AgentStatePending AgentExecutionState = iota
	AgentStateRunning
	AgentStateCompleted
	AgentStateFailed
	AgentStateCancelled
)

// AgentExecutionSummary provides a summary of an agent execution
type AgentExecutionSummary struct {
	ExecutionID string
	Intent      string
	State       AgentExecutionState
	StartTime   time.Time
	EndTime     *time.Time
	Duration    time.Duration
	Success     bool
	ErrorMsg    string
}

// AgentExecutionResult contains the detailed result of an agent execution
type AgentExecutionResult struct {
	ExecutionID        string
	Intent             string
	Success            bool
	Error              error
	StartTime          time.Time
	EndTime            time.Time
	Duration           time.Duration
	ExecutedOperations []string
	TokenUsage         *AgentTokenUsageSummary
	FilesModified      []string
	ValidationResults  []ValidationResult
}

// AgentTokenUsageSummary provides a summary of token usage
type AgentTokenUsageSummary struct {
	TotalTokens      int
	PromptTokens     int
	CompletionTokens int
	CostEstimate     float64
	Model            string
}

// ValidationResult represents a validation result
type ValidationResult struct {
	Type    string
	Success bool
	Message string
	Details map[string]interface{}
}

// AgentExecutionRequest represents a request to execute an agent
type AgentExecutionRequest struct {
	Intent      string
	Config      *config.Config
	Timeout     time.Duration
	Priority    int
	CallbackURL string // For async notifications
}

// AgentExecutionResponse represents the response to an agent execution request
type AgentExecutionResponse struct {
	ExecutionID       string
	Accepted          bool
	Error             error
	EstimatedDuration time.Duration
}

// AgentCallback defines a callback interface for agent events
type AgentCallback interface {
	OnAgentStarted(executionID string)
	OnAgentProgress(executionID string, progress float64, step string)
	OnAgentCompleted(executionID string, result *AgentExecutionResult)
	OnAgentFailed(executionID string, err error)
}

// AgentFactory creates agent service instances
type AgentFactory interface {
	CreateAgentService() AgentService
	CreateAgentServiceWithConfig(cfg *config.Config) AgentService
}

// DefaultAgentService provides a default implementation of AgentService
type DefaultAgentService struct {
	config     *config.Config
	logger     *utils.Logger
	callbacks  []AgentCallback
	executions map[string]*AgentExecution
}

// NewDefaultAgentService creates a new default agent service
func NewDefaultAgentService(cfg *config.Config, logger *utils.Logger) *DefaultAgentService {
	return &DefaultAgentService{
		config:     cfg,
		logger:     logger,
		callbacks:  make([]AgentCallback, 0),
		executions: make(map[string]*AgentExecution),
	}
}

// AgentExecution represents an ongoing agent execution
type AgentExecution struct {
	ID         string
	Request    *AgentExecutionRequest
	Status     *AgentStatus
	Result     *AgentExecutionResult
	CancelFunc context.CancelFunc
	StartTime  time.Time
}

// AddCallback adds a callback for agent events
func (s *DefaultAgentService) AddCallback(callback AgentCallback) {
	s.callbacks = append(s.callbacks, callback)
}

// RemoveCallback removes a callback
func (s *DefaultAgentService) RemoveCallback(callback AgentCallback) {
	for i, cb := range s.callbacks {
		if cb == callback {
			s.callbacks = append(s.callbacks[:i], s.callbacks[i+1:]...)
			break
		}
	}
}

// notifyCallbacks notifies all callbacks of an event
func (s *DefaultAgentService) notifyCallbacks(fn func(AgentCallback)) {
	for _, callback := range s.callbacks {
		fn(callback)
	}
}

// ExecuteAgent implements AgentService.ExecuteAgent
func (s *DefaultAgentService) ExecuteAgent(ctx context.Context, intent string, cfg *config.Config, logger *utils.Logger) error {
	// Create execution request
	request := &AgentExecutionRequest{
		Intent:  intent,
		Config:  cfg,
		Timeout: 5 * time.Minute, // Default timeout
	}

	// Execute synchronously for now
	// In a real implementation, this would be asynchronous
	return s.executeAgentSync(ctx, request)
}

// GetAgentStatus implements AgentService.GetAgentStatus
func (s *DefaultAgentService) GetAgentStatus(executionID string) (*AgentStatus, error) {
	execution, exists := s.executions[executionID]
	if !exists {
		return nil, utils.NewUserError("execution not found: "+executionID, nil)
	}
	return execution.Status, nil
}

// CancelAgent implements AgentService.CancelAgent
func (s *DefaultAgentService) CancelAgent(executionID string) error {
	execution, exists := s.executions[executionID]
	if !exists {
		return utils.NewUserError("execution not found: "+executionID, nil)
	}

	if execution.CancelFunc != nil {
		execution.CancelFunc()
		execution.Status.State = AgentStateCancelled
	}

	return nil
}

// ListAgentExecutions implements AgentService.ListAgentExecutions
func (s *DefaultAgentService) ListAgentExecutions(limit int) ([]*AgentExecutionSummary, error) {
	if limit <= 0 {
		limit = 10
	}

	summaries := make([]*AgentExecutionSummary, 0, len(s.executions))
	count := 0

	for _, execution := range s.executions {
		if count >= limit {
			break
		}

		summary := &AgentExecutionSummary{
			ExecutionID: execution.ID,
			Intent:      execution.Request.Intent,
			State:       execution.Status.State,
			StartTime:   execution.StartTime,
			Success:     execution.Result != nil && execution.Result.Success,
		}

		if execution.Result != nil {
			summary.EndTime = &execution.Result.EndTime
			summary.Duration = execution.Result.Duration
		}

		if execution.Result != nil && execution.Result.Error != nil {
			summary.ErrorMsg = execution.Result.Error.Error()
		}

		summaries = append(summaries, summary)
		count++
	}

	return summaries, nil
}

// GetExecutionResult implements AgentService.GetExecutionResult
func (s *DefaultAgentService) GetExecutionResult(executionID string) (*AgentExecutionResult, error) {
	execution, exists := s.executions[executionID]
	if !exists {
		return nil, utils.NewUserError("execution not found: "+executionID, nil)
	}

	if execution.Result == nil {
		return nil, utils.NewUserError("execution not completed: "+executionID, nil)
	}

	return execution.Result, nil
}

// executeAgentSync executes an agent synchronously
func (s *DefaultAgentService) executeAgentSync(ctx context.Context, request *AgentExecutionRequest) error {
	// This is a placeholder implementation
	// In a real system, this would call the actual agent execution logic

	executionID := utils.GenerateRequestHash(request.Intent)

	execution := &AgentExecution{
		ID:        executionID,
		Request:   request,
		StartTime: time.Now(),
		Status: &AgentStatus{
			ExecutionID: executionID,
			State:       AgentStateRunning,
			Progress:    0.0,
			StartTime:   time.Now(),
		},
	}

	s.executions[executionID] = execution

	// Notify callbacks
	s.notifyCallbacks(func(cb AgentCallback) {
		cb.OnAgentStarted(executionID)
	})

	// Simulate agent execution
	// In a real implementation, this would call the actual agent
	time.Sleep(100 * time.Millisecond) // Simulate work

	// Mark as completed
	execution.Status.State = AgentStateCompleted
	execution.Status.Progress = 1.0
	execution.Result = &AgentExecutionResult{
		ExecutionID: executionID,
		Intent:      request.Intent,
		Success:     true,
		StartTime:   execution.StartTime,
		EndTime:     time.Now(),
		Duration:    time.Since(execution.StartTime),
	}

	// Notify callbacks
	s.notifyCallbacks(func(cb AgentCallback) {
		cb.OnAgentCompleted(executionID, execution.Result)
	})

	return nil
}
