package todo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TodoService defines the interface for todo management
type TodoService interface {
	CreateFromIntent(ctx context.Context, intent string, workspace WorkspaceContext) (*TodoList, error)
	PrioritizeTodos(ctx context.Context, todos []Todo) ([]Todo, error)
	SelectNextTodo(ctx context.Context, todoList *TodoList) (*Todo, error)
	ExecuteTodo(ctx context.Context, todo *Todo, executor TodoExecutor) (*ExecutionResult, error)
	UpdateTodoStatus(ctx context.Context, todoID string, status Status) error
}

// TodoExecutor defines how todos are executed
type TodoExecutor interface {
	Execute(ctx context.Context, todo *Todo) (*ExecutionResult, error)
	CanExecute(todoType TodoType) bool
	GetRequiredTools(todoType TodoType) []string
}

// LLMProvider defines the interface for LLM operations needed by todo service
type LLMProvider interface {
	GenerateResponse(ctx context.Context, prompt string, options map[string]interface{}) (string, error)
}

// todoServiceImpl implements the TodoService interface
type todoServiceImpl struct {
	llmProvider LLMProvider
}

// NewTodoService creates a new todo service
func NewTodoService(llmProvider LLMProvider) TodoService {
	return &todoServiceImpl{
		llmProvider: llmProvider,
	}
}

// CreateFromIntent creates a todo list from user intent and workspace context
func (s *todoServiceImpl) CreateFromIntent(ctx context.Context, intent string, workspace WorkspaceContext) (*TodoList, error) {
	// Generate unique ID for the todo list
	id, err := generateID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate todo list ID: %w", err)
	}

	// Build context-aware prompt for LLM
	prompt := s.buildTodoCreationPrompt(intent, workspace)

	// Call LLM to generate todos
	response, err := s.llmProvider.GenerateResponse(ctx, prompt, map[string]interface{}{
		"temperature": 0.3, // Lower temperature for more consistent todo generation
		"max_tokens":  2000,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate todos from LLM: %w", err)
	}

	// Parse the LLM response into todos
	todos, err := s.parseTodosFromResponse(response, workspace)
	if err != nil {
		return nil, fmt.Errorf("failed to parse todos from LLM response: %w", err)
	}

	// Create todo list
	todoList := &TodoList{
		ID:         id,
		UserIntent: intent,
		Todos:      todos,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		TotalTodos: len(todos),
	}

	// Set initial priorities and dependencies
	s.setInitialPriorities(todoList)
	s.analyzeDependencies(todoList)

	return todoList, nil
}

// PrioritizeTodos reorders todos based on priority, dependencies, and complexity
func (s *todoServiceImpl) PrioritizeTodos(ctx context.Context, todos []Todo) ([]Todo, error) {
	if len(todos) <= 1 {
		return todos, nil
	}

	// Create a copy to avoid modifying the original
	prioritized := make([]Todo, len(todos))
	copy(prioritized, todos)

	// Sort todos using multiple criteria
	sort.Slice(prioritized, func(i, j int) bool {
		todoA, todoB := prioritized[i], prioritized[j]

		// First, prioritize by dependencies (todos with no dependencies first)
		depsA := len(todoA.Dependencies)
		depsB := len(todoB.Dependencies)
		if depsA != depsB {
			return depsA < depsB
		}

		// Then by priority (higher priority first)
		if todoA.Priority != todoB.Priority {
			return todoA.Priority > todoB.Priority
		}

		// Then by complexity (simpler tasks first for quick wins)
		if todoA.Complexity != todoB.Complexity {
			return todoA.Complexity < todoB.Complexity
		}

		// Finally by creation time (older tasks first)
		return todoA.CreatedAt.Before(todoB.CreatedAt)
	})

	return prioritized, nil
}

// SelectNextTodo selects the next todo to execute based on availability and priority
func (s *todoServiceImpl) SelectNextTodo(ctx context.Context, todoList *TodoList) (*Todo, error) {
	if todoList == nil {
		return nil, fmt.Errorf("todo list is nil")
	}

	// Get all executable todos
	pending := todoList.GetPendingTodos()
	if len(pending) == 0 {
		return nil, nil // No todos available
	}

	// Prioritize the pending todos
	prioritized, err := s.PrioritizeTodos(ctx, pending)
	if err != nil {
		return nil, fmt.Errorf("failed to prioritize todos: %w", err)
	}

	// Return the highest priority executable todo
	if len(prioritized) > 0 {
		// Find the todo in the original list and return a pointer to it
		for i := range todoList.Todos {
			if todoList.Todos[i].ID == prioritized[0].ID {
				return &todoList.Todos[i], nil
			}
		}
	}

	return nil, fmt.Errorf("selected todo not found in original list")
}

