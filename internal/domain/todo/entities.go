package todo

import (
	"encoding/json"
	"fmt"
	"time"
)

// TodoType represents the type of work a todo involves
type TodoType string

const (
	TodoTypeAnalysis      TodoType = "analysis"
	TodoTypeCodeChange    TodoType = "code_change"
	TodoTypeValidation    TodoType = "validation"
	TodoTypeDocumentation TodoType = "documentation"
	TodoTypeTest          TodoType = "test"
	TodoTypeConfiguration TodoType = "configuration"
)

// Status represents the current state of a todo
type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusSkipped    Status = "skipped"
)

// Priority levels for todo execution
type Priority int

const (
	PriorityLow      Priority = 10
	PriorityNormal   Priority = 50
	PriorityHigh     Priority = 80
	PriorityCritical Priority = 100
)

// ComplexityLevel represents the estimated complexity of a todo
type ComplexityLevel int

const (
	ComplexitySimple   ComplexityLevel = 1
	ComplexityModerate ComplexityLevel = 2
	ComplexityComplex  ComplexityLevel = 3
	ComplexityCritical ComplexityLevel = 4
)

// Todo represents a single actionable task within an agent workflow
type Todo struct {
	ID           string                 `json:"id"`
	Description  string                 `json:"description"`
	ActiveForm   string                 `json:"active_form"`
	Type         TodoType               `json:"type"`
	Status       Status                 `json:"status"`
	Priority     Priority               `json:"priority"`
	Complexity   ComplexityLevel        `json:"complexity"`
	Dependencies []string               `json:"dependencies"`
	Context      map[string]interface{} `json:"context"`
	CreatedAt    time.Time              `json:"created_at"`
	StartedAt    *time.Time             `json:"started_at,omitempty"`
	CompletedAt  *time.Time             `json:"completed_at,omitempty"`
	Error        string                 `json:"error,omitempty"`

	// Execution context
	TargetFiles      []string      `json:"target_files,omitempty"`
	RequiredTools    []string      `json:"required_tools,omitempty"`
	ExpectedDuration time.Duration `json:"expected_duration,omitempty"`
}

// TodoList represents a collection of todos with metadata
type TodoList struct {
	ID         string    `json:"id"`
	UserIntent string    `json:"user_intent"`
	Todos      []Todo    `json:"todos"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`

	// Execution metadata
	TotalTodos     int `json:"total_todos"`
	CompletedTodos int `json:"completed_todos"`
	FailedTodos    int `json:"failed_todos"`
}

