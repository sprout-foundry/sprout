# Tool Suggestions for Ledit

This document outlines useful tools that would enhance ledit's coding agent capabilities, comparing them to Claude Code's toolset and recommending implementation approaches.

## Comparison with Claude Code Tools

### Claude Code Tools (from Anthropic documentation)
- **bash-tool** - Execute shell commands
- **code-execution-tool** - Execute code in sandboxed environment
- **computer-use-tool** - Virtual computer control (UI automation)
- **text-editor-tool** - Edit text files
- **web-fetch-tool** - Fetch web content
- **web-search-tool** - Search web
- **memory-tool** - Persistent memory for sessions
- **tool-search-tool** - Search for available tools

### Ledit Current Tools
- `shell_command` - Terminal commands ✓ (similar to bash-tool)
- `read_file`, `write_file`, `edit_file` - File operations ✓ (similar to text-editor-tool)
- `search_files` - File content search ✓
- `web_search`, `fetch_url` - Web capabilities ✓
- `analyze_ui_screenshot`, `analyze_image_content` - Vision capabilities ✓
- `add_todo`, `add_todos`, `update_todo_status`, `list_todos`, `get_active_todos_compact`, `archive_completed` - Task management ✓
- `validate_build` - Build validation ✓
- `view_history`, `rollback_changes` - History/rollback capability ✓
- `mcp_tools` - MCP server management ✓ (similar to tool-search-tool but more extensible)

---

## Suggested Tools for Ledit

### File System & Navigation

| Tool | Description | Priority | Implementation |
|------|-------------|----------|-----------------|
| `list_files` | List files in directory with tree structure/filters | High | Native Tool |
| `get_file_info` | Get file metadata (size, type, permissions, modified time) | Medium | Native Tool |
| `find_file_by_name` | Find files by name pattern (not content) | Medium | Native Tool |
| `list_directory` | List directory contents recursively with depth control | High | Native Tool |
| `copy_file` | Copy files/directories | Low | Native Tool |
| `move_file` / `rename_file` | Move/rename files | Low | Native Tool |
| `delete_file` | Delete files or directories safely | Medium | Native Tool |

### Code Analysis & Navigation

| Tool | Description | Priority | Implementation |
|------|-------------|----------|-----------------|
| `analyze_code_structure` | Extract functions, classes, imports from code files | High | Native Tool |
| `find_references` | Find where a symbol/identifier is used | High | Native Tool |
| `extract_symbols` | List all function/class definitions in a file | High | Native Tool |
| `analyze_dependencies` | Show import dependency graph | Medium | Native Tool |
| `detect_language` | Auto-detect programming language of a file | Low | Native Tool |

### Testing & Validation

| Tool | Description | Priority | Implementation |
|------|-------------|----------|-----------------|
| `run_tests` | Run tests with filtering (single test, package, all) | High | Native Tool |
| `get_test_coverage` | Get code coverage report | Medium | Native Tool |
| `find_test_for_file` | Locate test files for a source file | Medium | Native Tool |
| `generate_test` | Generate unit test stubs for a function/class | Medium | Native Tool |
| `check_linting` | Run linters (eslint, flake8, pylint, golangci-lint) | Low | MCP Server |

### Git Operations

| Tool | Description | Priority | Implementation |
|------|-------------|----------|-----------------|
| `get_git_status` | Check git status (changed files, branch, etc.) | High | Native Tool |
| `get_diff` | Get diff between files/commits | High | Native Tool |
| `create_branch` | Create and switch to new git branch | Medium | Native Tool |
| `commit_changes` | Stage and commit changes | Medium | Native Tool |
| `get_commit_history` | View commit history | Medium | Native Tool |
| `git_blame` | Get last modification info for lines | Low | Native Tool |

**Note**: Per ledit's policy, committing/pushing should remain under user control, but tool assistance for staging and committing (without push) can be helpful.

### Documentation & Comments

| Tool | Description | Priority | Implementation |
|------|-------------|----------|-----------------|
| `extract_comments` | Extract all comments/todos from code | Low | Native Tool |
| `generate_docstring` | Generate docstrings for functions | Medium | Native Tool |
| `search_documentation` | Search in language/framework documentation | Low | MCP Server |

### Refactoring Tools

