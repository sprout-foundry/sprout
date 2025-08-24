# Agent Configuration Design

This document outlines the configuration patterns and best practices for creating configurable agent templates.

## Configuration Principles

### 1. Simple First, Powerful When Needed

**Guiding Principles:**
- Start with simple YAML/JSON configuration
- Allow progressive complexity
- Make common configurations trivial
- Keep advanced features accessible but not required

### 2. Environment-Driven Configuration

**Pattern:**
```yaml
# config.yaml
agent:
  name: "Code Assistant"
  goals:
    - "Help developers write better code"
    - "Fix bugs and issues"
  tools:
    - "read_file"
    - "edit_file"
    - "run_tests"

llm:
  provider: "openai"
  model: "gpt-4"
  temperature: 0.7

environment:
  max_tokens: 4096
  timeout: 30s
```

**Environment Variables:**
```bash
export AGENT_CONFIG_FILE=config.yaml
export OPENAI_API_KEY=sk-...
export AGENT_DEBUG=true
export AGENT_LOG_LEVEL=info
```

### 3. Configuration Hierarchy

**Levels (in order of precedence):**
1. Command-line flags (highest priority)
2. Environment variables
3. Configuration file
4. Default values (lowest priority)

## Core Configuration Domains

### 1. Agent Configuration

**Essential Fields:**
```yaml
agent:
  name: string                    # Agent identifier
  description: string            # What this agent does
  goals: []string                # Agent's objectives
  capabilities: []string         # What the agent can do
  tools: []string               # Available tools
  workflows: []string           # Available workflow types

  # Behavior settings
  max_iterations: int           # Max execution steps (default: 10)
  timeout: duration             # Max execution time (default: 5m)
  interactive: bool             # Allow user interaction (default: true)

  # Safety settings
  require_approval: bool        # Require user approval for changes (default: false)
  dry_run: bool                 # Test mode, no actual changes (default: false)
  allowed_paths: []string       # Restrict file system access
```

### 2. Tool Configuration

**Tool Definitions:**
```yaml
tools:
  # Built-in tools
  read_file:
    enabled: true
    max_file_size: "1MB"
    allowed_extensions: [".py", ".js", ".go", ".md"]

  edit_file:
    enabled: true
    backup: true
    validate_syntax: true

  run_shell:
    enabled: true
    allowed_commands: ["git", "npm", "go", "python"]
    timeout: "30s"
    require_approval: true

  # Custom tools can be defined here
  custom_tool:
    enabled: false
    config:
      api_endpoint: "https://api.example.com"
      api_key: "${CUSTOM_API_KEY}"
```

### 3. LLM Configuration

**Provider Configuration:**
```yaml
llm:
  # Provider selection
  provider: "openai" | "anthropic" | "gemini" | "ollama"
  model: string

  # Generation parameters
  temperature: float              # 0.0 - 1.0
  max_tokens: int
  top_p: float
  presence_penalty: float
  frequency_penalty: float

  # Provider-specific settings
  openai:
    api_key: "${OPENAI_API_KEY}"
    base_url: "https://api.openai.com/v1"
    organization: string

  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"
    base_url: "https://api.anthropic.com"
    max_tokens: int

  gemini:
    api_key: "${GEMINI_API_KEY}"
    safety_settings:
      - category: "HARM_CATEGORY_HATE_SPEECH"
        threshold: "BLOCK_MEDIUM_AND_ABOVE"

  ollama:
    base_url: "http://localhost:11434"
    model: string
    keep_alive: "5m"
```

### 4. Workflow Configuration

**Workflow Definitions:**
```yaml
workflows:
  code_review:
    name: "Code Review"
    description: "Review code for issues and improvements"
    steps:
      - analyze_code
      - check_style
      - suggest_improvements
    tools: ["read_file", "run_linter"]

  bug_fix:
    name: "Bug Fix"
    description: "Identify and fix bugs"
    steps:
      - reproduce_issue
      - analyze_problem
      - implement_fix
      - test_fix
    tools: ["read_file", "edit_file", "run_tests"]

  documentation:
    name: "Documentation"
    description: "Generate or update documentation"
    steps:
      - analyze_code
      - generate_docs
      - update_readme
    tools: ["read_file", "edit_file", "web_search"]
```

### 5. Environment Configuration

**Runtime Environment:**
```yaml
environment:
  # Resource limits
  max_memory: "1GB"
  max_cpu: "2.0"
  timeout: "5m"

  # File system
  workspace_root: "."
  allowed_paths: ["./src", "./tests", "./docs"]
  ignore_patterns: ["*.log", "node_modules/**", ".git/**"]

  # Logging
  log_level: "info"
  log_file: "agent.log"
  structured_logging: false

  # Caching
  cache_dir: ".agent/cache"
  cache_ttl: "1h"
  enable_cache: true
```

## Configuration Examples

### 1. Minimal Configuration

For simple use cases:
```yaml
# minimal.yaml
agent:
  name: "Simple Assistant"
  goals: ["Help with coding tasks"]
  tools: ["read_file", "edit_file"]

llm:
  provider: "openai"
  model: "gpt-4o-mini"
  temperature: 0.7
```

### 2. Development Assistant

