# Files Longer Than 600 Lines - Refactoring Candidates

This file lists all source code files in the project that exceed 600 lines, which are candidates for refactoring to improve maintainability.

## Files to Refactor

### Console Components
- **2212 lines**: `./pkg/console/components/agent_console.go` - Very large UI component, likely handling multiple responsibilities
- **1231 lines**: `./pkg/console/components/input_manager.go` - Complex input handling logic
- **894 lines**: `./pkg/console/components/footer.go` - Large footer component
- **712 lines**: `./pkg/console/components/terminal_ui_test.go` - Extensive test file
- **685 lines**: `./pkg/console/components/input_test.go` - Large test suite
- **683 lines**: `./pkg/console/components/streaming_formatter.go` - Complex formatting logic

### Workspace Management
- **1552 lines**: `./pkg/workspace/workspace_manager.go` - Core workspace logic, likely handles multiple responsibilities
- **722 lines**: `./pkg/workspace/workspace_context.go` - Context management

### Agent Core
- **1059 lines**: `./pkg/agent/agent.go` - Main agent logic, potentially handling too many concerns
- **979 lines**: `./pkg/agent/tool_registry.go` - Tool registration and management
- **760 lines**: `./pkg/agent/fallback_parser.go` - Complex parsing logic
- **743 lines**: `./pkg/agent/conversation_handler.go` - Conversation management

### Code Review
- **973 lines**: `./pkg/codereview/service.go` - Code review service logic

### Console Core
- **957 lines**: `./pkg/console/terminal_controller.go` - Terminal control logic

### Provider Integrations
- **858 lines**: `./pkg/agent_providers/openrouter.go` - OpenRouter provider implementation
- **738 lines**: `./pkg/agent_providers/deepinfra.go` - DeepInfra provider implementation

### Command-Line Interface
- **852 lines**: `./cmd/mcp.go` - MCP command implementation

### Agent Tools
- **803 lines**: `./pkg/agent_tools/vision.go` - Vision tool implementation

### Agent Commands
- **784 lines**: `./pkg/agent_commands/models.go` - Command models
- **723 lines**: `./pkg/agent_commands/commit.go` - Commit command logic

### API Integrations
- **683 lines**: `./pkg/agent_api/openai.go` - OpenAI API integration
- **618 lines**: `./pkg/agent_api/ollama_local.go` - Ollama local API integration

## Refactoring Priority

### High Priority (1000+ lines)
1. `./pkg/console/components/agent_console.go` (2212 lines) - Critical UI component
2. `./pkg/workspace/workspace_manager.go` (1552 lines) - Core workspace logic
3. `./pkg/console/components/input_manager.go` (1231 lines) - Important input handling
4. `./pkg/agent/agent.go` (1059 lines) - Main agent logic

### Medium Priority (800-1000 lines)
5. `./pkg/agent/tool_registry.go` (979 lines) - Tool management
6. `./pkg/codereview/service.go` (973 lines) - Code review service
7. `./pkg/console/terminal_controller.go` (957 lines) - Terminal control
8. `./pkg/agent_providers/openrouter.go` (858 lines) - Provider integration
9. `./cmd/mcp.go` (852 lines) - CLI command
10. `./pkg/agent_tools/vision.go` (803 lines) - Vision tool

### Lower Priority (600-800 lines)
11. `./pkg/agent_commands/models.go` (784 lines)
12. `./pkg/agent/fallback_parser.go` (760 lines)
13. `./pkg/agent/conversation_handler.go` (743 lines)
14. `./pkg/agent_providers/deepinfra.go` (738 lines)
15. `./pkg/agent_commands/commit.go` (723 lines)
16. `./pkg/workspace/workspace_context.go` (722 lines)
17. `./pkg/console/components/terminal_ui_test.go` (712 lines)
18. `./pkg/console/components/input_test.go` (685 lines)
19. `./pkg/console/components/streaming_formatter.go` (683 lines)
20. `./pkg/agent_api/openai.go` (683 lines)
21. `./pkg/console/components/footer.go` (894 lines)
22. `./pkg/agent_api/ollama_local.go` (618 lines)

## Refactoring Suggestions

For each file, consider:
- Breaking into smaller, focused files by responsibility
- Extracting helper functions or utility classes
- Applying SOLID principles to separate concerns
- Creating interfaces for better testability
- Grouping related functionality into packages

## Notes
- Shell output files (`.ledit/shell_outputs/`) are excluded from refactoring consideration as they are generated files
- Test files should be refactored alongside their corresponding source files
- Focus on files that contain business logic rather than generated or configuration files