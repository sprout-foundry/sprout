package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/internal/domain/todo"
)

// AgentWorkflow defines the main workflow interface for agent operations
type AgentWorkflow interface {
	AnalyzeIntent(ctx context.Context, intent string, workspaceContext WorkspaceContext) (*IntentAnalysis, error)
	CreateExecutionPlan(ctx context.Context, analysis *IntentAnalysis, workspaceContext WorkspaceContext) (*ExecutionPlan, error)
	ExecutePlan(ctx context.Context, plan *ExecutionPlan) (*ExecutionResult, error)
	HandleQuestion(ctx context.Context, question string, context *AgentContext) (string, error)
}

// IntentAnalyzer analyzes user intents to determine execution strategy
type IntentAnalyzer interface {
	Analyze(ctx context.Context, intent string, workspaceContext WorkspaceContext) (*IntentAnalysis, error)
	DetermineStrategy(analysis *IntentAnalysis) ExecutionStrategy
	EstimateComplexity(intent string, workspaceContext WorkspaceContext) ComplexityLevel
}

// WorkspaceContext represents the context of the current workspace
type WorkspaceContext struct {
	RootPath     string            `json:"root_path"`
	ProjectType  string            `json:"project_type"`
	Files        []FileInfo        `json:"files"`
	Summary      string            `json:"summary"`
	Dependencies map[string]string `json:"dependencies"`
}

// FileInfo represents information about a file
type FileInfo struct {
	Path      string  `json:"path"`
	Type      string  `json:"type"`
	Language  string  `json:"language"`
	Summary   string  `json:"summary"`
	Relevance float64 `json:"relevance"`
}

// LLMProvider defines the interface for LLM operations
type LLMProvider interface {
	GenerateResponse(ctx context.Context, prompt string, options map[string]interface{}) (string, error)
	AnalyzeIntent(ctx context.Context, intent string, context WorkspaceContext) (map[string]interface{}, error)
}

// WorkspaceProvider defines the interface for workspace operations
type WorkspaceProvider interface {
	GetContext(ctx context.Context, intent string) (WorkspaceContext, error)
	GetRelevantFiles(ctx context.Context, intent string, maxFiles int) ([]FileInfo, error)
	AnalyzeStructure(ctx context.Context) (WorkspaceContext, error)
}

// EventBus defines the interface for publishing workflow events
type EventBus interface {
	Publish(ctx context.Context, event *WorkflowEvent) error
	Subscribe(ctx context.Context, eventType EventType, handler func(*WorkflowEvent)) error
}

// workflowImpl implements the AgentWorkflow interface
type workflowImpl struct {
	llmProvider       LLMProvider
	workspaceProvider WorkspaceProvider
	todoService       todo.TodoService
	eventBus          EventBus
}

// NewAgentWorkflow creates a new agent workflow implementation
func NewAgentWorkflow(
	llmProvider LLMProvider,
	workspaceProvider WorkspaceProvider,
	todoService todo.TodoService,
	eventBus EventBus,
) AgentWorkflow {
	return &workflowImpl{
		llmProvider:       llmProvider,
		workspaceProvider: workspaceProvider,
		todoService:       todoService,
		eventBus:          eventBus,
	}
}