For comprehensive development support:
```yaml
# dev-assistant.yaml
agent:
  name: "Development Assistant"
  description: "Comprehensive development support"
  goals:
    - "Write and improve code"
    - "Fix bugs and issues"
    - "Review code quality"
    - "Generate documentation"
    - "Run tests and validation"

  tools:
    - "read_file"
    - "edit_file"
    - "run_shell"
    - "run_tests"
    - "web_search"
    - "analyze_code"

  max_iterations: 15
  timeout: "10m"

llm:
  provider: "openai"
  model: "gpt-4"
  temperature: 0.3
  max_tokens: 4096

workflows:
  - code_review
  - bug_fix
  - feature_development
  - documentation

environment:
  allowed_paths: ["./src", "./tests", "./docs"]
  ignore_patterns: ["*.log", "node_modules/**", ".git/**"]
  log_level: "debug"
```

### 3. Security-Focused Agent

For security analysis:
```yaml
# security-agent.yaml
agent:
  name: "Security Analyst"
  description: "Security vulnerability analysis and remediation"
  goals:
    - "Identify security vulnerabilities"
    - "Suggest security improvements"
    - "Review security configurations"

  tools:
    - "read_file"
    - "run_security_scan"
    - "analyze_dependencies"
    - "check_permissions"

  require_approval: true
  allowed_paths: ["./src", "./config"]

llm:
  provider: "anthropic"
  model: "claude-3-sonnet-20240229"
  temperature: 0.1  # Lower temperature for security analysis

workflows:
  - security_audit
  - vulnerability_assessment
  - dependency_check
```

### 4. Documentation Agent

For documentation tasks:
```yaml
# docs-agent.yaml
agent:
  name: "Documentation Specialist"
  description: "Documentation generation and maintenance"
  goals:
    - "Generate API documentation"
    - "Update README files"
    - "Create tutorials and guides"
    - "Maintain changelog"

  tools:
    - "read_file"
    - "edit_file"
    - "analyze_code"
    - "web_search"

llm:
  provider: "gemini"
  model: "gemini-1.5-pro"
  temperature: 0.5

workflows:
  - api_documentation
  - readme_generation
  - tutorial_creation
```

## Configuration Loading Strategy

### 1. Configuration Loader

```go
type ConfigLoader struct {
    configPaths []string
    envPrefix   string
    validators  []ConfigValidator
}

func (cl *ConfigLoader) Load() (*AgentConfig, error) {
    config := &AgentConfig{}

    // Load default configuration
    cl.loadDefaults(config)

    // Load from configuration files
    for _, path := range cl.configPaths {
        if fileConfig, err := cl.loadFromFile(path); err == nil {
            cl.mergeConfigs(config, fileConfig)
        }
    }

    // Override with environment variables
    cl.loadFromEnv(config)

    // Override with command-line flags
    cl.loadFromFlags(config)

    // Validate configuration
    return cl.validate(config)
}
```

### 2. Configuration Validation

```go
type ConfigValidator interface {
    Validate(config *AgentConfig) []ValidationError
}

type ValidationError struct {
    Field   string
    Message string
    Code    string
}

func (c *AgentConfig) Validate() []ValidationError {
    var errors []ValidationError

    // Agent validation
    if c.Agent.Name == "" {
        errors = append(errors, ValidationError{
            Field:   "agent.name",
            Message: "Agent name is required",
            Code:    "REQUIRED",
        })
    }

    // LLM validation
    if c.LLM.Provider == "" {
        errors = append(errors, ValidationError{
            Field:   "llm.provider",
            Message: "LLM provider is required",
            Code:    "REQUIRED",
        })
    }

    // Tool validation
    for _, tool := range c.Tools {
        if !isValidTool(tool) {
            errors = append(errors, ValidationError{
                Field:   fmt.Sprintf("tools.%s", tool),
                Message: fmt.Sprintf("Unknown tool: %s", tool),
                Code:    "INVALID_TOOL",
            })
        }
    }

    return errors
}
```

### 3. Configuration Templates

Provide configuration templates for common use cases:

```yaml
# templates/code-assistant.yaml
agent:
  name: "${AGENT_NAME}"
  goals:
    - "Write and improve code"
    - "Fix bugs and issues"
  tools:
    - "read_file"
    - "edit_file"
    - "run_shell"

llm:
  provider: "${LLM_PROVIDER}"
  model: "${LLM_MODEL}"
  temperature: 0.7
```

## Best Practices

### 1. Configuration Design

**Do:**
- Use clear, descriptive field names
- Provide sensible defaults
- Support environment variable substitution
- Include helpful comments and examples
- Validate configuration early
- Support configuration inheritance

**Avoid:**
- Overly complex nested structures
- Required fields without good defaults
- Hard-coded values
- Inconsistent naming conventions
- Missing validation

### 2. Environment Variables

**Naming Convention:**
```bash
# Use consistent prefixes
AGENT_NAME="MyAgent"
AGENT_DEBUG="true"

# Provider-specific
OPENAI_API_KEY="sk-..."
ANTHROPIC_API_KEY="sk-ant-..."

# Configuration paths
AGENT_CONFIG_FILE="config.yaml"
AGENT_WORKSPACE_ROOT="."
```

### 3. Configuration Discovery

**Search Order:**
1. `./agent.yaml` or `./agent.yml`
2. `./config/agent.yaml`
3. `~/.agent/config.yaml`
4. `/etc/agent/config.yaml`
5. Environment variable: `AGENT_CONFIG_FILE`
6. Command-line flag: `--config`

### 4. Configuration Security

**Secure Practices:**
- Never store API keys in configuration files
- Use environment variables for secrets
- Support secure credential stores
- Validate file permissions
- Encrypt sensitive configuration

This configuration design provides a flexible, secure, and user-friendly way to configure agent templates while maintaining simplicity for common use cases and power for advanced scenarios.
