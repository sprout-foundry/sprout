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

## Code Quality Evaluation

Before making changes, evaluate code against these metrics:

### File Size Guidelines
- **Target**: Under 500 lines per file
- **Review threshold**: Files over 800 lines need splitting consideration
- Large files violate Single Responsibility Principle and are hard to maintain

### SRP (Single Responsibility Principle)
- Each type should have one primary responsibility
- Functions should do one thing well
- If a file imports many unrelated packages, it likely violates SRP

### Code Duplication
- Use existing utilities rather than duplicating logic
- Look for similar patterns across files before adding new code
- Consolidate repeated error handling, validation, or utility patterns

### Self-Documenting Code
- Prefer descriptive names; code should explain itself
- Comments only for "why", not "what"
- Avoid explaining obvious logic with comments

### Refactoring Protocol
- **INCREMENTAL** – Extract one logical unit at a time
- **BUILD FIRST** – Ensure code compiles after each change
- **PRACTICAL** – Balance validation with efficiency
- **MAINTAIN FUNCTIONALITY** – Refactor without changing behavior
- **MINIMIZE IMPACT** – Do the minimum necessary

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

## Zsh Command Detection

By default (when using zsh), ledit detects zsh commands before sending them to the AI. This can be disabled by setting `enable_zsh_command_detection: false` in the config. Commands are auto-executed by default, which can be changed with `auto_execute_detected_commands: false`.

**Implementation:**
- `pkg/zsh/command.go`: Core zsh command detection logic
  - `IsCommand(input)`: Checks if input starts with a valid zsh command
  - Queries zsh's command tables: `${commands}`, `${builtins}`, `${aliases}`, `${functions}`
  - Returns command type and metadata (path, alias value, etc.)

**Integration Flow:**
- `cmd/agent_execution.go`: `tryZshCommandExecution()` called before `tryDirectExecution()`
- Only active when `$SHELL` contains "zsh" and config flag is not explicitly disabled
- Auto-executes by default (configurable via `auto_execute_detected_commands`)
- Shows confirmation prompt only if auto-execute is disabled
- `!` prefix always forces auto-execution (overrides config)
- Falls back to LLM-based detection if zsh detection fails or user declines

**Configuration:**
- `enable_zsh_command_detection`: Enable/disable feature (default: `true`)
- `auto_execute_detected_commands`: Auto-execute without prompting (default: `true`)

**Key Design Points:**
- Respects existing `!` prefix for auto-execution (compatibility with slash commands)
- Enabled by default when using zsh (can be disabled in config)
- Auto-execution enabled by default for smoother UX
- Gracefully falls back if zsh is not available or command is unclear
- All command types supported: external, builtin, alias, function
