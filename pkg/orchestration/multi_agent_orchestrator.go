package orchestration

// This file now serves as the main entry point and coordination layer.
// The actual implementation has been split into focused modules:
// - coordinator.go: Main orchestration logic and agent initialization
// - runner.go: Agent execution and step running logic
// - dependencies.go: Dependency management and step ordering
// - state.go: State persistence and loading functionality
// - validation.go: Validation logic and agent budget management

// Note: MultiAgentOrchestrator and AgentRunner types are defined in coordinator.go
// All function implementations have been moved to focused modules.
