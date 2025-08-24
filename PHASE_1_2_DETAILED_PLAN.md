# Phase 1 & 2 Detailed Implementation Plan

## Phase 1: Interface Definition & Abstraction

### 1.1 Create Core Interfaces Package

**File: `pkg/interfaces/core.go`**
- [ ] `LLMProvider` interface - minimal surface area for LLM operations
- [ ] `PromptProvider` interface - prompt loading and templating
- [ ] `ConfigProvider` interface - configuration access
- [ ] `WorkspaceProvider` interface - workspace operations
- [ ] `ChangeTracker` interface - change management

**File: `pkg/interfaces/domain.go`**
- [ ] `CodeGenerator` interface - code generation operations
- [ ] `WorkspaceAnalyzer` interface - workspace analysis
- [ ] `AgentOrchestrator` interface - agent coordination

**File: `pkg/interfaces/adapters.go`**
- [ ] `FileSystem` interface - file system operations
- [ ] `GitProvider` interface - Git operations  
- [ ] `UIProvider` interface - user interface operations

### 1.2 Create Types Package for Shared Types

**File: `pkg/interfaces/types/common.go`**
- [ ] `TokenUsage` struct
- [ ] `Message` struct
- [ ] `ModelInfo` struct
- [ ] `WorkspaceContext` struct
- [ ] `ChangeSet` struct

**File: `pkg/interfaces/types/config.go`**
- [ ] `ProviderConfig` struct
- [ ] `AgentConfig` struct
- [ ] `EditorConfig` struct

## Phase 2: Provider Abstraction Layer

### 2.1 LLM Provider Abstraction

**Directory: `pkg/providers/llm/`**

**File: `pkg/providers/llm/registry.go`**
- [ ] Provider registry implementation
- [ ] Dynamic provider registration
- [ ] Provider discovery and validation
- [ ] Health checking for providers

**File: `pkg/providers/llm/factory.go`**
- [ ] Provider factory pattern
- [ ] Configuration-driven provider creation
- [ ] Provider lifecycle management

**Directory: `pkg/providers/llm/openai/`**
- [ ] Extract OpenAI provider from `pkg/llm/api.go`
- [ ] Implement `LLMProvider` interface
- [ ] Remove external dependencies
- [ ] Add provider-specific configuration

**Directory: `pkg/providers/llm/gemini/`**
- [ ] Extract Gemini provider from `pkg/llm/gemini_api.go`
- [ ] Implement `LLMProvider` interface
- [ ] Remove external dependencies
- [ ] Add provider-specific configuration

**Directory: `pkg/providers/llm/ollama/`**
- [ ] Extract Ollama provider from `pkg/llm/ollama_api.go`
- [ ] Implement `LLMProvider` interface
- [ ] Remove external dependencies
- [ ] Add provider-specific configuration

### 2.2 Prompt Provider Abstraction

**Directory: `pkg/providers/prompts/`**

**File: `pkg/providers/prompts/manager.go`**
- [ ] Extract prompt loading logic from `pkg/prompts/prompts.go`
- [ ] Implement `PromptProvider` interface
- [ ] Add template engine for customization
- [ ] Add prompt versioning system

**File: `pkg/providers/prompts/registry.go`**
- [ ] Prompt registry with hot-reloading
- [ ] Prompt validation system
- [ ] User override management

**File: `pkg/providers/prompts/templates.go`**
- [ ] Template engine implementation
- [ ] Variable substitution system
- [ ] Conditional prompt sections

### 2.3 Configuration Provider Abstraction

**Directory: `pkg/providers/config/`**

**File: `pkg/providers/config/layered.go`**
- [ ] Layered configuration system (global, project, runtime)
- [ ] Configuration merging and precedence
- [ ] Environment variable support

**File: `pkg/providers/config/validation.go`**
- [ ] Configuration validation and schema support
- [ ] Provider capability validation
- [ ] Migration system for config changes

**File: `pkg/providers/config/watcher.go`**
- [ ] Configuration change notification system
- [ ] File watching for hot-reload
- [ ] Change event propagation

## Implementation Strategy

### Step-by-Step Approach

1. **Create interfaces first** - Define contracts before implementations
2. **Implement one provider type at a time** - Start with LLM providers
3. **Keep old implementations** - Use build tags for gradual migration
4. **Add integration points** - Create factory functions for easy adoption
5. **Test each component** - Unit tests for each new interface and implementation

### Build Tags Strategy

Use build tags to manage the transition:

```go
//go:build !modular
// old implementation

//go:build modular  
// new implementation
```

### Backward Compatibility

- Keep existing function signatures during transition
- Create adapter functions that delegate to new interfaces
- Use feature flags for gradual rollout

### Testing Approach

- Unit tests for each interface implementation
- Integration tests for provider registries
- Mock implementations for testing consumers
- Benchmarks to ensure no performance regression

## Success Criteria for Phases 1 & 2

### Phase 1 Completion
- [ ] All core interfaces defined with clear contracts
- [ ] Shared types extracted to common package
- [ ] Interface documentation complete
- [ ] Build passes with new interfaces

### Phase 2 Completion
- [ ] All LLM providers extracted and implementing common interface
- [ ] Provider registry working with dynamic registration
- [ ] Prompt system extracted with template engine
- [ ] Configuration system supports layered approach
- [ ] All providers can be created via factory functions
- [ ] Build passes with new provider implementations
- [ ] Basic integration tests passing

## File Structure After Implementation

```
pkg/
├── interfaces/
│   ├── core.go           # Core service interfaces
│   ├── domain.go         # Domain-specific interfaces  
│   ├── adapters.go       # External adapter interfaces
│   └── types/
│       ├── common.go     # Shared data types
│       └── config.go     # Configuration types
├── providers/
│   ├── llm/
│   │   ├── registry.go   # Provider registry
│   │   ├── factory.go    # Provider factory
│   │   ├── openai/       # OpenAI implementation
│   │   ├── gemini/       # Gemini implementation
│   │   └── ollama/       # Ollama implementation
│   ├── prompts/
│   │   ├── manager.go    # Prompt management
│   │   ├── registry.go   # Prompt registry
│   │   └── templates.go  # Template engine
│   └── config/
│       ├── layered.go    # Layered config
│       ├── validation.go # Config validation
│       └── watcher.go    # Change watching
└── [existing packages remain unchanged during transition]
```

## Risk Mitigation

1. **Interface Design Issues**: Create prototypes and validate with existing code
2. **Performance Concerns**: Benchmark each implementation against current code  
3. **Complex Dependencies**: Start with least coupled components (LLM providers)
4. **Integration Challenges**: Create comprehensive integration tests

---

*This detailed plan will be updated as implementation progresses and issues are discovered.*