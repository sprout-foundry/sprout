# Agent Workflow Implementation Playbook

## Overview

This playbook provides actionable guidance for modifying, debugging, and optimizing the ledit agent workflow. It serves as a practical reference for developers implementing changes to agent behavior, prompts, and execution logic.

## Table of Contents

- [Core Architecture](#core-architecture)
- [Configuration Points](#configuration-points)
- [Workflow Modification Guide](#workflow-modification-guide)
- [Prompt Engineering](#prompt-engineering)
- [Playbook Development](#playbook-development)
- [Debugging and Monitoring](#debugging-and-monitoring)
- [Performance Optimization](#performance-optimization)
- [Troubleshooting](#troubleshooting)

## Core Architecture

### Agent Entry Points

**Primary Entry Point:**
- File: `cmd/agent.go`
- Function: `runAgentMode()`
- Logic: Routes to either Agent v2 (fast-path) or Adaptive Agent based on conditions

**Adaptive Agent:**
- File: `pkg/agent/orchestrator.go`
- Function: `runOptimizedAgent()`
- Use Case: Complex tasks requiring analysis and planning
- Characteristics: Iterative evaluation loop, comprehensive planning

### Key Data Structures

**AgentContext** (`pkg/agent/types.go:33-50`)
```go
type AgentContext struct {
    UserIntent         string
    CurrentPlan        *EditPlan
    IntentAnalysis     *IntentAnalysis
    ExecutedOperations []string
    TokenUsage         *AgentTokenUsage
    // ... other fields
}
```

**IntentAnalysis** (`pkg/agent/types.go:64-70`)
```go
type IntentAnalysis struct {
    Category        string   // "code", "fix", "docs", "test", "review"
    Complexity      string   // "simple", "moderate", "complex"
    EstimatedFiles  []string
    CanExecuteNow   bool
    ImmediateCommand string
}
```

## Configuration Points

### Model Selection

**Orchestration Model:**
- Purpose: Planning, analysis, decision-making
- Config: `cfg.OrchestrationModel`
- Default: Falls back to `cfg.EditingModel`
- Recommendation: Use capable models (GPT-4, Claude-3) for complex tasks

**Editing Model:**
- Purpose: Code generation and editing
- Config: `cfg.EditingModel`
- Use Case: Applied to actual file modifications

### Execution Flags

**Interactive Mode:**
- Config: `cfg.Interactive`
- Effect: Enables tool-calling capabilities
- Default: `true` for agent mode

**Skip Prompts:**
- Config: `cfg.SkipPrompt`
- Effect: Reduces user interaction, enables automation
- Use Case: CI/CD pipelines, batch processing

### Token Management

**Cost Control:**
- File: `pkg/agent/types.go:10-25`
- Structure: `AgentTokenUsage` with split tracking
- Usage: `context.TokenUsage.Planning += tokens`

## Workflow Modification Guide

### Adding New Agent Actions

**Step 1: Define Action in Progress Evaluation**

File: `pkg/agent/agent_prompts.go:BuildProgressEvaluationPrompt()`

Add to AVAILABLE NEXT ACTIONS section:
```go
- "your_new_action": Description of when to use this action
```

**Step 2: Add Decision Logic**

File: `pkg/agent/agent_prompts.go:BuildProgressEvaluationPrompt()`

Add to DECISION LOGIC section:
```go
- **YOUR CONDITION**: When to use your_new_action
```

**Step 3: Implement Action Handler**

File: `pkg/agent/orchestrator.go:runOptimizedAgent()`

Add to switch statement:
```go
case "your_new_action":
    err = executeYourNewAction(context)
```

**Step 4: Implement Handler Function**

File: `pkg/agent/orchestrator.go`

```go
func executeYourNewAction(context *AgentContext) error {
    context.Logger.LogProcessStep("🎯 Executing your new action")
    // Implementation here
    return nil
}
```

### Modifying Intent Analysis

**Current Categories:**
- "code": General code modifications
- "fix": Bug fixes and error resolution
- "docs": Documentation changes
- "test": Testing related tasks
- "review": Code review and analysis

**Adding New Category:**

1. **Update Prompt** (`pkg/agent/agent_prompts.go:BuildIntentAnalysisPrompt()`)
2. **Update Inference** (`pkg/agent/agent.go:inferCategory()`)
3. **Add Playbooks** (if applicable)

### File Discovery Enhancement

**Current Methods:**
1. **Embedding Search**: Semantic similarity
2. **Symbol Index**: Exact symbol matching
3. **Content Search**: Text pattern matching
4. **Shell Commands**: Directory and file operations

**Adding New Discovery Method:**

File: `pkg/agent/agent.go:findRelevantFilesRobust()`

```go
// Add your discovery method
additionalFiles := yourCustomDiscovery(userIntent, cfg, logger)
if len(additionalFiles) > 0 {
    // Integrate with existing results
    relevantFiles = append(relevantFiles, additionalFiles...)
}
```

## Prompt Engineering

### Prompt Structure Guidelines

**System Messages:**
- Start with clear role definition
- Include format instructions
- Specify response constraints

**User Messages:**
- Provide context and constraints
- Include workspace information
- Be specific about expected output format

### Example: Intent Analysis Prompt

```go
prompt := fmt.Sprintf(`Analyze this user intent and classify it for optimal execution:

User Intent: %s

WORKSPACE ANALYSIS:
Project Type: %s
Total Files: %d

CRITICAL WORKSPACE CONSTRAINTS:
- This is a %s project - do NOT suggest files with mismatched extensions

IMMEDIATE EXECUTION OPTIMIZATION:
IMPORTANT: Be VERY conservative with immediate execution...

Respond with JSON:
{
  "Category": "code|fix|docs|test|review",
  "Complexity": "simple|moderate|complex",
  "EstimatedFiles": ["file1.ext", "file2.ext"],
  "CanExecuteNow": false,
  "ImmediateCommand": ""
}`, userIntent, projectType, len(relevantFiles), projectType)
```

### Prompt Tuning Checklist

- [ ] Clear task definition
- [ ] Specific format requirements
- [ ] Context boundaries
- [ ] Error handling instructions
- [ ] Token efficiency considerations

## Playbook Development

### Playbook Structure

**Required Interface:**
```go
type Playbook interface {
    Name() string
    Matches(userIntent string, category string) bool
    BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec
}
```

**Example Implementation:**

File: `pkg/agent/playbooks/your_playbook.go`

```go
type YourPlaybook struct{}

func (p YourPlaybook) Name() string { return "your_playbook" }

func (p YourPlaybook) Matches(userIntent string, category string) bool {
    lo := strings.ToLower(userIntent)
    return strings.Contains(lo, "your trigger phrase")
}

func (p YourPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
    return &PlanSpec{
        Scope: "Description of what this playbook accomplishes",
        Files: estimatedFiles,
        Ops: []PlanOp{
            {
                FilePath:           estimatedFiles[0],
                Description:        "What to do",
                Instructions:       "How to do it",
                ScopeJustification: "Why this is appropriate",
            },
        },
    }
}
```

### Registration

File: `pkg/agent/playbooks/registry.go`

```go
func init() {
    Register(YourPlaybook{})
}
```

### Testing Playbooks

**Unit Tests:**
- File: `pkg/agent/playbooks/your_playbook_test.go`
- Test matching logic
- Test plan generation
- Mock file system operations

**Integration Tests:**
- File: `e2e_test_scripts/test_agent_playbook.sh`
- End-to-end workflow testing
- Validate actual file modifications

## Debugging and Monitoring

### Logging Levels

**Process Steps:**
```go
context.Logger.LogProcessStep("🎯 Starting analysis phase")
```

**Errors:**
```go
context.Logger.LogError(fmt.Errorf("analysis failed: %w", err))
```

**Debug Info:**
```go
context.Logger.Logf("Found %d relevant files", len(files))
```

### Telemetry Integration

**Event Logging:**
```go
if context.Config.TelemetryEnabled {
    logTelemetry(context.Config.TelemetryFile, telemetryEvent{
        Timestamp: time.Now(),
        Intent: context.UserIntent,
        Iteration: context.IterationCount,
        Action: evaluation.NextAction,
        Status: evaluation.Status,
    })
}
```

### Performance Monitoring

**Memory Tracking:**
```go
var m runtime.MemStats
runtime.ReadMemStats(&m)
logger.Logf("Alloc: %v MiB", m.Alloc/1024/1024)
```

**Timing:**
```go
startTime := time.Now()
duration := time.Since(startTime)
logger.Logf("Completed in %v", duration)
```

## Performance Optimization

### Caching Strategies

**Workspace Analysis:**
- Cached in `.ledit/workspace.json`
- Regenerated when missing or outdated
- Includes file summaries and embeddings

**Embedding Cache:**
- File: `pkg/embedding/`
- Vector database for semantic search
- Reduces API calls for file discovery

### Token Efficiency

**Prompt Optimization:**
- Use concise instructions
- Provide minimal necessary context
- Structure for clear responses

**Response Processing:**
- Extract only required JSON
- Validate response format early
- Handle malformed responses gracefully

### Execution Optimization

**Fast Path Conditions:**
- Simple file operations
- Documentation changes
- Single-file modifications
- No complex analysis needed

**Batch Operations:**
- Group related file changes
- Use hunks for precise edits
- Minimize file I/O operations

## Troubleshooting

### Common Issues

**Empty Plans:**
- **Symptom**: Agent produces no edit operations
- **Debug**: Check intent analysis categorization
- **Fix**: Review prompt constraints, add fallback logic

**File Not Found:**
- **Symptom**: Operations fail with file errors
- **Debug**: Verify file discovery methods
- **Fix**: Enhance file discovery, add validation

**Infinite Loops:**
- **Symptom**: Agent exceeds max iterations
- **Debug**: Check progress evaluation logic
- **Fix**: Adjust decision thresholds, add termination conditions

**High Token Usage:**
- **Symptom**: Excessive API costs
- **Debug**: Monitor token usage by category
- **Fix**: Optimize prompts, use cheaper models for simple tasks

### Recovery Strategies

**State Persistence:**
- Agent state saved to `.ledit/run_state.json`
- Enables resume after interruption
- Automatic cleanup on completion

**Error Recovery:**
- Fallback to simpler execution paths
- Retry with different models
- Graceful degradation to manual intervention

### Debug Commands

**Manual Testing:**
```bash
# Test intent analysis
go run main.go agent "your test intent" --debug

# Check workspace analysis
ls -la .ledit/workspace.json

# Monitor token usage
tail -f .ledit/telemetry.log
```

**Log Analysis:**
```bash
# Search for specific patterns
grep "ERROR" .ledit/run.log
grep "PERF:" .ledit/run.log

# Analyze execution flow
grep "Next Action:" .ledit/run.log
```

## Implementation Examples

### Example 1: Adding Custom File Type Support

**Problem:** Agent doesn't handle `.proto` files properly

**Solution:**

1. **Update Project Type Detection:**
   ```go
   // pkg/agent/agent.go
   func detectProjectType() string {
       // Add protobuf detection
       if hasFilesWithExtension(".proto") {
           return "protobuf"
       }
       // ... existing logic
   }
   ```

2. **Add Type-Specific Guidance:**
   ```go
   // pkg/agent/planning.go
   switch projectType {
   case "protobuf":
       preferredExt = ".proto"
       // Add protobuf-specific instructions
   }
   ```

### Example 2: Custom Playbook for Database Migrations

**Problem:** Need specialized handling for database schema changes

**Solution:**

1. **Create Playbook:**
   ```go
   type DatabaseMigrationPlaybook struct{}

   func (p DatabaseMigrationPlaybook) Matches(userIntent string, category string) bool {
       lo := strings.ToLower(userIntent)
       return strings.Contains(lo, "migration") && strings.Contains(lo, "database")
   }

   func (p DatabaseMigrationPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
       return &PlanSpec{
           Scope: "Handle database migration with proper rollback support",
           Files: estimatedFiles,
           Ops: []PlanOp{
               {
                   FilePath:           "migrations/up.sql",
                   Description:        "Create migration up script",
                   Instructions:       "Generate SQL for forward migration",
                   ScopeJustification: "Forward migration logic",
               },
               {
                   FilePath:           "migrations/down.sql",
                   Description:        "Create migration rollback script",
                   Instructions:       "Generate SQL for rollback migration",
                   ScopeJustification: "Rollback capability requirement",
               },
           },
       }
   }
   ```

2. **Register Playbook:**
   ```go
   // pkg/agent/playbooks/registry.go
   func init() {
       Register(DatabaseMigrationPlaybook{})
   }
   ```

### Example 3: Performance Monitoring Enhancement

**Problem:** Need better visibility into agent performance

**Solution:**

1. **Add Custom Metrics:**
   ```go
   // pkg/agent/types.go
   type AgentMetrics struct {
       StartTime        time.Time
       EndTime          time.Time
       TotalIterations  int
       FilesModified    int
       TokensByCategory map[string]int
       MemoryPeak       uint64
   }
   ```

2. **Integrate Tracking:**
   ```go
   // pkg/agent/orchestrator.go
   func runOptimizedAgent(userIntent string, cfg *config.Config, logger *utils.Logger, tokenUsage *AgentTokenUsage) error {
       metrics := &AgentMetrics{
           StartTime: time.Now(),
           TokensByCategory: make(map[string]int),
       }

       defer func() {
           metrics.EndTime = time.Now()
           logAgentMetrics(metrics)
       }()

       // ... existing logic
   }
   ```

This playbook serves as a living document for agent workflow modifications. Update it as new patterns and optimizations are discovered.

## Debugging and Visibility Improvements

### Enhanced Logging Features

The agent now includes comprehensive logging to improve visibility into its decision-making process:

#### Debug Mode
Enable verbose logging with:
```bash
export LEDIT_DEBUG=1
ledit agent "your intent here"
```

#### Log Output
- **Planning Phase**: Shows LLM prompts, responses, and plan parsing
- **Execution Phase**: Details each edit attempt and success/failure
- **Decision Points**: Clear reasoning for each action selection

#### Example Debug Output
```
🐛 Debug mode enabled - verbose logging activated
📝 Planning prompt: Focus files available: [README.md, docs/]
📝 Planning system prompt: Planner: Return ONLY the final JSON plan now...
📝 Focus message: Focus files (prefer these): README.md
📝 User goal: Update the readme to reflect current commands
📋 LLM Response received (1247 chars)
📋 Cleaned JSON response (245 chars): {"edits":[{"file":"README.md","instructions":"Update command descriptions"}]}
📋 Plan parsed successfully: 1 edits found
📋 Edit 1: File=README.md, Instructions=Update command descriptions
📋 Added to plan: README.md
🛠️ Executing operation 1/1: README.md
🛠️ Instructions: Update command descriptions
🛠️ Attempting partial edit for README.md
🛠️ Partial edit succeeded for README.md
✅ Agent v2 execution completed: 1/1 edits applied successfully
```

#### Troubleshooting Common Issues

**Problem**: Agent produces no edits
**Debug Steps**:
1. Check if focus files were identified correctly
2. Verify LLM response contains valid JSON
3. Confirm file paths exist and are accessible
4. Review plan parsing logs for errors

**Problem**: Edits fail during execution
**Debug Steps**:
1. Check file permissions
2. Verify instructions are clear and actionable
3. Review partial vs full edit attempt logs
4. Check for backup file creation (indicates edit attempt)

**Problem**: Unexpected action selection
**Debug Steps**:
1. Enable debug mode to see decision reasoning
2. Check intent analysis results
3. Review progress evaluation prompts and responses
4. Verify pattern matching logic

### Testing the Enhanced Agent

Create a test script to verify improved debugging:
```bash
#!/bin/bash
export LEDIT_DEBUG=1
./ledit agent "Update README.md with current command list" 2>&1 | tee agent_debug.log
```

This will provide complete visibility into:
- Intent analysis process
- File discovery and selection
- Planning prompt and response
- Plan parsing and validation
- Edit execution attempts
- Success/failure outcomes
