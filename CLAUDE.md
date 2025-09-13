# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`ledit` is an AI-powered code editing and assistance tool that leverages Large Language Models (LLMs) to understand workspaces, generate code, and orchestrate complex features. It functions as a development partner that can implement features, provide intelligent context, self-correct, and integrate with development tools.

## Build and Development Commands

### Building
```bash
go build                        # Build the main executable
go install                      # Install to GOPATH/bin
```

### Testing
```bash
./test_e2e.sh                  # Run end-to-end tests via Python test runner
./test_e2e.sh --single         # Run single test mode (interactive selection)
python3 test_runner.py         # Direct test runner execution with parallel support
go test ./...                  # Run unit tests
go test ./... -v               # Run unit tests with verbose output
go test -race ./...            # Run unit tests with race detection
```

### Development Scripts
```bash
# E2E test scripts are located in e2e_test_scripts/
# Examples:
./e2e_test_scripts/test_agent_v2_full_edit.sh
./e2e_test_scripts/test_multi_file_edit.sh
./e2e_test_scripts/test_orchestration.sh
```

## Architecture Overview

### Core Components

**Agent System** (`pkg/agent/`):
- **Simplified Agent** (`agent.go`): Main entry point with optimized editing strategies  
- **Editing Optimizer** (`editing_optimizer.go`): Intelligent strategy selection between quick and full edits with rollback support
- **Todo Management** (`todo_management.go`): Task breakdown and execution coordination
- **Context Manager** (`context_manager.go`): Persistent context across agent operations

**Editor System** (`pkg/editor/`):
- **Code Generation** (`generate.go`): Core code generation with workspace context integration
- **Partial Editing** (`partial_edit.go`): Targeted file section modifications  
- **Rollback Support** (`rollback_aware.go`): Version-aware editing with full rollback capabilities
- **Change Tracking** (`updates.go`): File update management with review integration

**Multi-Agent Orchestration** (`pkg/orchestration/`):
- **Coordinator** (`coordinator.go`): Multi-agent process management with personas
- **Process Loader** (`process_loader.go`): JSON-based agent configuration loading
- **State Management** (`state.go`): Orchestration state persistence and recovery

**Workspace Intelligence** (`pkg/workspace/`):
- **Context Builder** (`workspace_context.go`): Smart file selection for LLM context
- **Analyzer** (`workspace_analyzer.go`): Workspace indexing and summarization  
- **Manager** (`workspace_manager.go`): Workspace lifecycle and maintenance

**LLM Integration** (`pkg/llm/`):
- **Multi-Provider API** (`api.go`): Unified interface for OpenAI, Gemini, Groq, Ollama, DeepInfra
- **Interactive LLM** (`unified_interactive.go`): Tool-enabled LLM interactions
- **Cost Tracking** (`pricing.go`, `token_utils.go`): Real-time cost monitoring across providers

### Key Data Flow

1. **User Input** → CLI commands (`cmd/`) parse and route to appropriate handlers
2. **Agent Processing** → Agent system analyzes intent and breaks down tasks, selects editing strategy  
3. **Context Building** → Workspace analyzer selects relevant files and builds LLM context
4. **Code Generation** → Editor system generates changes using optimal strategy (quick vs full)
5. **Change Management** → Change tracker records all modifications with rollback support
6. **Validation** → Code review and validation systems ensure quality

### Command Architecture

The CLI supports several modes of operation:
- **`ledit code`**: Direct code generation and editing
- **`ledit agent`**: Intent-driven autonomous operations with task breakdown
  - Interactive mode: Uses simple console input by default
  - Use `--tui` flag for experimental TUI mode
- **`ledit process`**: Multi-step orchestration for complex features
- **`ledit question`**: Interactive Q&A about the workspace
- **`ledit fix`**: Error-driven code fixing with validation loops


### Multi-Agent Architecture

The system supports complex workflows through JSON-defined multi-agent orchestration:

```json
{
  "goal": "Complex feature implementation",
  "agents": [
    {"id": "planner", "persona": "Senior architect", "model": "..."},
    {"id": "implementer", "persona": "Full-stack developer", "model": "..."}
  ],
  "steps": [
    {"id": "analyze", "agent": "planner", "type": "analysis"},
    {"id": "implement", "agent": "implementer", "depends_on": ["analyze"]}
  ]
}
```

### Editing Strategy Intelligence

The **OptimizedEditingService** automatically selects between:
- **Quick Edit**: Single file, simple changes (70% faster, 60% lower cost)  
- **Full Edit**: Multi-file, complex changes with comprehensive review

Strategy selection factors:
- File scope (single vs multiple files)
- Task complexity (keywords: "add/fix" vs "refactor/architecture")  
- Estimated change size and cost
- Multi-file dependencies

### Rollback System

Both editing paths support complete rollback via the changelog system:
- **Revision Tracking**: Every edit generates a revision ID
- **Change Recording**: All file modifications tracked in `.ledit/changelog.json`
- **CLI Rollback**: `ledit rollback [revision-id]` 
- **Service API**: Programmatic rollback support

## Configuration

The system uses layered configuration:
- Global: `~/.ledit/config.json` 
- Project: `.ledit/config.json`
- API Keys: `~/.ledit/api_keys.json`

Key configuration aspects:
- **Model Selection**: Different models for editing, orchestration, workspace analysis, and summarization
- **Code Style**: Project-specific style guidelines that influence LLM generation
- **Security**: Credential scanning and safety checks
- **Performance**: Cost optimization and caching settings

## Key Workspace Files

- `.ledit/workspace.json` - Workspace analysis index with file summaries and exports
- `.ledit/changelog.json` - Change history for rollback functionality  
- `.ledit/leditignore` - Files to exclude from workspace analysis
- `setup.sh` / `validate.sh` - Generated project setup and validation scripts

## Context System Features

The workspace context system provides intelligent file selection:

**Context Directives**:
- `#<filepath>` - Include specific file content
- `#WORKSPACE` / `#WS` - Smart workspace context selection
- `#SG "query"` - Web search grounding for up-to-date information

**Smart Context Selection**:
The system analyzes all workspace files and intelligently determines which files to include as full content vs summary for optimal LLM context usage.

## Development Notes

- **Modular Architecture**: Currently undergoing modular architecture refactor on `feat/modular-architecture-refactor` branch
- **Build Tags**: Some agent components use `//go:build !agent2refactor` to manage refactoring
- **Provider Support**: Extensive multi-provider LLM support with unified cost tracking (OpenAI, Gemini, Groq, Ollama, DeepInfra, Cerebras, DeepSeek)
- **Security Focus**: Built-in credential scanning and safety checks
- **Self-Correction**: Orchestration includes retry logic with error analysis and web search
- **TDD Integration**: Test-driven development workflows in orchestration mode
- **Testing**: Python-based E2E test runner with parallel execution and timeout handling
- **Console UI**: Clean component-based architecture for terminal interactions with proper input handling and footer display