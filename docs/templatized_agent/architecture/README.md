# Agent Architecture Analysis

This document provides a detailed analysis of the ledit agent architecture, identifying key patterns and components that should be preserved in a templatized version.

## Core Architecture Patterns

### 1. Command Pattern Implementation

The system uses Cobra CLI framework with a clear command hierarchy:

```
Root Command (ledit)
├── agent - AI agent mode with intent analysis
├── code - Direct code editing with LLM
├── process - Multi-agent orchestration
├── commit - Commit message generation
└── ...other commands
```

**Key Characteristics:**
- Each command encapsulates a specific workflow
- Commands share common configuration and utilities
- Clean separation between command definition and execution logic

### 2. Agent Workflow Pattern

The agent system implements a sophisticated workflow pattern:

```go
// Simplified workflow from agent.go
func RunSimplifiedAgent(userIntent string, skipPrompt bool, model string) error {
    // 1. Initialize context and configuration
    // 2. Analyze intent type (code_update, question, command)
    // 3. Route to appropriate handler
    // 4. Execute with state management
    // 5. Track token usage and costs
}
```

**Key Components:**
- **Intent Analysis**: Classify user input into actionable categories
- **Context Management**: Persistent state across agent sessions
- **Todo Management**: Break down complex tasks into executable units
- **Execution Orchestration**: Coordinate tool usage and LLM calls
- **Result Validation**: Ensure changes work correctly

### 3. Tool System Architecture

The tool system is built around a pluggable architecture:

```go
// Tool interface from tools/types.go
type Tool interface {
    Name() string
    Description() string
    Category() string
    Execute(ctx context.Context, params Parameters) (*Result, error)
    CanExecute(ctx context.Context, params Parameters) bool
    RequiredPermissions() []string
    EstimatedDuration() time.Duration
    IsAvailable() bool
}
```

**Key Features:**
- **Standardized Interface**: All tools implement the same interface
- **Permission System**: Tools declare required permissions
- **Category Organization**: Tools organized by functionality (workspace, file, search, etc.)
- **Registry Pattern**: Tools registered and discovered at runtime

### 4. Configuration Hierarchy

The configuration system uses a sophisticated hierarchy:

```go
type Config struct {
    // Domain-specific configurations
    LLM         *LLMConfig         `json:"llm,omitempty"`
    UI          *UIConfig          `json:"ui,omitempty"`
    Agent       *AgentConfig       `json:"agent,omitempty"`
    Security    *SecurityConfig    `json:"security,omitempty"`
    Performance *PerformanceConfig `json:"performance,omitempty"`

    // Legacy fields for backward compatibility
    EditingModel string `json:"editing_model,omitempty"`
    // ... many other legacy fields
}
```

**Key Patterns:**
- **Domain Separation**: Different concerns have separate configs
- **Backward Compatibility**: Legacy fields maintained
- **Default Values**: Sensible defaults for all settings
- **Validation**: Configuration validation with error reporting

## Key Architectural Decisions

### 1. Simplified vs Complex Agent Modes

The system supports two agent execution modes:

**Simplified Agent Mode:**
- Direct intent → action mapping
- Todo-based task breakdown
- Sequential execution
- Integrated validation

**Complex Orchestration Mode:**
- Multi-agent coordination
- Dependency management
- Parallel execution
- Budget controls

### 2. Tool Execution Strategy

Tools are executed through a unified system:

```go
func executeAgentWorkflowWithTools(ctx *SimplifiedAgentContext, messages []prompts.Message, workflowType string) (string, *llm.TokenUsage, error) {
    // 1. Configure workflow context
    // 2. Set tool limits and permissions
    // 3. Execute with unified interactive system
    // 4. Handle fallbacks and errors
}
```

**Execution Features:**
- **Tool Limits**: Prevent runaway tool usage
- **Fallback Strategy**: Basic LLM calls if tool execution fails
- **Context Preservation**: Maintain conversation context
- **Error Recovery**: Graceful handling of tool failures

### 3. State Management Strategy

Multiple levels of state management:

**Session Context:**
- User intent and goals
- Analysis results
- Current progress
- Token usage tracking

**Execution State:**
- Current todo being worked on
- Tool execution results
- Error states and recovery

**Persistent Context:**
- Cross-session analysis
- Context summaries
- Learning from previous interactions

### 4. Error Handling and Recovery

Sophisticated error handling:

```go
// From agent.go - graceful error handling
if err != nil {
    gracefulExitMsg := prompts.NewGracefulExitWithTokenUsage(
        "AI agent processing your request",
        err,
        tokenUsage,
        modelName,
    )
    fmt.Fprint(os.Stderr, gracefulExitMsg)
    os.Exit(1)
}
```

**Error Handling Features:**
- **Graceful Degradation**: Continue with reduced functionality
- **User-Friendly Messages**: Clear error explanations
- **Token Usage Reporting**: Cost transparency
- **Recovery Suggestions**: Actionable error resolution steps

## LLM Integration Architecture

### 1. Provider Abstraction

The system abstracts LLM providers behind a unified interface:

```go
// From llm/api.go
func CallLLMWithUnifiedInteractive(config *UnifiedInteractiveConfig) (string, *UnifiedInteractiveResponse, error) {
    // Handle different providers (OpenAI, Gemini, Ollama, etc.)
    // Unified tool calling interface
    // Token usage tracking
}
```

