# Agent Template Examples

## Core Configurations

### Code Assistant
```yaml
agent:
  name: "Code Assistant"
  goals: ["Write/improve code", "Fix bugs", "Review quality"]
  tools: ["read_file", "edit_file", "run_tests"]
  max_iterations: 15

llm:
  model: "gpt-4"
  temperature: 0.3

workflows: ["code_review", "bug_fix", "refactoring"]
```

### Documentation Specialist  
```yaml
agent:
  name: "Documentation Specialist"
  goals: ["Generate docs", "Update README", "Create tutorials"]
  tools: ["read_file", "edit_file", "web_search"]
  require_approval: true

llm:
  model: "gemini-1.5-pro"
  temperature: 0.5
```

### Security Analyst
```yaml
agent:
  name: "Security Analyst" 
  goals: ["Identify vulnerabilities", "Security review"]
  tools: ["read_file", "run_security_scan"]
  require_approval: true

llm:
  model: "claude-3-sonnet"
  temperature: 0.1
```

### DevOps Assistant
```yaml
agent:
  name: "DevOps Assistant"
  goals: ["Automate deployment", "Configure CI/CD"]
  tools: ["run_shell", "run_docker", "run_kubernetes"]
  max_iterations: 20
  require_approval: true
```

### Testing Specialist
```yaml  
agent:
  name: "Testing Specialist"
  goals: ["Write test suites", "Improve coverage"]
  tools: ["run_tests", "analyze_code"]
  max_iterations: 10
```

## Usage Examples

```bash
# Code tasks
./agent "Review code in src/main.py"
./agent "Fix login bug"

# Documentation  
./agent "Generate API docs"
./agent "Update README"

# Security
./agent "Security audit of auth system"
./agent "Check SQL injection vulnerabilities"

# DevOps
./agent "Setup GitHub Actions CI"
./agent "Create Dockerfile"

# Testing
./agent "Write unit tests for auth module"
./agent "Debug failing test"
```

## Environment Configs

### Development
```yaml
environment:
  debug: true
  log_level: "debug"
llm:
  model: "gpt-4o-mini"
  temperature: 0.8
```

### Production  
```yaml
environment:
  debug: false
  log_level: "warn"
llm:
  model: "gpt-4"
  temperature: 0.3
```

## Best Practices

- **Security**: Never store secrets in configs, require approval for dangerous ops
- **Performance**: Use appropriate models, set timeouts, enable caching  
- **Organization**: Separate concerns, use env vars, document thoroughly