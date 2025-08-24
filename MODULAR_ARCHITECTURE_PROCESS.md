# Modular Architecture Refactoring Process

## Overview

This document outlines the process to refactor the ledit project from its current architecture to a cleaner, more modular architecture where providers and prompts are less interdependent. This will improve maintainability, testability, and extensibility.

## Current Architecture Analysis

Based on analysis of the codebase, the main areas of tight coupling are:

1. **LLM Providers**: Direct imports across multiple packages (`pkg/llm/api.go`, `pkg/agent/`, `pkg/editor/`)
2. **Prompt System**: Hardcoded prompt dependencies throughout the codebase
3. **Configuration**: Monolithic config structure spread across components
4. **Agent System**: Tightly coupled to specific LLM providers and prompt formats
5. **Editor System**: Direct dependencies on LLM and workspace packages

## Target Architecture

### Core Principles
- **Dependency Inversion**: High-level modules should not depend on low-level modules
- **Interface Segregation**: Clients should not be forced to depend on interfaces they don't use  
- **Single Responsibility**: Each component should have one reason to change
- **Open/Closed**: Open for extension, closed for modification

### Target Structure
```
pkg/
├── interfaces/          # Core interfaces and contracts
├── providers/           # Provider implementations (LLM, storage, etc.)
├── prompts/            # Prompt management and templating
├── core/               # Core business logic
├── adapters/           # External system adapters
└── config/             # Configuration management
```

## Refactoring Process

### Phase 1: Interface Definition & Abstraction
**Timeline: 1-2 weeks**

- [ ] **1.1** Create `pkg/interfaces/` package with core abstractions
  - [ ] Define `LLMProvider` interface with minimal surface area
  - [ ] Define `PromptProvider` interface for prompt management
  - [ ] Define `ConfigProvider` interface for configuration access
  - [ ] Define `WorkspaceProvider` interface for workspace operations
  - [ ] Define `ChangeTracker` interface for change management

- [ ] **1.2** Create `pkg/interfaces/domain/` for domain-specific interfaces
  - [ ] Define `CodeGenerator` interface
  - [ ] Define `WorkspaceAnalyzer` interface  
  - [ ] Define `AgentOrchestrator` interface

- [ ] **1.3** Create adapter interfaces for external dependencies
  - [ ] Define `FileSystem` interface
  - [ ] Define `GitProvider` interface
  - [ ] Define `UIProvider` interface

### Phase 2: Provider Abstraction Layer  
**Timeline: 2-3 weeks**

- [ ] **2.1** Create `pkg/providers/llm/` with clean provider implementations
  - [ ] Extract OpenAI provider to `pkg/providers/llm/openai/`
  - [ ] Extract Gemini provider to `pkg/providers/llm/gemini/`
  - [ ] Extract Ollama provider to `pkg/providers/llm/ollama/`
  - [ ] Extract Groq provider to `pkg/providers/llm/groq/`
  - [ ] Create provider registry with dynamic registration

- [ ] **2.2** Create `pkg/providers/prompts/` for prompt management
  - [ ] Move prompt loading logic from `pkg/prompts/prompts.go`
  - [ ] Create template engine for prompt customization
  - [ ] Add prompt versioning and validation
  - [ ] Create prompt registry with hot-reloading

- [ ] **2.3** Create `pkg/providers/config/` for configuration management
  - [ ] Extract config loading from current `pkg/config/`
  - [ ] Create layered config system (global, project, runtime)
  - [ ] Add config validation and schema support
  - [ ] Create config change notification system

### Phase 3: Core Domain Logic Extraction
**Timeline: 2-3 weeks**

- [ ] **3.1** Create `pkg/core/agent/` for agent business logic
  - [ ] Extract agent orchestration logic from `pkg/agent/agent.go`
  - [ ] Remove direct LLM provider dependencies
  - [ ] Use dependency injection for provider access
  - [ ] Create agent state management abstraction

- [ ] **3.2** Create `pkg/core/editor/` for editing business logic  
  - [ ] Extract code generation logic from `pkg/editor/generate.go`
  - [ ] Remove direct workspace and LLM dependencies
  - [ ] Create editing strategy pattern implementation
  - [ ] Add rollback capability abstraction

- [ ] **3.3** Create `pkg/core/workspace/` for workspace management
  - [ ] Extract workspace analysis from `pkg/workspace/`
  - [ ] Create workspace indexing abstraction
  - [ ] Add workspace change notification system

### Phase 4: Dependency Injection Container
**Timeline: 1-2 weeks**

- [ ] **4.1** Create `pkg/container/` for dependency management
  - [ ] Implement service container with interface registration
  - [ ] Add lifecycle management (singleton, transient, scoped)
  - [ ] Create factory pattern for complex object creation
  - [ ] Add configuration-driven service registration

- [ ] **4.2** Create initialization system
  - [ ] Bootstrap container with default implementations
  - [ ] Add environment-specific overrides
  - [ ] Create health check system for registered services
  - [ ] Add graceful shutdown handling

### Phase 5: Adapter Layer Implementation
**Timeline: 1-2 weeks**

- [ ] **5.1** Create `pkg/adapters/cli/` for CLI integration
  - [ ] Move CLI-specific logic from `cmd/` where appropriate
  - [ ] Create CLI command factory using dependency injection
  - [ ] Add CLI middleware for common operations