// ExecutionResult represents the result of executing a todo
type ExecutionResult struct {
	Success  bool                   `json:"success"`
	Output   string                 `json:"output,omitempty"`
	Error    error                  `json:"error,omitempty"`
	Duration time.Duration          `json:"duration"`
	Changes  []string               `json:"changes,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// WorkspaceContext represents the context needed for todo creation and execution
type WorkspaceContext struct {
	RootPath     string            `json:"root_path"`
	ProjectType  string            `json:"project_type"`
	Files        []FileInfo        `json:"files"`
	Summary      string            `json:"summary"`
	Dependencies map[string]string `json:"dependencies"`
}

// FileInfo represents information about a file in the workspace
type FileInfo struct {
	Path      string  `json:"path"`
	Type      string  `json:"type"`
	Language  string  `json:"language"`
	Summary   string  `json:"summary,omitempty"`
	Relevance float64 `json:"relevance"`
}

// Methods for Todo entity

// IsBlocked returns true if the todo is blocked by incomplete dependencies
func (t *Todo) IsBlocked(completedTodos map[string]bool) bool {
	for _, depID := range t.Dependencies {
		if !completedTodos[depID] {
			return true
		}
	}
	return false
}

// CanExecute returns true if the todo can be executed now
func (t *Todo) CanExecute(completedTodos map[string]bool) bool {
	return t.Status == StatusPending && !t.IsBlocked(completedTodos)
}

// MarkStarted marks the todo as started
func (t *Todo) MarkStarted() {
	t.Status = StatusInProgress
	now := time.Now()
	t.StartedAt = &now
}

// MarkCompleted marks the todo as completed
func (t *Todo) MarkCompleted() {
	t.Status = StatusCompleted
	now := time.Now()
	t.CompletedAt = &now
}

// MarkFailed marks the todo as failed with an error message
func (t *Todo) MarkFailed(err error) {
	t.Status = StatusFailed
	now := time.Now()
	t.CompletedAt = &now
	if err != nil {
		t.Error = err.Error()
	}
}

// GetDuration returns the duration the todo took to complete
func (t *Todo) GetDuration() time.Duration {
	if t.StartedAt == nil {
		return 0
	}

	endTime := time.Now()
	if t.CompletedAt != nil {
		endTime = *t.CompletedAt
	}

	return endTime.Sub(*t.StartedAt)
}

// Methods for TodoList

// AddTodo adds a todo to the list
func (tl *TodoList) AddTodo(todo Todo) {
	tl.Todos = append(tl.Todos, todo)
	tl.TotalTodos = len(tl.Todos)
	tl.UpdatedAt = time.Now()
}

// GetPendingTodos returns all todos that are pending and not blocked
func (tl *TodoList) GetPendingTodos() []Todo {
	completedTodos := make(map[string]bool)
	for _, todo := range tl.Todos {
		if todo.Status == StatusCompleted {
			completedTodos[todo.ID] = true
		}
	}

	var pending []Todo
	for _, todo := range tl.Todos {
		if todo.CanExecute(completedTodos) {
			pending = append(pending, todo)
		}
	}

	return pending
}

// GetNextTodo returns the highest priority todo that can be executed
func (tl *TodoList) GetNextTodo() *Todo {
	pending := tl.GetPendingTodos()
	if len(pending) == 0 {
		return nil
	}

	// Sort by priority (highest first), then by creation time
	var best *Todo
	for i := range pending {
		todo := &pending[i]
		if best == nil || todo.Priority > best.Priority ||
			(todo.Priority == best.Priority && todo.CreatedAt.Before(best.CreatedAt)) {
			best = todo
		}
	}

	return best
}

// UpdateProgress updates the progress counters
func (tl *TodoList) UpdateProgress() {
	completed := 0
	failed := 0

	for _, todo := range tl.Todos {
		switch todo.Status {
		case StatusCompleted:
			completed++
		case StatusFailed:
			failed++
		}
	}

	tl.CompletedTodos = completed
	tl.FailedTodos = failed
	tl.UpdatedAt = time.Now()
}

// IsCompleted returns true if all todos are completed or failed
func (tl *TodoList) IsCompleted() bool {
	for _, todo := range tl.Todos {
		if todo.Status == StatusPending || todo.Status == StatusInProgress {
			return false
		}
	}
	return true
}

// GetProgress returns the completion percentage (0.0 to 1.0)
func (tl *TodoList) GetProgress() float64 {
	if tl.TotalTodos == 0 {
		return 1.0
	}

	return float64(tl.CompletedTodos) / float64(tl.TotalTodos)
}

// JSON marshaling helpers

// MarshalJSON implements custom JSON marshaling for ExecutionResult
func (er ExecutionResult) MarshalJSON() ([]byte, error) {
	type Alias ExecutionResult

	// Convert error to string for JSON serialization
	result := struct {
		Alias
		ErrorStr string `json:"error,omitempty"`
	}{
		Alias: (Alias)(er),
	}

	if er.Error != nil {
		result.ErrorStr = er.Error.Error()
	}

	return json.Marshal(result)
}

// UnmarshalJSON implements custom JSON unmarshaling for ExecutionResult
func (er *ExecutionResult) UnmarshalJSON(data []byte) error {
	type Alias ExecutionResult

	aux := struct {
		*Alias
		ErrorStr string `json:"error,omitempty"`
	}{
		Alias: (*Alias)(er),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if aux.ErrorStr != "" {
		er.Error = fmt.Errorf("%s", aux.ErrorStr)
	}

	return nil
}
