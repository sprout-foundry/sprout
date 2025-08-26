package agent

import (
	"time"

	"github.com/alantheprice/ledit/internal/domain/todo"
)

// IntentType represents the type of user intent
type IntentType string

const (
	IntentTypeCodeGeneration IntentType = "code_generation"
	IntentTypeAnalysis       IntentType = "analysis"
	IntentTypeRefactoring    IntentType = "refactoring"
	IntentTypeDocumentation  IntentType = "documentation"
	IntentTypeQuestion       IntentType = "question"
	IntentTypeMultiStep      IntentType = "multi_step"
	IntentTypeOrchestration  IntentType = "orchestration"
)

// ComplexityLevel represents the estimated complexity of the intent
type ComplexityLevel int

const (
	ComplexitySimple   ComplexityLevel = 1
	ComplexityModerate ComplexityLevel = 2
	ComplexityComplex  ComplexityLevel = 3
	ComplexityCritical ComplexityLevel = 4
)

// ExecutionStrategy represents how the intent should be executed
type ExecutionStrategy string

const (
	StrategyQuickEdit   ExecutionStrategy = "quick_edit"
	StrategyFullEdit    ExecutionStrategy = "full_edit"
	StrategyAnalyze     ExecutionStrategy = "analyze"
	StrategyOrchestrate ExecutionStrategy = "orchestrate"
)

// IntentAnalysis represents the analyzed user intent
type IntentAnalysis struct {
	Type            IntentType        `json:"type"`
	Complexity      ComplexityLevel   `json:"complexity"`
	Strategy        ExecutionStrategy `json:"strategy"`
	TargetFiles     []string          `json:"target_files"`
	RequiredContext []string          `json:"required_context"`
	EstimatedTime   time.Duration     `json:"estimated_time"`
	Confidence      float64           `json:"confidence"`
	Keywords        []string          `json:"keywords"`
	Description     string            `json:"description"`
}

// ExecutionPlan represents a plan for executing an intent
type ExecutionPlan struct {
	ID            string            `json:"id"`
	IntentID      string            `json:"intent_id"`
	Analysis      IntentAnalysis    `json:"analysis"`
	TodoList      *todo.TodoList    `json:"todo_list"`
	Strategy      ExecutionStrategy `json:"strategy"`
	CreatedAt     time.Time         `json:"created_at"`
	EstimatedTime time.Duration     `json:"estimated_time"`
	Dependencies  []string          `json:"dependencies"`
}