| Tool | Description | Priority | Implementation |
|------|-------------|----------|-----------------|
| `extract_function` | Extract selected code into a function | High | Native Tool |
| `inline_function` | Inline a function call | Medium | Native Tool |
| `rename_symbol` | Rename symbol across files | High | Native Tool |
| `move_symbol` | Move function/class to another file | Medium | Native Tool |

### Package/Dependency Management

| Tool | Description | Priority | Implementation |
|------|-------------|----------|-----------------|
| `list_dependencies` | List project dependencies (package.json, go.mod, requirements.txt, etc.) | High | Native Tool |
| `add_dependency` | Add a new dependency | Medium | Native Tool |
| `update_dependencies` | Update dependencies | Medium | Native Tool |
| `check_vulnerabilities` | Check for security vulnerabilities in dependencies | High | MCP Server |

### Database/API Tools

| Tool | Description | Priority | Implementation |
|------|-------------|----------|-----------------|
| `execute_sql` | Execute SQL queries on databases | Low | MCP Server |
| `test_endpoint` | Test HTTP/API endpoints | Low | MCP Server |
| `analyze_api_schema` | Analyze OpenAPI/Swagger specs | Low | MCP Server |

### Performance & Profiling

| Tool | Description | Priority | Implementation |
|------|-------------|----------|-----------------|
| `benchmark_code` | Run benchmarking on code | Low | MCP Server |
| `profile_performance` | Profile code for bottlenecks | Low | MCP Server |
| `analyze_code_complexity` | Calculate cyclomatic complexity | Medium | MCP Server |

### Project Templates & Scaffolding

| Tool | Description | Priority | Implementation |
|------|-------------|----------|-----------------|
| `create_project_template` | Create project from templates | Low | MCP Server |
| `generate_boilerplate` | Generate boilerplate code for common patterns | Low | MCP Server |
| `init_project` | Initialize new project structure | Medium | MCP Server |

### Configuration Management

| Tool | Description | Priority | Implementation |
|------|-------------|----------|-----------------|
| `read_config` | Parse and read config files (JSON, YAML, TOML, etc.) | Medium | Native Tool |
| `validate_config` | Validate configuration files | Medium | Native Tool |
| `merge_configs` | Merge multiple config files | Low | Native Tool |

### Security

| Tool | Description | Priority | Implementation |
|------|-------------|----------|-----------------|
| `scan_secrets` | Scan code for hardcoded secrets/credentials | High | MCP Server |
| `analyze_security_issues` | Find common security vulnerabilities | High | MCP Server |

### Code Formatting & Style

| Tool | Description | Priority | Implementation |
|------|-------------|----------|-----------------|
| `format_code` | Format code (prettier, black, gofmt, etc.) | Medium | Native Tool (with wrappers for language-specific formatters) |
| `check_code_style` | Check code style compliance | Low | MCP Server |

### Terminal & Interaction

| Tool | Description | Priority | Implementation |
|------|-------------|----------|-----------------|
| `pause_for_confirmation` | Pause and wait for user confirmation | Medium | Native Tool |
| `select_option` | Present options to user and get selection | Low | Native Tool |

### Code Generation

| Tool | Description | Priority | Implementation |
|------|-------------|----------|-----------------|
| `generate_stub` | Generate interface implementation stubs | Medium | Native Tool |
| `generate_type_definitions` | Generate types from JSON schema or examples | Low | Native Tool |

---

## Implementation Strategy: Native Tool vs MCP Server

### Native Tools (~15 recommended)

**Characteristics:**
- Core, language-agnostic, frequently used
- Fast, no external dependencies
- Tightly integrated with ledit's workflow

**Recommended Native Tools:**
1. **Code execution** (sandboxed Python/JS/other language snippets)
2. **Code structure analysis** + symbol lookup
3. **Test runner** (including coverage)
4. **File/directory listings and metadata**
5. **Git operations** (status, diff, history - common workflow tools)
6. **Code formatting** (format_code) wrapper for language-specific formatters
7. **Memory** (localStorage or simple file store for cross-session context)

**Rationale:** These are used frequently, need tight integration, and are language-agnostic or have simple language-specific variants.

### MCP Servers (~20-25 recommended)

**Characteristics:**
- Domain-specific, optional, complex
- External dependencies available
- Can be maintained independently

