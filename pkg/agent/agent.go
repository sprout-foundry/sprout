package agent

// Agent package has been refactored into focused modules for better organization:
//
// - intent_analysis.go: Intent categorization and complexity inference
// - file_discovery.go: File discovery using embeddings, symbols, and content search
// - planning.go: Plan creation and revision logic
// - progress.go: Progress evaluation and state management
// - tools.go: Tool execution and management
// - types.go: Core type definitions and data structures
// - validation.go: Validation and error recovery
// - workspace_discovery.go: Workspace analysis and context building
//
// This file serves as the main entry point and coordination layer.
