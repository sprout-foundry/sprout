package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/utils"
)

// ContextManager manages persistent context for complex analysis workflows
type ContextManager struct {
	config      *config.Config
	logger      *utils.Logger
	contextDir  string
	maxContexts int
}

// AnalysisFinding represents a key finding from analysis
type AnalysisFinding struct {
	ID          string            `json:"id"`
	TodoID      string            `json:"todo_id"`
	Type        string            `json:"type"`     // "file_analysis", "code_pattern", "architecture", "security", "performance"
	Severity    string            `json:"severity"` // "low", "medium", "high", "critical"
	Title       string            `json:"title"`
	Description string            `json:"description"`
	FilePath    string            `json:"file_path,omitempty"`
	LineNumber  int               `json:"line_number,omitempty"`
	CodeSnippet string            `json:"code_snippet,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Timestamp   time.Time         `json:"timestamp"`
}

// KnowledgeItem represents accumulated knowledge from analysis
type KnowledgeItem struct {
	ID           string                 `json:"id"`
	Category     string                 `json:"category"` // "architecture", "patterns", "dependencies", "issues", "insights"
	Title        string                 `json:"title"`
	Content      string                 `json:"content"`
	SourceTodos  []string               `json:"source_todos"` // Which todos contributed to this knowledge
	Confidence   float64                `json:"confidence"`   // 0.0 to 1.0
	RelatedFiles []string               `json:"related_files"`
	Tags         []string               `json:"tags"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	Timestamp    time.Time              `json:"timestamp"`
	Updated      time.Time              `json:"updated"`
}

// PersistentContext represents the persistent context for a complex analysis workflow
type PersistentContext struct {
	SessionID      string                 `json:"session_id"`
	UserIntent     string                 `json:"user_intent"`
	ProjectHash    string                 `json:"project_hash"` // Hash of workspace structure
	StartTime      time.Time              `json:"start_time"`
	LastUpdate     time.Time              `json:"last_update"`
	Status         string                 `json:"status"` // "active", "completed", "paused"
	CurrentPhase   string                 `json:"current_phase"`
	CompletedTodos []string               `json:"completed_todos"`
	Findings       []AnalysisFinding      `json:"findings"`
	KnowledgeBase  []KnowledgeItem        `json:"knowledge_base"`
	FileAnalyses   map[string]interface{} `json:"file_analyses,omitempty"` // Analysis results per file
	CodePatterns   []CodePattern          `json:"code_patterns,omitempty"`
	Dependencies   map[string][]string    `json:"dependencies,omitempty"` // File dependency graph
	Summary        *AnalysisSummary       `json:"summary,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// CodePattern represents identified code patterns
type CodePattern struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"` // "design", "anti", "security", "performance"
	Description string   `json:"description"`
	Files       []string `json:"files"`
	Examples    []string `json:"examples"`
	Severity    string   `json:"severity"`
}

// AnalysisSummary represents a comprehensive summary of all findings
type AnalysisSummary struct {
	Overview           string         `json:"overview"`
	KeyFindings        []string       `json:"key_findings"`
	Recommendations    []string       `json:"recommendations"`
	SeverityBreakdown  map[string]int `json:"severity_breakdown"`
	CategoryBreakdown  map[string]int `json:"category_breakdown"`
	FilesAnalyzed      int            `json:"files_analyzed"`
	IssuesFound        int            `json:"issues_found"`
	PatternsIdentified int            `json:"patterns_identified"`
	GeneratedAt        time.Time      `json:"generated_at"`
}

// NewContextManager creates a new context manager
func NewContextManager(cfg *config.Config, logger *utils.Logger) *ContextManager {
	contextDir := filepath.Join(".ledit", "agent_contexts")
	maxContexts := 10 // Keep last 10 contexts

	return &ContextManager{
		config:      cfg,
		logger:      logger,
		contextDir:  contextDir,
		maxContexts: maxContexts,
	}
}