**Recommended MCP Servers:**
1. **Linting** - Toolchain-specific (eslint, pylint, pyright, etc.)
2. **Security scanning** - gitleaks, bandit, etc.
3. **API/database interaction** - External services, database drivers
4. **Remote testing/benchmarking** - CI/CD integration, performance testing
5. **Documentation search** - Language/framework docs
6. **Package vulnerability scanning** - Snyk, npm audit, etc.

**Rationale:** These are domain-specific, have external dependencies, and benefit from being maintained independently. Users can enable/disable based on their needs.

---

## Phased Implementation Plan

### Phase 1: High-Priority Native Tools (Immediate Value)
1. `run_tests` - Test filtering and execution
2. `get_git_status` / `get_diff` - Git workflow support
3. `analyze_code_structure` - Symbol extraction
4. `find_references` - Cross-reference analysis
5. `list_directory` - Better navigation

### Phase 2: Medium-Priority Native Tools
1. `extract_symbols` - Function/class listing
2. `format_code` - Code formatting
3. `list_dependencies` - Package management basics
4. `validate_config` - Config handling
5. `generate_test` - Test stub generation

### Phase 3: MCP Server Ecosystem
1. Create example MCP server for linting
2. Create example MCP server for security scanning
3. Create example MCP server for API testing
4. Documentation for building custom MCP servers

### Phase 4: Advanced Features (Low Priority)
1. `extract_function` / `inline_function` - Refactoring
2. `rename_symbol` / `move_symbol` - Advanced refactoring
3. `benchmark_code` / `profile_performance` - Performance tools
4. Computer-use integration - UI automation

---

## Tool Parameter Standards

All new tools should follow these parameter conventions:

```go
type ParameterConfig struct {
    Name         string   `json:"name"`
    Type         string   `json:"type"` // "string", "int", "float64", "bool", "array"
    Required     bool     `json:"required"`
    Alternatives []string `json:"alternatives"` // Alternative parameter names for backward compatibility
    Description  string   `json:"description"`
}
```

Example:
```json
{
  "name": "run_tests",
  "type": "tool",
  "description": "Run tests with optional filtering",
  "parameters": [
    {
      "name": "filter",
      "type": "string",
      "required": false,
      "alternatives": ["test_name", "pattern"],
      "description": "Specific test to run (function name, file path, or pattern)"
    },
    {
      "name": "coverage",
      "type": "bool",
      "required": false,
      "description": "Include coverage report"
    }
  ]
}
```

---

## MCP Server Examples

### Example 1: Linting MCP Server

```go
// mcp_linter/server.go
func (s *LinterServer) RunLint(ctx context.Context, path string) (string, error) {
    ext := filepath.Ext(path)
    switch ext {
    case ".js", ".ts":
        return runCommand(ctx, "eslint", path)
    case ".go":
        return runCommand(ctx, "golangci-lint", "run", path)
    // ... other languages
    }
}
```

### Example 2: Security Scanning MCP Server

```go
// mcp_security/server.go
func (s *SecurityServer) ScanSecrets(ctx context.Context, repoPath string) (ScanResult, error) {
    return runCommand(ctx, "gitleaks", "detect", "--source", repoPath, "--no-update")
}

func (s *SecurityServer) CheckVulnerabilities(ctx context.Context, deps []string) (VulnResult, error) {
    // Use npm audit, go mod tidy, etc.
}
```

---

## Testing Guidelines

For each new tool, ensure:

1. **Unit tests** for core logic (in `*_test.go`)
2. **Integration tests** for operations on real files when applicable
3. **Error handling** with clear error messages
4. **Parameter validation** with helpful suggestions
5. **Tool logs** for user visibility (via `a.ToolLog()`)

Example test structure:
```go
func TestRunTests(t *testing.T) {
    tests := []struct {
        name     string
        filter   string
        expected int
    }{
        {"run all tests", "", 0},
        {"run specific test", "TestExample", 0},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

---

## Summary

This comprehensive toolset would make ledit significantly more powerful for:

- **Code analysis and navigation** (structure, symbols, references)
- **Testing workflows** (run, coverage, stub generation)
- **Git integration** (status, diff, history)
- **Refactoring support** (extract, inline, rename, move)
- **Package management** (dependencies, vulnerabilities)
- **Code quality** (formatting, linting, complexity)
- **Security** (secret scanning, vulnerability checking)

The split approximately 15-20 native tools and 20-25 MCP servers provides a balanced approach: core capabilities remain fast and reliable while extensibility through MCP allows domain-specific features without bloating the core.