// ExecuteTodo executes a todo using the provided executor
func (s *todoServiceImpl) ExecuteTodo(ctx context.Context, todo *Todo, executor TodoExecutor) (*ExecutionResult, error) {
	if todo == nil {
		return nil, fmt.Errorf("todo is nil")
	}

	if executor == nil {
		return nil, fmt.Errorf("executor is nil")
	}

	// Check if executor can handle this type of todo
	if !executor.CanExecute(todo.Type) {
		return nil, fmt.Errorf("executor cannot handle todo type: %s", todo.Type)
	}

	// Mark todo as started
	todo.MarkStarted()

	// Execute the todo
	result, err := executor.Execute(ctx, todo)
	if err != nil {
		todo.MarkFailed(err)
		return nil, fmt.Errorf("failed to execute todo %s: %w", todo.ID, err)
	}

	// Update todo status based on result
	if result.Success {
		todo.MarkCompleted()
	} else {
		todo.MarkFailed(result.Error)
	}

	return result, nil
}

// UpdateTodoStatus updates the status of a todo
func (s *todoServiceImpl) UpdateTodoStatus(ctx context.Context, todoID string, status Status) error {
	// This would typically update persistent storage
	// For now, it's a placeholder for the interface
	return nil
}

// Private helper methods

// generateID generates a unique ID for todos and todo lists
func generateID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// buildTodoCreationPrompt creates the prompt for LLM todo generation
func (s *todoServiceImpl) buildTodoCreationPrompt(intent string, workspace WorkspaceContext) string {
	var promptBuilder strings.Builder

	promptBuilder.WriteString(`You are an expert software developer. Break down this user request into specific, actionable todos grounded in the provided workspace context.

User Request: "` + intent + `"

## Workspace Context
`)

	// Add workspace information
	promptBuilder.WriteString(fmt.Sprintf("Project Type: %s\n", workspace.ProjectType))
	promptBuilder.WriteString(fmt.Sprintf("Root Path: %s\n", workspace.RootPath))

	if workspace.Summary != "" {
		promptBuilder.WriteString(fmt.Sprintf("Summary: %s\n", workspace.Summary))
	}

	// Add relevant files
	if len(workspace.Files) > 0 {
		promptBuilder.WriteString("\nRelevant Files:\n")
		for _, file := range workspace.Files {
			promptBuilder.WriteString(fmt.Sprintf("- %s (%s", file.Path, file.Type))
			if file.Summary != "" {
				promptBuilder.WriteString(fmt.Sprintf(": %s", file.Summary))
			}
			promptBuilder.WriteString(")\n")
		}
	}

	promptBuilder.WriteString(`
Please respond with a JSON array of todos. Each todo should have:
- "description": A clear, actionable description
- "active_form": Present continuous form (e.g., "Creating function" vs "Create function")
- "type": One of "analysis", "code_change", "validation", "documentation", "test", "configuration"
- "priority": Number from 10 (low) to 100 (critical)
- "complexity": Number from 1 (simple) to 4 (critical)
- "target_files": Array of file paths this todo will modify (if applicable)
- "dependencies": Array of other todo descriptions this depends on (if applicable)

Example response:
[
  {
    "description": "Analyze existing code structure to understand the current implementation",
    "active_form": "Analyzing existing code structure",
    "type": "analysis",
    "priority": 80,
    "complexity": 2,
    "target_files": [],
    "dependencies": []
  },
  {
    "description": "Add new function to handle user authentication",
    "active_form": "Adding new function to handle user authentication",
    "type": "code_change", 
    "priority": 70,
    "complexity": 3,
    "target_files": ["auth.go"],
    "dependencies": ["Analyze existing code structure to understand the current implementation"]
  }
]
`)

	return promptBuilder.String()
}

// parseTodosFromResponse parses the LLM response into Todo objects
func (s *todoServiceImpl) parseTodosFromResponse(response string, workspace WorkspaceContext) ([]Todo, error) {
	// Extract JSON from the response
	jsonStr, err := s.extractJSON(response)
	if err != nil {
		return nil, fmt.Errorf("failed to extract JSON from response: %w", err)
	}

	// Parse the JSON into a slice of raw todo data
	var rawTodos []map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &rawTodos); err != nil {
		return nil, fmt.Errorf("failed to unmarshal todos JSON: %w", err)
	}

	// Convert raw data to Todo objects
	var todos []Todo
	for i, raw := range rawTodos {
		todo, err := s.convertRawTodo(raw, i)
		if err != nil {
			return nil, fmt.Errorf("failed to convert raw todo %d: %w", i, err)
		}
		todos = append(todos, todo)
	}

	return todos, nil
}

