# Phases 3-9 Detailed Implementation Plan

Based on comprehensive codebase analysis, this document outlines the detailed implementation strategy for completing the modular architecture refactoring.

## Risk Assessment Summary

### Critical Path Components (Cannot Break)
- `pkg/agent/agent.go` (2,411 lines) - Core user workflow
- `pkg/llm/api.go` - All AI functionality flows through this
- `pkg/config/config.go` - Global configuration system
- `cmd/` directory - CLI interface users depend on

### High Coupling Risk Areas
- Agent ↔ Editor ↔ Workspace triangle dependency
- Config scattered across 15+ packages
- LLM calls duplicated in 20+ files

## Phase 3: Core Domain Logic Extraction (Priority: HIGH)

### 3.1 Extract Todo Management Domain Logic
**Target Files**: `pkg/agent/todo_management.go` → `internal/domain/todo/`

**Implementation Steps**:
- [ ] Create `internal/domain/todo/todo.go` with domain entities
- [ ] Create `internal/domain/todo/service.go` with business logic
- [ ] Extract todo prioritization logic from `AnalyzeTodos()` function
- [ ] Extract todo execution logic from `ExecuteTodo()` function
- [ ] Create interfaces for todo persistence and execution

**Files to Modify**:
```
pkg/agent/todo_management.go (238 lines) - Extract core logic
pkg/agent/agent.go - Update to use new domain service
```

**Domain Model**:
```go
// internal/domain/todo/todo.go
type Todo struct {
    ID          string
    Description string
    Type        TodoType
    Priority    int
    Status      Status
    Dependencies []string
    Context     map[string]interface{}
}

type TodoService interface {
    CreateFromIntent(intent string, context WorkspaceContext) ([]Todo, error)
    PrioritizeTodos(todos []Todo) ([]Todo, error)
    ExecuteTodo(todo Todo, executor TodoExecutor) error
    SelectNextTodo(todos []Todo) (*Todo, error)
}
```

### 3.2 Extract Agent Workflow Domain Logic
**Target Files**: `pkg/agent/agent.go` (2,411 lines) → `internal/domain/agent/`

**Implementation Steps**:
- [ ] Create `internal/domain/agent/intent_analyzer.go`
- [ ] Create `internal/domain/agent/workflow_manager.go`
- [ ] Create `internal/domain/agent/context_manager.go`
- [ ] Extract `analyzeIntentType()` function logic
- [ ] Extract workflow execution patterns
- [ ] Create agent state management abstraction

**Domain Model**:
```go
// internal/domain/agent/workflow.go
type AgentWorkflow interface {
    AnalyzeIntent(intent string) (IntentAnalysis, error)
    CreateExecutionPlan(analysis IntentAnalysis) (ExecutionPlan, error)
    ExecutePlan(plan ExecutionPlan) (ExecutionResult, error)
}

type IntentAnalysis struct {
    Type        IntentType
    Complexity  ComplexityLevel
    FileTargets []string
    Strategy    ExecutionStrategy
}
```

### 3.3 Extract Code Generation Domain Logic
**Target Files**: `pkg/editor/generate.go` → `internal/domain/codegen/`

**Implementation Steps**:
- [ ] Create `internal/domain/codegen/generator.go`
- [ ] Create `internal/domain/codegen/strategy.go`
- [ ] Extract strategy selection logic from `OptimizedEditingService`
- [ ] Extract validation logic
- [ ] Create rollback management abstraction

**Domain Model**:
```go
// internal/domain/codegen/generator.go
type CodeGenerator interface {
    GenerateCode(request CodeGenerationRequest) (CodeGenerationResult, error)
    ValidateChanges(changes ChangeSet) (ValidationResult, error)
    ApplyChanges(changes ChangeSet) error
}
```

## Phase 4: Dependency Injection Container (Priority: HIGH)

### 4.1 Enhance Existing Container System
**Target Files**: `pkg/boundaries/container.go` → Enhanced DI system

**Implementation Steps**:
- [ ] Analyze current container implementation
- [ ] Add service lifecycle management (singleton, transient)
- [ ] Add configuration-driven service registration
- [ ] Create factory interfaces for complex service creation
- [ ] Add service health checks and monitoring

**Enhanced Container**:
```go
// pkg/boundaries/container.go (enhanced)
type Container interface {
    // Core services
    GetLLMService() ports.LLMService
    GetWorkspaceService() ports.WorkspaceService
    GetConfigService() ports.ConfigService
    
    // Domain services
    GetTodoService() domain.TodoService
    GetAgentWorkflow() domain.AgentWorkflow
    GetCodeGenerator() domain.CodeGenerator
    
    // Application services
    GetAgentUseCase() application.AgentUseCase
    GetCodeGenUseCase() application.CodeGenUseCase
    
    // Lifecycle management
    Initialize() error
    Shutdown() error
    HealthCheck() error
}
```

