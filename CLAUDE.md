# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`ledit` is an AI-powered code editing and assistance tool that leverages Large Language Models (LLMs) to understand workspaces, generate code, and manage development tasks. It functions as a development partner that can implement features, provide intelligent context, and integrate with development tools.

## CRITICAL: Git Operations Policy

**NEVER COMMIT OR PUSH CHANGES**
- Do NOT use `git commit` under any circumstances
- Do NOT use `git push` under any circumstances
- Only the repository owner decides when to commit
- You may use `git add` to stage changes when explicitly asked
- You may use `git status`, `git diff`, and other read-only git commands
- If you're about to type `git commit`, STOP immediately

## Build and Development Commands

### Building
```bash
go build                        # Build the main executable
go install                      # Install to GOPATH/bin
```

### Testing
```bash
python3 test_runner.py          # Run E2E tests via Python test runner
go test ./...                   # Run unit tests
go test ./... -v                # Run unit tests with verbose output
go test -race ./...             # Run unit tests with race detection
go test ./pkg/console/ -v  # Run UI component tests (critical for console UI)
```

**IMPORTANT - UI Testing Policy:**
When making changes to console UI components (`pkg/console/`), **ALWAYS** run the UI component tests to ensure functionality remains intact:
```bash
go test ./pkg/console/ -v
```
The UI components are critical for user interaction and terminal display. Any changes to input handling, footer display, agent console behavior, or related formatting should be validated with the test suite to prevent regressions in the user experience.

## Architecture Overview

### Core Components

**Agent System** (`internal/domain/agent/`):
- **Domain Entities** (`entities.go`): Core agent, execution plan, and workflow event definitions
- **Workflow Management** (`workflow.go`): Agent workflow orchestration

**Todo Management** (`internal/domain/todo/`):
- **Todo Entities** (`entities.go`): Todo and todo list definitions with execution logic
- **Todo Service** (`service.go`): Todo creation, prioritization, and execution

**UI Framework** (`pkg/ui/`):
- **Core UI** (`core/app.go`): Terminal UI application framework
- **Components** (`core/components/`): UI components including dropdowns
- **Styles and Themes** (`styles.go`, `theme/theme.go`): UI styling and theming

**Command Interface** (`cmd/`):
- **Agent Command** (`agent.go`): Interactive AI-powered code editing and assistance
- **Other Commands**: Shell, commit, version, and MCP commands

### Key Data Flow

1. **User Input** → CLI commands parse and route to appropriate handlers
2. **Agent Processing** → Agent system processes requests using LLM providers
3. **Context Building** → Workspace analyzer selects relevant files for LLM context
4. **Code Generation** → LLM generates code changes with workspace awareness
5. **Change Management** → Change tracker records modifications with rollback support

### Command Architecture

Main CLI commands:
- **`ledit agent`**: Interactive AI-powered code editing and assistance
- **`ledit code`**: Direct code generation and modification
- **`ledit question`**: Q&A about the workspace and codebase

### Change Tracking System

The system provides comprehensive change tracking:
- **Revision Tracking**: Every edit generates a revision ID
- **Change Recording**: All file modifications tracked in `.ledit/changelog.json`
- **Rollback Support**: Complete rollback capability for any changes

## Configuration

The system uses layered configuration:

- Global: `~/.ledit/config.json`
- Project: `.ledit/config.json`
- API Keys: `~/.ledit/api_keys.json`

Key configuration aspects:

- **Model Selection**: Different LLM providers and models for various tasks
- **Provider Settings**: API endpoints, authentication, and model parameters
- **Workspace Settings**: File inclusion/exclusion patterns and analysis preferences

## Key Workspace Files

- `.ledit/workspace.json` - Workspace analysis and file summaries
- `.ledit/changelog.json` - Change history for rollback functionality
- `.ledit/runlogs/*.jsonl` - Per-run logs for debugging and telemetry

## Development Notes

- **Modular Architecture**: Clean separation between agent logic, UI components, and API providers
- **Provider Support**: Multi-provider LLM support (OpenAI, Ollama, DeepInfra, Cerebras, etc.)
- **Console UI**: Component-based terminal interface with proper input handling and display
- **Testing**: Python-based E2E test runner and Go unit tests for components
- **Streaming**: Real-time response streaming for improved user experience

## Environment Variables

- **`CI`** or **`GITHUB_ACTIONS`**: When set, agent runs in non-interactive mode suitable for CI/CD pipelines