// extractJSON extracts JSON content from a response that may contain other text
func (s *todoServiceImpl) extractJSON(response string) (string, error) {
	// Look for JSON array in the response
	re := regexp.MustCompile(`\[[\s\S]*\]`)
	matches := re.FindAllString(response, -1)

	if len(matches) == 0 {
		return "", fmt.Errorf("no JSON array found in response")
	}

	// Return the last (likely most complete) match
	return matches[len(matches)-1], nil
}

// convertRawTodo converts a raw map to a Todo object
func (s *todoServiceImpl) convertRawTodo(raw map[string]interface{}, index int) (Todo, error) {
	// Generate unique ID
	id, err := generateID()
	if err != nil {
		return Todo{}, fmt.Errorf("failed to generate todo ID: %w", err)
	}

	// Extract required fields
	description, ok := raw["description"].(string)
	if !ok || description == "" {
		return Todo{}, fmt.Errorf("missing or invalid description")
	}

	activeForm, ok := raw["active_form"].(string)
	if !ok || activeForm == "" {
		activeForm = description // Fallback to description
	}

	todoTypeStr, ok := raw["type"].(string)
	if !ok {
		return Todo{}, fmt.Errorf("missing or invalid type")
	}

	// Convert priority
	var priority Priority = PriorityNormal
	if p, ok := raw["priority"].(float64); ok {
		priority = Priority(p)
	} else if p, ok := raw["priority"].(string); ok {
		if pInt, err := strconv.Atoi(p); err == nil {
			priority = Priority(pInt)
		}
	}

	// Convert complexity
	var complexity ComplexityLevel = ComplexityModerate
	if c, ok := raw["complexity"].(float64); ok {
		complexity = ComplexityLevel(c)
	} else if c, ok := raw["complexity"].(string); ok {
		if cInt, err := strconv.Atoi(c); err == nil {
			complexity = ComplexityLevel(cInt)
		}
	}

	// Extract target files
	var targetFiles []string
	if files, ok := raw["target_files"].([]interface{}); ok {
		for _, f := range files {
			if file, ok := f.(string); ok {
				targetFiles = append(targetFiles, file)
			}
		}
	}

	// Extract dependencies (will be resolved later)
	var dependencies []string
	if deps, ok := raw["dependencies"].([]interface{}); ok {
		for _, d := range deps {
			if dep, ok := d.(string); ok {
				dependencies = append(dependencies, dep)
			}
		}
	}

	return Todo{
		ID:           id,
		Description:  description,
		ActiveForm:   activeForm,
		Type:         TodoType(todoTypeStr),
		Status:       StatusPending,
		Priority:     priority,
		Complexity:   complexity,
		Dependencies: dependencies, // These are still description strings, need to be resolved to IDs
		TargetFiles:  targetFiles,
		Context:      make(map[string]interface{}),
		CreatedAt:    time.Now(),
	}, nil
}

// setInitialPriorities adjusts priorities based on heuristics
func (s *todoServiceImpl) setInitialPriorities(todoList *TodoList) {
	for i := range todoList.Todos {
		todo := &todoList.Todos[i]

		// Boost priority for analysis tasks (usually need to be done first)
		if todo.Type == TodoTypeAnalysis {
			todo.Priority = Priority(int(todo.Priority) + 10)
		}

		// Boost priority for validation tasks if they're simple
		if todo.Type == TodoTypeValidation && todo.Complexity == ComplexitySimple {
			todo.Priority = Priority(int(todo.Priority) + 5)
		}

		// Lower priority for documentation tasks (can be done later)
		if todo.Type == TodoTypeDocumentation {
			todo.Priority = Priority(int(todo.Priority) - 10)
		}
	}
}

// analyzeDependencies converts dependency descriptions to todo IDs
func (s *todoServiceImpl) analyzeDependencies(todoList *TodoList) {
	// Create a map of descriptions to IDs
	descToID := make(map[string]string)
	for _, todo := range todoList.Todos {
		descToID[todo.Description] = todo.ID
	}

	// Resolve dependencies
	for i := range todoList.Todos {
		todo := &todoList.Todos[i]
		var resolvedDeps []string

		for _, depDesc := range todo.Dependencies {
			if depID, found := descToID[depDesc]; found {
				resolvedDeps = append(resolvedDeps, depID)
			}
		}

		todo.Dependencies = resolvedDeps
	}
}