// InitializeContext creates a new persistent context for a complex analysis
func (cm *ContextManager) InitializeContext(sessionID, userIntent, projectHash string) (*PersistentContext, error) {
	if err := os.MkdirAll(cm.contextDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create context directory: %w", err)
	}

	ctx := &PersistentContext{
		SessionID:      sessionID,
		UserIntent:     userIntent,
		ProjectHash:    projectHash,
		StartTime:      time.Now(),
		LastUpdate:     time.Now(),
		Status:         "active",
		CurrentPhase:   "initialization",
		CompletedTodos: []string{},
		Findings:       []AnalysisFinding{},
		KnowledgeBase:  []KnowledgeItem{},
		FileAnalyses:   make(map[string]interface{}),
		CodePatterns:   []CodePattern{},
		Dependencies:   make(map[string][]string),
		Summary:        nil,
		Metadata:       make(map[string]interface{}),
	}

	// Save the initial context
	if err := cm.saveContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to save initial context: %w", err)
	}

	cm.logger.LogProcessStep(fmt.Sprintf("📝 Initialized persistent context: %s", sessionID))
	return ctx, nil
}

// LoadContext loads an existing persistent context
func (cm *ContextManager) LoadContext(sessionID string) (*PersistentContext, error) {
	contextPath := filepath.Join(cm.contextDir, sessionID+".json")

	if _, err := os.Stat(contextPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("context not found: %s", sessionID)
	}

	data, err := os.ReadFile(contextPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read context file: %w", err)
	}

	var ctx PersistentContext
	if err := json.Unmarshal(data, &ctx); err != nil {
		return nil, fmt.Errorf("failed to parse context JSON: %w", err)
	}

	cm.logger.LogProcessStep(fmt.Sprintf("📂 Loaded persistent context: %s", sessionID))
	return &ctx, nil
}

// SaveContext saves the current context
func (cm *ContextManager) SaveContext(ctx *PersistentContext) error {
	ctx.LastUpdate = time.Now()
	return cm.saveContext(ctx)
}

// saveContext is the internal save method
func (cm *ContextManager) saveContext(ctx *PersistentContext) error {
	contextPath := filepath.Join(cm.contextDir, ctx.SessionID+".json")

	data, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal context: %w", err)
	}

	if err := os.WriteFile(contextPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write context file: %w", err)
	}

	return nil
}

// AddFinding adds a new finding to the context
func (cm *ContextManager) AddFinding(ctx *PersistentContext, finding AnalysisFinding) error {
	finding.ID = fmt.Sprintf("finding_%d", len(ctx.Findings)+1)
	finding.Timestamp = time.Now()

	ctx.Findings = append(ctx.Findings, finding)

	// Update knowledge base based on finding
	cm.updateKnowledgeFromFinding(ctx, finding)

	return cm.SaveContext(ctx)
}

// AddKnowledge adds a new knowledge item to the context
func (cm *ContextManager) AddKnowledge(ctx *PersistentContext, knowledge KnowledgeItem) error {
	knowledge.ID = fmt.Sprintf("knowledge_%d", len(ctx.KnowledgeBase)+1)
	knowledge.Timestamp = time.Now()
	knowledge.Updated = time.Now()

	ctx.KnowledgeBase = append(ctx.KnowledgeBase, knowledge)
	return cm.SaveContext(ctx)
}

// CompleteTodo marks a todo as completed and updates context
func (cm *ContextManager) CompleteTodo(ctx *PersistentContext, todoID string) error {
	// Add to completed todos if not already there
	for _, id := range ctx.CompletedTodos {
		if id == todoID {
			return nil // Already completed
		}
	}

	ctx.CompletedTodos = append(ctx.CompletedTodos, todoID)
	return cm.SaveContext(ctx)
}