// ExecutionResult represents the result of executing an intent
type ExecutionResult struct {
	ID          string                 `json:"id"`
	PlanID      string                 `json:"plan_id"`
	Success     bool                   `json:"success"`
	CompletedAt time.Time              `json:"completed_at"`
	Duration    time.Duration          `json:"duration"`
	Changes     []FileChange           `json:"changes"`
	Output      string                 `json:"output"`
	Error       string                 `json:"error,omitempty"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// FileChange represents a change made to a file
type FileChange struct {
	Path       string     `json:"path"`
	Type       ChangeType `json:"type"`
	OldContent string     `json:"old_content,omitempty"`
	NewContent string     `json:"new_content,omitempty"`
	LineStart  int        `json:"line_start,omitempty"`
	LineEnd    int        `json:"line_end,omitempty"`
}

// ChangeType represents the type of change made to a file
type ChangeType string

const (
	ChangeTypeCreate ChangeType = "create"
	ChangeTypeModify ChangeType = "modify"
	ChangeTypeDelete ChangeType = "delete"
	ChangeTypeRename ChangeType = "rename"
)

// AgentContext represents the context available to an agent during execution
type AgentContext struct {
	SessionID      string                 `json:"session_id"`
	UserIntent     string                 `json:"user_intent"`
	WorkspaceRoot  string                 `json:"workspace_root"`
	Configuration  map[string]interface{} `json:"configuration"`
	PersistentData map[string]interface{} `json:"persistent_data"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

// AgentState represents the current state of agent execution
type AgentState struct {
	ID             string         `json:"id"`
	SessionID      string         `json:"session_id"`
	CurrentPlan    *ExecutionPlan `json:"current_plan,omitempty"`
	CurrentTodo    *todo.Todo     `json:"current_todo,omitempty"`
	Status         AgentStatus    `json:"status"`
	Progress       float64        `json:"progress"`
	StartedAt      time.Time      `json:"started_at"`
	LastActivityAt time.Time      `json:"last_activity_at"`
	Context        *AgentContext  `json:"context"`
}

// AgentStatus represents the current status of the agent
type AgentStatus string

const (
	StatusIdle       AgentStatus = "idle"
	StatusAnalyzing  AgentStatus = "analyzing"
	StatusPlanning   AgentStatus = "planning"
	StatusExecuting  AgentStatus = "executing"
	StatusValidating AgentStatus = "validating"
	StatusCompleted  AgentStatus = "completed"
	StatusFailed     AgentStatus = "failed"
	StatusPaused     AgentStatus = "paused"
)

// WorkflowEvent represents an event that occurred during workflow execution
type WorkflowEvent struct {
	ID        string                 `json:"id"`
	SessionID string                 `json:"session_id"`
	Type      EventType              `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	Message   string                 `json:"message"`
}

// EventType represents the type of workflow event
type EventType string

const (
	EventTypeIntentReceived   EventType = "intent_received"
	EventTypeAnalysisStarted  EventType = "analysis_started"
	EventTypeAnalysisComplete EventType = "analysis_complete"
	EventTypePlanCreated      EventType = "plan_created"
	EventTypeTodoStarted      EventType = "todo_started"
	EventTypeTodoCompleted    EventType = "todo_completed"
	EventTypeTodoFailed       EventType = "todo_failed"
	EventTypeWorkflowComplete EventType = "workflow_complete"
	EventTypeWorkflowFailed   EventType = "workflow_failed"
	EventTypeUserQuestion     EventType = "user_question"
	EventTypeValidationFailed EventType = "validation_failed"
)

// Methods for AgentContext

// UpdatePersistentData updates a value in the persistent data
func (ac *AgentContext) UpdatePersistentData(key string, value interface{}) {
	if ac.PersistentData == nil {
		ac.PersistentData = make(map[string]interface{})
	}
	ac.PersistentData[key] = value
	ac.UpdatedAt = time.Now()
}

// GetPersistentData retrieves a value from persistent data
func (ac *AgentContext) GetPersistentData(key string) (interface{}, bool) {
	if ac.PersistentData == nil {
		return nil, false
	}
	value, exists := ac.PersistentData[key]
	return value, exists
}

// UpdateConfiguration updates a configuration value
func (ac *AgentContext) UpdateConfiguration(key string, value interface{}) {
	if ac.Configuration == nil {
		ac.Configuration = make(map[string]interface{})
	}
	ac.Configuration[key] = value
	ac.UpdatedAt = time.Now()
}

// GetConfiguration retrieves a configuration value
func (ac *AgentContext) GetConfiguration(key string) (interface{}, bool) {
	if ac.Configuration == nil {
		return nil, false
	}
	value, exists := ac.Configuration[key]
	return value, exists
}

// Methods for AgentState

// UpdateStatus updates the agent status and last activity time
func (as *AgentState) UpdateStatus(status AgentStatus) {
	as.Status = status
	as.LastActivityAt = time.Now()
}

// UpdateProgress updates the progress and last activity time
func (as *AgentState) UpdateProgress(progress float64) {
	as.Progress = progress
	as.LastActivityAt = time.Now()
}

// SetCurrentPlan sets the current execution plan
func (as *AgentState) SetCurrentPlan(plan *ExecutionPlan) {
	as.CurrentPlan = plan
	as.LastActivityAt = time.Now()
}

// SetCurrentTodo sets the current todo being executed
func (as *AgentState) SetCurrentTodo(todo *todo.Todo) {
	as.CurrentTodo = todo
	as.LastActivityAt = time.Now()
}

// IsActive returns true if the agent is currently active
func (as *AgentState) IsActive() bool {
	return as.Status == StatusAnalyzing ||
		as.Status == StatusPlanning ||
		as.Status == StatusExecuting ||
		as.Status == StatusValidating
}

// GetUptime returns how long the agent has been running
func (as *AgentState) GetUptime() time.Duration {
	return time.Since(as.StartedAt)
}

// GetIdleTime returns how long the agent has been idle
func (as *AgentState) GetIdleTime() time.Duration {
	if as.IsActive() {
		return 0
	}
	return time.Since(as.LastActivityAt)
}

// Methods for ExecutionPlan

// GetProgress calculates the progress of the execution plan
func (ep *ExecutionPlan) GetProgress() float64 {
	if ep.TodoList == nil {
		return 0.0
	}
	return ep.TodoList.GetProgress()
}

// IsCompleted returns true if the execution plan is completed
func (ep *ExecutionPlan) IsCompleted() bool {
	if ep.TodoList == nil {
		return false
	}
	return ep.TodoList.IsCompleted()
}

// GetRemainingTodos returns the number of remaining todos
func (ep *ExecutionPlan) GetRemainingTodos() int {
	if ep.TodoList == nil {
		return 0
	}
	return ep.TodoList.TotalTodos - ep.TodoList.CompletedTodos
}

// Methods for ExecutionResult

// AddChange adds a file change to the result
func (er *ExecutionResult) AddChange(change FileChange) {
	er.Changes = append(er.Changes, change)
}

// GetChangedFiles returns a list of files that were changed
func (er *ExecutionResult) GetChangedFiles() []string {
	var files []string
	for _, change := range er.Changes {
		files = append(files, change.Path)
	}
	return files
}

// HasErrors returns true if the result contains errors
func (er *ExecutionResult) HasErrors() bool {
	return !er.Success || er.Error != ""
}

// SetError sets the error message and marks the result as failed
func (er *ExecutionResult) SetError(err error) {
	er.Success = false
	if err != nil {
		er.Error = err.Error()
	}
}

// Factory functions

// NewAgentContext creates a new agent context
func NewAgentContext(sessionID, userIntent, workspaceRoot string) *AgentContext {
	return &AgentContext{
		SessionID:      sessionID,
		UserIntent:     userIntent,
		WorkspaceRoot:  workspaceRoot,
		Configuration:  make(map[string]interface{}),
		PersistentData: make(map[string]interface{}),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
}

// NewAgentState creates a new agent state
func NewAgentState(sessionID string, context *AgentContext) *AgentState {
	return &AgentState{
		ID:             generateID(),
		SessionID:      sessionID,
		Status:         StatusIdle,
		Progress:       0.0,
		StartedAt:      time.Now(),
		LastActivityAt: time.Now(),
		Context:        context,
	}
}

// NewWorkflowEvent creates a new workflow event
func NewWorkflowEvent(sessionID string, eventType EventType, message string, data map[string]interface{}) *WorkflowEvent {
	return &WorkflowEvent{
		ID:        generateID(),
		SessionID: sessionID,
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
		Message:   message,
	}
}

// generateID generates a unique ID (simplified implementation)
func generateID() string {
	return time.Now().Format("20060102150405") + "-" + randomString(8)
}

// randomString generates a random string of specified length
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}