### 4.2 Create Service Registration System
**Implementation Steps**:
- [ ] Create `pkg/boundaries/registry.go`
- [ ] Implement service discovery mechanism
- [ ] Add configuration-based service binding
- [ ] Create service factory pattern
- [ ] Add graceful service initialization

**Service Registry**:
```go
// pkg/boundaries/registry.go
type ServiceRegistry interface {
    RegisterFactory(name string, factory ServiceFactory) error
    RegisterSingleton(name string, instance interface{}) error
    GetService(name string) (interface{}, error)
    ListServices() []ServiceInfo
}
```

### 4.3 Wire Dependencies in Main Application
**Target Files**: `main.go`, `cmd/root.go`

**Implementation Steps**:
- [ ] Initialize container in main.go
- [ ] Inject container into command system
- [ ] Update all commands to use dependency injection
- [ ] Add configuration loading for service bindings
- [ ] Implement graceful shutdown handling

## Phase 5: Adapter Layer Implementation (Priority: MEDIUM)

### 5.1 Create LLM Service Adapters
**Target Files**: `pkg/llm/` → `internal/adapters/llm/`

**Implementation Steps**:
- [ ] Create `internal/adapters/llm/openai_adapter.go`
- [ ] Create `internal/adapters/llm/gemini_adapter.go`
- [ ] Create `internal/adapters/llm/ollama_adapter.go`
- [ ] Extract common retry and error handling patterns
- [ ] Add adapter-specific configuration

**Adapter Pattern**:
```go
// internal/adapters/llm/openai_adapter.go
type OpenAIAdapter struct {
    client HTTPClient
    config OpenAIConfig
    retry  RetryPolicy
}

func (a *OpenAIAdapter) GenerateResponse(ctx context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
    // Implement OpenAI-specific logic
}
```

### 5.2 Create Workspace Service Adapters
**Target Files**: `pkg/workspace/` → `internal/adapters/workspace/`

**Implementation Steps**:
- [ ] Create `internal/adapters/workspace/filesystem_adapter.go`
- [ ] Create `internal/adapters/workspace/git_adapter.go`
- [ ] Extract file discovery and analysis logic
- [ ] Add workspace caching layer
- [ ] Implement workspace change notifications

### 5.3 Create Configuration Service Adapters
**Target Files**: `pkg/config/` → `internal/adapters/config/`

**Implementation Steps**:
- [ ] Create `internal/adapters/config/layered_adapter.go`
- [ ] Create `internal/adapters/config/file_adapter.go`
- [ ] Implement configuration validation
- [ ] Add configuration migration support
- [ ] Create configuration hot-reload mechanism

## Phase 6: Configuration System Overhaul (Priority: HIGH)

### 6.1 Create Layered Configuration System
**Target**: Replace direct config access with layered system

**Implementation Steps**:
- [ ] Create configuration layer hierarchy (system → user → project → runtime)
- [ ] Implement configuration merging logic
- [ ] Add configuration validation schemas
- [ ] Create migration tools for existing configurations
- [ ] Add configuration change notifications

**Layered Config Model**:
```go
// internal/config/layered.go
type LayeredConfig interface {
    GetString(key string) string
    GetBool(key string) bool
    GetInt(key string) int
    GetSection(name string) ConfigSection
    SetValue(key string, value interface{}) error
    Save() error
    Reload() error
}
```

### 6.2 Replace Direct Configuration Access
**Target Files**: All files accessing `config.Config` directly

**Implementation Steps**:
- [ ] Find all direct config access points (15+ packages identified)
- [ ] Replace with dependency-injected configuration service
- [ ] Update package constructors to accept config providers
- [ ] Add configuration validation at service boundaries
- [ ] Maintain backward compatibility for existing config files

### 6.3 Add Configuration Schema Validation
**Implementation Steps**:
- [ ] Create JSON schema for configuration validation
- [ ] Add configuration validation on load
- [ ] Provide helpful error messages for invalid configurations
- [ ] Create configuration documentation generator
- [ ] Add configuration testing utilities

## Phase 7: Testing Infrastructure (Priority: HIGH)

### 7.1 Create Domain Testing Framework
**Implementation Steps**:
- [ ] Create mock implementations for all domain interfaces
- [ ] Add domain logic unit tests
- [ ] Create test doubles for external dependencies
- [ ] Implement property-based testing for core algorithms
- [ ] Add domain model validation tests

**Test Framework Structure**:
```
internal/testing/
├── mocks/              # Generated mocks
├── fixtures/           # Test data
├── builders/           # Test object builders
└── assertions/         # Custom assertions
```

### 7.2 Create Integration Testing Framework
**Implementation Steps**:
- [ ] Create integration test harness
- [ ] Add adapter integration tests
- [ ] Create end-to-end workflow tests
- [ ] Implement test containers for external dependencies
- [ ] Add performance regression tests

### 7.3 Add Service Testing Utilities
**Implementation Steps**:
- [ ] Create service test base classes
- [ ] Add dependency injection testing utilities
- [ ] Create configuration testing helpers
- [ ] Add test environment management
- [ ] Implement test data management

## Phase 8: Migration & Cleanup (Priority: MEDIUM)