// GenerateSummary generates a comprehensive summary of all findings
func (cm *ContextManager) GenerateSummary(ctx *PersistentContext) (*AnalysisSummary, error) {
	summary := &AnalysisSummary{
		KeyFindings:        []string{},
		Recommendations:    []string{},
		SeverityBreakdown:  make(map[string]int),
		CategoryBreakdown:  make(map[string]int),
		FilesAnalyzed:      len(ctx.FileAnalyses),
		IssuesFound:        len(ctx.Findings),
		PatternsIdentified: len(ctx.CodePatterns),
		GeneratedAt:        time.Now(),
	}

	// Count findings by severity and category
	for _, finding := range ctx.Findings {
		summary.SeverityBreakdown[finding.Severity]++
		summary.CategoryBreakdown[finding.Type]++
	}

	// Extract key findings (top 10 by severity)
	type findingWithPriority struct {
		finding  AnalysisFinding
		priority int
	}

	severityPriority := map[string]int{
		"critical": 4,
		"high":     3,
		"medium":   2,
		"low":      1,
	}

	var prioritizedFindings []findingWithPriority
	for _, finding := range ctx.Findings {
		priority := severityPriority[finding.Severity]
		prioritizedFindings = append(prioritizedFindings, findingWithPriority{
			finding:  finding,
			priority: priority,
		})
	}

	// Sort by priority (descending)
	sort.Slice(prioritizedFindings, func(i, j int) bool {
		return prioritizedFindings[i].priority > prioritizedFindings[j].priority
	})

	// Take top findings
	for i, pf := range prioritizedFindings {
		if i >= 10 { // Limit to top 10
			break
		}
		summary.KeyFindings = append(summary.KeyFindings,
			fmt.Sprintf("[%s] %s: %s", strings.ToUpper(pf.finding.Severity), pf.finding.Title, pf.finding.Description))
	}

	// Generate overview based on findings
	summary.Overview = cm.generateOverview(ctx)

	// Generate recommendations
	summary.Recommendations = cm.generateRecommendations(ctx)

	// Save summary to context
	ctx.Summary = summary
	return summary, cm.SaveContext(ctx)
}

// WriteSummaryToFile writes the analysis summary to a file
func (cm *ContextManager) WriteSummaryToFile(ctx *PersistentContext, outputPath string) error {
	if ctx.Summary == nil {
		_, err := cm.GenerateSummary(ctx)
		if err != nil {
			return fmt.Errorf("failed to generate summary: %w", err)
		}
	}

	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	content := cm.formatSummaryAsMarkdown(ctx)
	if err := os.WriteFile(outputPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write summary file: %w", err)
	}

	cm.logger.LogProcessStep(fmt.Sprintf("📄 Summary written to: %s", outputPath))
	return nil
}

// GetRolloverContext gets context information that should carry over to new todos
func (cm *ContextManager) GetRolloverContext(ctx *PersistentContext) map[string]interface{} {
	rollover := make(map[string]interface{})

	// Add recent findings (last 5)
	if len(ctx.Findings) > 0 {
		recentFindings := []AnalysisFinding{}
		start := len(ctx.Findings) - 5
		if start < 0 {
			start = 0
		}
		recentFindings = ctx.Findings[start:]

		rollover["recent_findings"] = recentFindings
	}

	// Add key knowledge items
	if len(ctx.KnowledgeBase) > 0 {
		rollover["key_knowledge"] = ctx.KnowledgeBase
	}

	// Add patterns identified
	if len(ctx.CodePatterns) > 0 {
		rollover["code_patterns"] = ctx.CodePatterns
	}

	// Add file dependency information
	if len(ctx.Dependencies) > 0 {
		rollover["dependencies"] = ctx.Dependencies
	}

	// Add current phase and status
	rollover["current_phase"] = ctx.CurrentPhase
	rollover["status"] = ctx.Status

	return rollover
}

// updateKnowledgeFromFinding updates the knowledge base based on a new finding
func (cm *ContextManager) updateKnowledgeFromFinding(ctx *PersistentContext, finding AnalysisFinding) {
	// Check if we already have knowledge about this
	for i, knowledge := range ctx.KnowledgeBase {
		if cm.knowledgeMatchesFinding(knowledge, finding) {
			// Update existing knowledge
			ctx.KnowledgeBase[i].Content += "\n\nAdditional finding: " + finding.Description
			ctx.KnowledgeBase[i].RelatedFiles = append(ctx.KnowledgeBase[i].RelatedFiles, finding.FilePath)
			ctx.KnowledgeBase[i].SourceTodos = append(ctx.KnowledgeBase[i].SourceTodos, finding.TodoID)
			ctx.KnowledgeBase[i].Updated = time.Now()
			return
		}
	}

	// Create new knowledge item
	knowledge := KnowledgeItem{
		Category:     cm.mapFindingTypeToCategory(finding.Type),
		Title:        finding.Title,
		Content:      finding.Description,
		SourceTodos:  []string{finding.TodoID},
		Confidence:   cm.calculateFindingConfidence(finding),
		RelatedFiles: []string{finding.FilePath},
		Tags:         finding.Tags,
		Timestamp:    time.Now(),
		Updated:      time.Now(),
	}

	ctx.KnowledgeBase = append(ctx.KnowledgeBase, knowledge)
}