**Provider Features:**
- **Unified Interface**: Same API for all providers
- **Tool Calling**: Standardized tool execution across providers
- **Streaming Support**: Real-time response handling
- **Token Tracking**: Usage and cost monitoring

### 2. Model Selection Strategy

Sophisticated model selection:

```go
// From config/llm.go
type LLMConfig struct {
    EditingModel       string `json:"editing_model"`
    SummaryModel       string `json:"summary_model"`
    OrchestrationModel string `json:"orchestration_model"`
    WorkspaceModel     string `json:"workspace_model"`
    EmbeddingModel     string `json:"embedding_model"`
    CodeReviewModel    string `json:"code_review_model"`
    // ... specialized models for different tasks
}
```

**Model Specialization:**
- **Task-Specific Models**: Different models for different tasks
- **Provider Optimization**: Best models per provider
- **Cost Optimization**: Balance cost vs performance
- **Capability Matching**: Match model capabilities to task requirements

## Workspace Integration

### 1. File System Operations

Deep integration with workspace:

```go
// From workspace/workspace_manager.go
type WorkspaceManager struct {
    path    string
    files   []string
    ignore  *WorkspaceIgnore
    context *WorkspaceContext
}
```

**Workspace Features:**
- **File Discovery**: Automatic workspace structure detection
- **Ignore Patterns**: Respect .gitignore and custom ignore files
- **Change Tracking**: Monitor file modifications
- **Build System Integration**: Support for multiple build systems

### 2. Change Management

Sophisticated change tracking:

```go
// From changetracker/changetracker.go
type ChangeTracker struct {
    changes map[string]*FileChange
    mutex   sync.RWMutex
}
```

**Change Tracking Features:**
- **Diff Generation**: Detailed before/after comparisons
- **File State Management**: Track file modifications
- **Undo Support**: Revert changes when needed
- **Validation Integration**: Ensure changes don't break builds

## Security and Permissions

### 1. Tool Permissions

Tools declare required permissions:

```go
// From tools/types.go
const (
    PermissionReadFile       = "read_file"
    PermissionWriteFile      = "write_file"
    PermissionExecuteShell   = "execute_shell"
    PermissionNetworkAccess  = "network_access"
    PermissionUserPrompt     = "user_prompt"
    PermissionWorkspaceRead  = "workspace_read"
    PermissionWorkspaceWrite = "workspace_write"
)
```

**Security Features:**
- **Permission Declaration**: Tools specify what they need
- **Capability Checking**: Verify permissions before execution
- **Sandboxed Execution**: Limit tool capabilities
- **Audit Logging**: Track tool usage for security

### 2. Configuration Security

Security-focused configuration:

```go
// From config/security.go
type SecurityConfig struct {
    EnableSecurityChecks     bool     `json:"enable_security_checks"`
    AllowNetworkAccess       bool     `json:"allow_network_access"`
    AllowedShellCommands     []string `json:"allowed_shell_commands,omitempty"`
    BlockedFilePatterns      []string `json:"blocked_file_patterns,omitempty"`
    MaxFileSize              int64    `json:"max_file_size,omitempty"`
    RequireUserApproval      bool     `json:"require_user_approval,omitempty"`
}
```

**Security Controls:**
- **Network Restrictions**: Control external access
- **Shell Command Whitelisting**: Limit executable commands
- **File Access Controls**: Restrict file operations
- **Size Limits**: Prevent large file processing

## Performance Optimizations

### 1. Caching Strategy

Multiple caching layers:

```go
// From llm/evidence_cache.go
type EvidenceCache struct {
    cache map[string]*CacheEntry
    mutex sync.RWMutex
}
```

**Caching Features:**
- **LLM Response Caching**: Avoid redundant API calls
- **Workspace Analysis Caching**: Cache expensive analysis
- **Embedding Caching**: Reuse computed embeddings
- **Prompt Template Caching**: Cache compiled prompts

### 2. Resource Management

Resource-aware execution:

```go
// From config/performance.go
type PerformanceConfig struct {
    MaxConcurrentRequests int           `json:"max_concurrent_requests"`
    RequestTimeout        time.Duration `json:"request_timeout"`
    MemoryLimit           int64         `json:"memory_limit,omitempty"`
    CPULimit              float64       `json:"cpu_limit,omitempty"`
}
```

**Performance Features:**
- **Concurrency Control**: Limit simultaneous operations
- **Timeout Management**: Prevent hanging operations
- **Resource Limits**: Memory and CPU constraints
- **Adaptive Scaling**: Adjust based on system resources

## Summary

The ledit agent architecture demonstrates sophisticated patterns for building AI agent systems:

1. **Modular Design**: Clear separation of concerns
2. **Extensible Tools**: Pluggable tool system
3. **Configuration Hierarchy**: Flexible configuration management
4. **State Management**: Multi-level state tracking
5. **Security First**: Comprehensive security controls
6. **Performance Aware**: Resource-conscious execution
7. **Error Resilient**: Robust error handling and recovery

These patterns provide a solid foundation for creating templatized agent systems that can be easily configured for specific use cases while maintaining the flexibility and robustness of the original design.