### 8.1 Implement Feature Flag Migration
**Implementation Steps**:
- [ ] Add feature flags for modular architecture components
- [ ] Implement gradual migration switches
- [ ] Create A/B testing framework for new vs old implementations
- [ ] Add monitoring and metrics for migration progress
- [ ] Create rollback procedures for each migration step

**Feature Flag Pattern**:
```go
// pkg/migration/flags.go
type FeatureFlags interface {
    UseModularAgent() bool
    UseNewConfigSystem() bool
    UseServiceContainer() bool
}
```

### 8.2 Update Existing Packages
**Target Files**: All packages using old patterns

**Implementation Steps**:
- [ ] Update `cmd/` packages to use dependency injection (19 files)
- [ ] Update `pkg/orchestration/` to use new architecture (12 files)
- [ ] Update `pkg/changetracker/` to use new interfaces (5 files)
- [ ] Update `pkg/codereview/` to use new patterns (4 files)
- [ ] Update all test files to use new testing framework

### 8.3 Remove Deprecated Code
**Implementation Steps**:
- [ ] Identify deprecated functions and types
- [ ] Create deprecation timeline and notices
- [ ] Remove old LLM API implementations
- [ ] Clean up circular dependencies
- [ ] Remove global state variables

**Deprecation Strategy**:
- Mark deprecated code with clear migration paths
- Maintain backward compatibility for 2 releases
- Provide migration tools and documentation

## Phase 9: Performance Optimization (Priority: LOW)

### 9.1 Optimize Service Resolution
**Implementation Steps**:
- [ ] Profile service container performance
- [ ] Implement service resolution caching
- [ ] Optimize interface dispatch overhead
- [ ] Add lazy initialization for expensive services
- [ ] Implement service pooling for high-frequency operations

### 9.2 Add Monitoring and Metrics
**Implementation Steps**:
- [ ] Add service performance metrics
- [ ] Implement provider health monitoring
- [ ] Add configuration change tracking
- [ ] Create performance dashboards
- [ ] Add alerting for performance degradation

**Metrics Framework**:
```go
// internal/monitoring/metrics.go
type MetricsCollector interface {
    RecordServiceCall(service string, duration time.Duration)
    RecordConfigChange(key string, oldValue, newValue interface{})
    RecordProviderHealth(provider string, healthy bool)
}
```

### 9.3 Implement Caching Strategy
**Implementation Steps**:
- [ ] Add workspace analysis caching
- [ ] Implement LLM response caching
- [ ] Add configuration value caching
- [ ] Create cache invalidation strategies
- [ ] Add cache performance monitoring

## Implementation Timeline

### Week 1-2: Phase 3 (Domain Logic Extraction)
- Extract todo management domain logic
- Extract agent workflow domain logic
- Create initial domain interfaces

### Week 3-4: Phase 4 (Dependency Injection)
- Enhance container system
- Create service registration
- Wire dependencies in main application

### Week 5-6: Phase 5 (Adapter Layer)
- Create LLM service adapters
- Create workspace service adapters
- Implement adapter interfaces

### Week 7-8: Phase 6 (Configuration Overhaul)
- Implement layered configuration
- Replace direct config access
- Add configuration validation

### Week 9-10: Phase 7 (Testing Infrastructure)
- Create testing framework
- Add comprehensive test coverage
- Implement integration tests

### Week 11-12: Phase 8 (Migration & Cleanup)
- Implement feature flag migration
- Update existing packages
- Remove deprecated code

### Week 13-14: Phase 9 (Performance Optimization)
- Optimize service resolution
- Add monitoring and metrics
- Implement caching strategies

## Success Criteria

### Technical Metrics
- [ ] Zero circular dependencies between packages
- [ ] 90%+ test coverage on domain logic
- [ ] Sub-10ms service resolution time
- [ ] All external dependencies accessed through interfaces
- [ ] Configuration hot-reload working

### Functional Metrics
- [ ] All existing CLI commands work unchanged
- [ ] Agent functionality maintains current performance
- [ ] New providers can be added without code changes
- [ ] Configuration changes don't require application restart
- [ ] Rollback functionality preserved

### Quality Metrics
- [ ] No breaking changes for end users
- [ ] Documentation covers all new interfaces
- [ ] Migration guides available for extensions
- [ ] Performance monitoring in place
- [ ] Error handling improved with better messages

## Risk Mitigation Strategy

### Development Risks
- **Large file refactoring**: Break down in small, incremental changes
- **Integration complexity**: Use feature flags for gradual rollout
- **Performance regression**: Continuous benchmarking and monitoring
- **Test coverage gaps**: Test-driven development for all new code

### User Impact Risks
- **Breaking changes**: Maintain backward compatibility layers
- **Configuration migration**: Provide automatic migration tools
- **CLI interface changes**: Keep all existing commands functional
- **Documentation gaps**: Update docs alongside code changes

This detailed plan provides a systematic approach to completing the modular architecture refactoring while minimizing risks and maintaining system stability.