- [ ] **5.2** Create `pkg/adapters/filesystem/` for file operations
  - [ ] Extract file operations from various packages
  - [ ] Create mockable filesystem interface implementation
  - [ ] Add file watching and change notification

- [ ] **5.3** Create `pkg/adapters/git/` for Git operations
  - [ ] Extract Git operations from `pkg/git/`
  - [ ] Create Git provider abstraction
  - [ ] Add Git hook management

### Phase 6: Configuration System Overhaul
**Timeline: 1-2 weeks**

- [ ] **6.1** Implement layered configuration
  - [ ] System-level defaults
  - [ ] User-level overrides (`~/.ledit/config.json`)
  - [ ] Project-level overrides (`.ledit/config.json`)  
  - [ ] Runtime overrides (CLI flags, environment variables)

- [ ] **6.2** Add configuration validation
  - [ ] JSON schema validation
  - [ ] Provider capability validation
  - [ ] Configuration migration system

- [ ] **6.3** Create configuration hot-reloading
  - [ ] File watcher for config changes
  - [ ] Service notification for config updates
  - [ ] Graceful config reload without restart

### Phase 7: Testing Infrastructure
**Timeline: 1-2 weeks**

- [ ] **7.1** Create mock implementations
  - [ ] Mock LLM providers for testing
  - [ ] Mock filesystem for isolated tests
  - [ ] Mock workspace for deterministic tests

- [ ] **7.2** Add integration testing framework
  - [ ] Component integration tests
  - [ ] End-to-end workflow tests
  - [ ] Performance regression tests

- [ ] **7.3** Create test utilities
  - [ ] Test fixture management
  - [ ] Assertion helpers for domain objects
  - [ ] Test configuration builder

### Phase 8: Migration & Cleanup
**Timeline: 2-3 weeks**

- [ ] **8.1** Update existing packages to use new interfaces
  - [ ] Refactor `cmd/` to use dependency injection
  - [ ] Update `pkg/orchestration/` to use new abstractions
  - [ ] Update `pkg/changetracker/` to use new interfaces

- [ ] **8.2** Remove deprecated code
  - [ ] Remove old LLM API implementations
  - [ ] Remove hardcoded prompt references
  - [ ] Clean up circular dependencies

- [ ] **8.3** Update documentation
  - [ ] Update CLAUDE.md with new architecture
  - [ ] Create developer documentation for new interfaces
  - [ ] Update README with new configuration options

### Phase 9: Performance Optimization
**Timeline: 1 week**

- [ ] **9.1** Optimize service resolution
  - [ ] Cache frequently accessed services
  - [ ] Optimize interface dispatch
  - [ ] Profile service creation overhead

- [ ] **9.2** Add monitoring and metrics
  - [ ] Service performance metrics
  - [ ] Provider health monitoring  
  - [ ] Configuration change tracking

### Phase 10: Documentation & Training
**Timeline: 1 week**

- [ ] **10.1** Create architecture documentation
  - [ ] Document new interfaces and their responsibilities
  - [ ] Create service interaction diagrams
  - [ ] Document extension points for new providers

- [ ] **10.2** Create developer guides
  - [ ] How to add new LLM providers
  - [ ] How to customize prompts
  - [ ] How to extend the agent system

- [ ] **10.3** Update build and deployment
  - [ ] Update build scripts for new structure
  - [ ] Update testing workflows
  - [ ] Update release documentation

## Success Criteria

### Technical Metrics
- [ ] Zero circular dependencies between packages
- [ ] All external dependencies accessed through interfaces
- [ ] 90%+ test coverage on core business logic
- [ ] Sub-100ms service resolution time
- [ ] All providers hot-swappable without restart

### Functional Metrics  
- [ ] All existing functionality preserved
- [ ] New LLM provider can be added in <4 hours
- [ ] New prompt templates can be added without code changes
- [ ] Configuration changes don't require application restart
- [ ] Agent behavior can be customized through configuration alone

## Risk Mitigation

### Technical Risks
- **Large-scale refactoring impact**: Implement changes incrementally with feature flags
- **Performance regression**: Continuous benchmarking during refactoring  
- **Interface design issues**: Create prototypes and validate with real use cases
- **Dependency injection overhead**: Profile and optimize container performance

### Process Risks
- **Timeline overrun**: Break phases into smaller, deliverable chunks
- **Feature regression**: Comprehensive automated testing at each phase
- **Team coordination**: Regular architecture review meetings
- **User impact**: Maintain backward compatibility during transition

## Implementation Notes

### Development Workflow
1. Create feature branch for each phase
2. Implement interfaces first, then implementations
3. Maintain backward compatibility until migration complete
4. Use build tags to manage transition (`//go:build !modular`)
5. Run full test suite after each phase

### Dependencies
- Go 1.19+ for generics in interface definitions
- Consider using wire or similar for dependency injection
- Maintain current external dependencies during transition

### Rollback Plan
- Each phase should be independently deployable
- Keep old implementations alongside new ones during transition
- Use feature flags to switch between old and new implementations
- Automated rollback triggers based on key metrics

---

*This process document should be reviewed and updated as implementation progresses. Each checkbox represents a discrete deliverable that can be tracked and validated.*