// Helper methods
func (cm *ContextManager) knowledgeMatchesFinding(knowledge KnowledgeItem, finding AnalysisFinding) bool {
	// Simple matching based on title similarity
	return strings.Contains(strings.ToLower(knowledge.Title), strings.ToLower(finding.Title)) ||
		strings.Contains(strings.ToLower(finding.Title), strings.ToLower(knowledge.Title))
}

func (cm *ContextManager) mapFindingTypeToCategory(findingType string) string {
	categoryMap := map[string]string{
		"file_analysis": "insights",
		"code_pattern":  "patterns",
		"architecture":  "architecture",
		"security":      "issues",
		"performance":   "issues",
	}
	if category, exists := categoryMap[findingType]; exists {
		return category
	}
	return "insights"
}

func (cm *ContextManager) calculateFindingConfidence(finding AnalysisFinding) float64 {
	// Simple confidence calculation based on available data
	confidence := 0.5 // Base confidence

	if finding.CodeSnippet != "" {
		confidence += 0.2 // Code evidence increases confidence
	}
	if len(finding.Tags) > 0 {
		confidence += 0.1 // Tags suggest more detailed analysis
	}
	if finding.LineNumber > 0 {
		confidence += 0.1 // Specific line number increases confidence
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

func (cm *ContextManager) generateOverview(ctx *PersistentContext) string {
	if len(ctx.Findings) == 0 {
		return "No significant findings identified in the analysis."
	}

	highIssues := 0
	criticalIssues := 0
	for _, finding := range ctx.Findings {
		if finding.Severity == "high" {
			highIssues++
		} else if finding.Severity == "critical" {
			criticalIssues++
		}
	}

	overview := fmt.Sprintf("Analysis completed with %d total findings", len(ctx.Findings))
	if criticalIssues > 0 {
		overview += fmt.Sprintf(", including %d critical and %d high severity issues", criticalIssues, highIssues)
	} else if highIssues > 0 {
		overview += fmt.Sprintf(", including %d high severity issues", highIssues)
	}

	if len(ctx.CodePatterns) > 0 {
		overview += fmt.Sprintf(". %d code patterns identified", len(ctx.CodePatterns))
	}

	if len(ctx.KnowledgeBase) > 0 {
		overview += fmt.Sprintf(". %d knowledge items accumulated", len(ctx.KnowledgeBase))
	}

	return overview + "."
}

func (cm *ContextManager) generateRecommendations(ctx *PersistentContext) []string {
	recommendations := []string{}

	// Generate recommendations based on findings
	severityCount := make(map[string]int)
	for _, finding := range ctx.Findings {
		severityCount[finding.Severity]++
	}

	if severityCount["critical"] > 0 {
		recommendations = append(recommendations, "Address critical severity issues immediately")
	}

	if severityCount["high"] > 0 {
		recommendations = append(recommendations, "Review and address high severity issues")
	}

	if len(ctx.CodePatterns) > 0 {
		recommendations = append(recommendations, "Review identified code patterns for potential improvements")
	}

	if len(ctx.KnowledgeBase) == 0 {
		recommendations = append(recommendations, "Consider deeper analysis to build knowledge base")
	}

	return recommendations
}

func (cm *ContextManager) formatSummaryAsMarkdown(ctx *PersistentContext) string {
	var sb strings.Builder

	sb.WriteString("# Analysis Summary\n\n")
	sb.WriteString(fmt.Sprintf("**Session ID:** %s\n\n", ctx.SessionID))
	sb.WriteString(fmt.Sprintf("**Generated:** %s\n\n", ctx.Summary.GeneratedAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("**Status:** %s\n\n", strings.Title(ctx.Status)))

	sb.WriteString("## Overview\n\n")
	sb.WriteString(ctx.Summary.Overview + "\n\n")

	sb.WriteString("## Key Findings\n\n")
	for _, finding := range ctx.Summary.KeyFindings {
		sb.WriteString(fmt.Sprintf("- %s\n", finding))
	}

	sb.WriteString("\n## Recommendations\n\n")
	for _, rec := range ctx.Summary.Recommendations {
		sb.WriteString(fmt.Sprintf("- %s\n", rec))
	}

	sb.WriteString("\n## Statistics\n\n")
	sb.WriteString(fmt.Sprintf("- **Files Analyzed:** %d\n", ctx.Summary.FilesAnalyzed))
	sb.WriteString(fmt.Sprintf("- **Issues Found:** %d\n", ctx.Summary.IssuesFound))
	sb.WriteString(fmt.Sprintf("- **Patterns Identified:** %d\n", ctx.Summary.PatternsIdentified))

	if len(ctx.Summary.SeverityBreakdown) > 0 {
		sb.WriteString("\n### Severity Breakdown\n\n")
		for severity, count := range ctx.Summary.SeverityBreakdown {
			sb.WriteString(fmt.Sprintf("- **%s:** %d\n", strings.Title(severity), count))
		}
	}

	if len(ctx.Summary.CategoryBreakdown) > 0 {
		sb.WriteString("\n### Category Breakdown\n\n")
		for category, count := range ctx.Summary.CategoryBreakdown {
			sb.WriteString(fmt.Sprintf("- **%s:** %d\n", strings.Title(category), count))
		}
	}

	if len(ctx.Findings) > 0 {
		sb.WriteString("\n## Detailed Findings\n\n")
		for _, finding := range ctx.Findings {
			sb.WriteString(fmt.Sprintf("### %s\n\n", finding.Title))
			sb.WriteString(fmt.Sprintf("**Type:** %s | **Severity:** %s\n\n", finding.Type, finding.Severity))
			sb.WriteString(fmt.Sprintf("**Description:** %s\n\n", finding.Description))
			if finding.FilePath != "" {
				sb.WriteString(fmt.Sprintf("**File:** %s", finding.FilePath))
				if finding.LineNumber > 0 {
					sb.WriteString(fmt.Sprintf(":%d", finding.LineNumber))
				}
				sb.WriteString("\n\n")
			}
			if finding.CodeSnippet != "" {
				sb.WriteString("**Code:**\n```go\n" + finding.CodeSnippet + "\n```\n\n")
			}
		}
	}

	return sb.String()
}

// ListRecentContexts returns a list of recent context sessions
func (cm *ContextManager) ListRecentContexts() ([]*PersistentContext, error) {
	files, err := os.ReadDir(cm.contextDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*PersistentContext{}, nil
		}
		return nil, err
	}

	var contexts []*PersistentContext
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".json") {
			sessionID := strings.TrimSuffix(file.Name(), ".json")
			ctx, err := cm.LoadContext(sessionID)
			if err != nil {
				cm.logger.LogProcessStep(fmt.Sprintf("Warning: Failed to load context %s: %v", sessionID, err))
				continue
			}
			contexts = append(contexts, ctx)
		}
	}

	// Sort by last update (most recent first)
	sort.Slice(contexts, func(i, j int) bool {
		return contexts[i].LastUpdate.After(contexts[j].LastUpdate)
	})

	// Limit to max contexts
	if len(contexts) > cm.maxContexts {
		contexts = contexts[:cm.maxContexts]
	}

	return contexts, nil
}

// CleanupOldContexts removes old context files beyond the maximum limit
func (cm *ContextManager) CleanupOldContexts() error {
	contexts, err := cm.ListRecentContexts()
	if err != nil {
		return err
	}

	if len(contexts) <= cm.maxContexts {
		return nil // No cleanup needed
	}

	// Remove oldest contexts beyond the limit
	for i := cm.maxContexts; i < len(contexts); i++ {
		ctx := contexts[i]
		contextPath := filepath.Join(cm.contextDir, ctx.SessionID+".json")
		if err := os.Remove(contextPath); err != nil {
			cm.logger.LogProcessStep(fmt.Sprintf("Warning: Failed to remove old context %s: %v", ctx.SessionID, err))
		} else {
			cm.logger.LogProcessStep(fmt.Sprintf("Removed old context: %s", ctx.SessionID))
		}
	}

	return nil
}