// AnalyzeIntent analyzes the user intent to determine how to proceed
func (w *workflowImpl) AnalyzeIntent(ctx context.Context, intent string, workspaceContext WorkspaceContext) (*IntentAnalysis, error) {
	// Publish event
	event := NewWorkflowEvent("", EventTypeAnalysisStarted, "Starting intent analysis", map[string]interface{}{
		"intent": intent,
	})
	w.eventBus.Publish(ctx, event)

	// Create analysis prompt
	prompt := w.buildIntentAnalysisPrompt(intent, workspaceContext)

	// Call LLM for analysis
	response, err := w.llmProvider.GenerateResponse(ctx, prompt, map[string]interface{}{
		"temperature": 0.2,
		"max_tokens":  1500,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to analyze intent with LLM: %w", err)
	}

	// Parse the response into IntentAnalysis
	analysis, err := w.parseIntentAnalysis(response, intent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse intent analysis: %w", err)
	}

	// Determine execution strategy
	analysis.Strategy = w.determineExecutionStrategy(analysis, workspaceContext)

	// Publish completion event
	event = NewWorkflowEvent("", EventTypeAnalysisComplete, "Intent analysis completed", map[string]interface{}{
		"analysis": analysis,
	})
	w.eventBus.Publish(ctx, event)

	return analysis, nil
}

// CreateExecutionPlan creates a detailed execution plan based on the analysis
func (w *workflowImpl) CreateExecutionPlan(ctx context.Context, analysis *IntentAnalysis, workspaceContext WorkspaceContext) (*ExecutionPlan, error) {
	// Create todo list from the intent
	todoList, err := w.todoService.CreateFromIntent(ctx, analysis.Description, todo.WorkspaceContext{
		RootPath:     workspaceContext.RootPath,
		ProjectType:  workspaceContext.ProjectType,
		Files:        w.convertFileInfos(workspaceContext.Files),
		Summary:      workspaceContext.Summary,
		Dependencies: workspaceContext.Dependencies,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create todo list: %w", err)
	}

	// Create execution plan
	plan := &ExecutionPlan{
		ID:            generateID(),
		IntentID:      analysis.Description, // Use description as intent ID for now
		Analysis:      *analysis,
		TodoList:      todoList,
		Strategy:      analysis.Strategy,
		CreatedAt:     time.Now(),
		EstimatedTime: analysis.EstimatedTime,
	}

	// Publish event
	event := NewWorkflowEvent("", EventTypePlanCreated, "Execution plan created", map[string]interface{}{
		"plan_id":    plan.ID,
		"todo_count": len(todoList.Todos),
		"strategy":   string(analysis.Strategy),
	})
	w.eventBus.Publish(ctx, event)

	return plan, nil
}

// ExecutePlan executes the given execution plan
func (w *workflowImpl) ExecutePlan(ctx context.Context, plan *ExecutionPlan) (*ExecutionResult, error) {
	startTime := time.Now()

	result := &ExecutionResult{
		ID:       generateID(),
		PlanID:   plan.ID,
		Success:  true,
		Metadata: make(map[string]interface{}),
	}

	// Execute todos one by one
	for {
		// Get next todo
		nextTodo := plan.TodoList.GetNextTodo()
		if nextTodo == nil {
			break // No more todos to execute
		}

		// Publish todo started event
		event := NewWorkflowEvent("", EventTypeTodoStarted, "Starting todo execution", map[string]interface{}{
			"todo_id":     nextTodo.ID,
			"description": nextTodo.Description,
		})
		w.eventBus.Publish(ctx, event)

		// Create a basic executor (this would be injected in a real implementation)
		executor := &basicTodoExecutor{
			llmProvider: w.llmProvider,
		}

		// Execute the todo
		todoResult, err := w.todoService.ExecuteTodo(ctx, nextTodo, executor)
		if err != nil {
			result.Success = false
			result.SetError(fmt.Errorf("failed to execute todo %s: %w", nextTodo.ID, err))

			event = NewWorkflowEvent("", EventTypeTodoFailed, "Todo execution failed", map[string]interface{}{
				"todo_id": nextTodo.ID,
				"error":   err.Error(),
			})
			w.eventBus.Publish(ctx, event)
			break
		}

		// Add any file changes to the result
		if todoResult.Changes != nil {
			for _, change := range todoResult.Changes {
				result.AddChange(FileChange{
					Path: change,
					Type: ChangeTypeModify,
				})
			}
		}

		// Publish todo completed event
		event = NewWorkflowEvent("", EventTypeTodoCompleted, "Todo completed successfully", map[string]interface{}{
			"todo_id": nextTodo.ID,
			"success": todoResult.Success,
		})
		w.eventBus.Publish(ctx, event)

		// Update progress
		plan.TodoList.UpdateProgress()
	}

	// Set final result data
	result.CompletedAt = time.Now()
	result.Duration = time.Since(startTime)

	// Publish workflow completion event
	eventType := EventTypeWorkflowComplete
	message := "Workflow completed successfully"
	if !result.Success {
		eventType = EventTypeWorkflowFailed
		message = "Workflow failed"
	}

	event := NewWorkflowEvent("", eventType, message, map[string]interface{}{
		"result_id": result.ID,
		"success":   result.Success,
		"duration":  result.Duration.String(),
	})
	w.eventBus.Publish(ctx, event)

	return result, nil
}

// HandleQuestion handles user questions during workflow execution
func (w *workflowImpl) HandleQuestion(ctx context.Context, question string, agentContext *AgentContext) (string, error) {
	// Create prompt for question handling
	prompt := fmt.Sprintf(`You are an AI assistant helping with software development. Answer this question clearly and concisely:

Question: %s

Context:
- User Intent: %s
- Workspace: %s

Provide a helpful answer that guides the user forward.`, question, agentContext.UserIntent, agentContext.WorkspaceRoot)

	// Call LLM for response
	response, err := w.llmProvider.GenerateResponse(ctx, prompt, map[string]interface{}{
		"temperature": 0.3,
		"max_tokens":  1000,
	})
	if err != nil {
		return "", fmt.Errorf("failed to get LLM response for question: %w", err)
	}

	// Publish event
	event := NewWorkflowEvent("", EventTypeUserQuestion, "Answered user question", map[string]interface{}{
		"question": question,
		"answer":   response,
	})
	w.eventBus.Publish(ctx, event)

	return response, nil
}

// Private helper methods

// buildIntentAnalysisPrompt creates the prompt for intent analysis
func (w *workflowImpl) buildIntentAnalysisPrompt(intent string, workspaceContext WorkspaceContext) string {
	var promptBuilder strings.Builder

	promptBuilder.WriteString(`Analyze this user intent for software development task planning:

User Intent: "` + intent + `"

Workspace Context:
- Project Type: ` + workspaceContext.ProjectType + `
- Root Path: ` + workspaceContext.RootPath + `
`)

	if len(workspaceContext.Files) > 0 {
		promptBuilder.WriteString("\nRelevant Files:\n")
		for _, file := range workspaceContext.Files {
			promptBuilder.WriteString(fmt.Sprintf("- %s (%s)\n", file.Path, file.Type))
		}
	}

	promptBuilder.WriteString(`
Based on this intent and context, provide analysis in JSON format:

{
  "type": "code_generation|analysis|refactoring|documentation|question|multi_step|orchestration",
  "complexity": 1-4,
  "target_files": ["file1.go", "file2.go"],
  "required_context": ["context1", "context2"],
  "estimated_time_minutes": 30,
  "confidence": 0.85,
  "keywords": ["keyword1", "keyword2"],
  "description": "Clear description of what needs to be done"
}

Guidelines:
- complexity: 1=simple, 2=moderate, 3=complex, 4=critical
- confidence: 0.0 to 1.0 (how confident you are in the analysis)
- estimated_time_minutes: realistic estimate in minutes
- description: should be clear and actionable
`)

	return promptBuilder.String()
}

// parseIntentAnalysis parses the LLM response into IntentAnalysis
func (w *workflowImpl) parseIntentAnalysis(response, originalIntent string) (*IntentAnalysis, error) {
	// For now, create a basic analysis - in a real implementation this would parse JSON
	return &IntentAnalysis{
		Type:        w.determineIntentType(originalIntent),
		Complexity:  w.estimateComplexity(originalIntent),
		Strategy:    StrategyQuickEdit, // Will be determined later
		Confidence:  0.8,
		Description: originalIntent,
		Keywords:    w.extractKeywords(originalIntent),
	}, nil
}

// determineIntentType determines the type of intent
func (w *workflowImpl) determineIntentType(intent string) IntentType {
	intent = strings.ToLower(intent)

	if strings.Contains(intent, "add") || strings.Contains(intent, "create") || strings.Contains(intent, "implement") {
		return IntentTypeCodeGeneration
	}
	if strings.Contains(intent, "analyze") || strings.Contains(intent, "understand") || strings.Contains(intent, "explain") {
		return IntentTypeAnalysis
	}
	if strings.Contains(intent, "refactor") || strings.Contains(intent, "restructure") || strings.Contains(intent, "clean up") {
		return IntentTypeRefactoring
	}
	if strings.Contains(intent, "document") || strings.Contains(intent, "comment") || strings.Contains(intent, "readme") {
		return IntentTypeDocumentation
	}
	if strings.Contains(intent, "?") || strings.Contains(intent, "how") || strings.Contains(intent, "what") || strings.Contains(intent, "why") {
		return IntentTypeQuestion
	}

	return IntentTypeCodeGeneration // Default
}

// estimateComplexity estimates the complexity of the intent
func (w *workflowImpl) estimateComplexity(intent string) ComplexityLevel {
	intent = strings.ToLower(intent)

	complexWords := []string{"refactor", "restructure", "architecture", "framework", "migrate", "convert"}
	simpleWords := []string{"add", "create", "fix", "update", "comment"}

	for _, word := range complexWords {
		if strings.Contains(intent, word) {
			return ComplexityComplex
		}
	}

	for _, word := range simpleWords {
		if strings.Contains(intent, word) {
			return ComplexitySimple
		}
	}

	return ComplexityModerate // Default
}

// extractKeywords extracts keywords from the intent
func (w *workflowImpl) extractKeywords(intent string) []string {
	// Simple keyword extraction - in practice this would be more sophisticated
	words := strings.Fields(strings.ToLower(intent))
	keywords := make([]string, 0)

	// Filter out common words and keep meaningful ones
	stopWords := map[string]bool{
		"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
		"be": true, "by": true, "for": true, "from": true, "has": true, "he": true,
		"in": true, "is": true, "it": true, "its": true, "of": true, "on": true,
		"that": true, "the": true, "to": true, "was": true, "will": true, "with": true,
	}

	for _, word := range words {
		if len(word) > 2 && !stopWords[word] {
			keywords = append(keywords, word)
		}
	}

	return keywords
}

// determineExecutionStrategy determines the best execution strategy
func (w *workflowImpl) determineExecutionStrategy(analysis *IntentAnalysis, workspaceContext WorkspaceContext) ExecutionStrategy {
	// Strategy selection logic
	if analysis.Type == IntentTypeQuestion {
		return StrategyAnalyze
	}

	if analysis.Complexity >= ComplexityComplex || len(analysis.TargetFiles) > 3 {
		return StrategyFullEdit
	}

	if analysis.Type == IntentTypeAnalysis {
		return StrategyAnalyze
	}

	return StrategyQuickEdit // Default for simple code changes
}

// convertFileInfos converts agent FileInfo to todo FileInfo
func (w *workflowImpl) convertFileInfos(files []FileInfo) []todo.FileInfo {
	todoFiles := make([]todo.FileInfo, len(files))
	for i, file := range files {
		todoFiles[i] = todo.FileInfo{
			Path:      file.Path,
			Type:      file.Type,
			Language:  file.Language,
			Summary:   file.Summary,
			Relevance: file.Relevance,
		}
	}
	return todoFiles
}

// basicTodoExecutor is a simple implementation of TodoExecutor for testing
type basicTodoExecutor struct {
	llmProvider LLMProvider
}

// Execute executes a todo (simplified implementation)
func (e *basicTodoExecutor) Execute(ctx context.Context, t *todo.Todo) (*todo.ExecutionResult, error) {
	// Simulate execution
	time.Sleep(100 * time.Millisecond)

	return &todo.ExecutionResult{
		Success:  true,
		Output:   fmt.Sprintf("Executed todo: %s", t.Description),
		Duration: 100 * time.Millisecond,
		Changes:  t.TargetFiles,
		Metadata: map[string]interface{}{
			"todo_type": string(t.Type),
		},
	}, nil
}

// CanExecute returns true if this executor can handle the given todo type
func (e *basicTodoExecutor) CanExecute(todoType todo.TodoType) bool {
	// For simplicity, this basic executor can handle all types
	return true
}

// GetRequiredTools returns the tools required for executing the given todo type
func (e *basicTodoExecutor) GetRequiredTools(todoType todo.TodoType) []string {
	switch todoType {
	case todo.TodoTypeCodeChange:
		return []string{"editor", "compiler"}
	case todo.TodoTypeValidation:
		return []string{"validator", "linter"}
	case todo.TodoTypeTest:
		return []string{"test_runner"}
	default:
		return []string{}
	}
}
