# Agent Template Examples

This directory contains example configurations and use cases for the templatized agent system.

## Example Configurations

### 1. Code Assistant (`code-assistant.yaml`)

**Use Case**: General code assistance and development support
**Focus**: Writing, reviewing, and improving code

```yaml
agent:
  name: "Code Assistant"
  description: "AI-powered coding assistant"
  goals:
    - "Write and improve code"
    - "Fix bugs and issues"
    - "Review code quality"
    - "Explain complex concepts"
    - "Suggest best practices"

  tools:
    - "read_file"
    - "edit_file"
    - "run_shell"
    - "run_tests"
    - "analyze_code"

  max_iterations: 15
  timeout: "10m"
  require_approval: false

llm:
  provider: "openai"
  model: "gpt-4"
  temperature: 0.3
  max_tokens: 4096

workflows:
  - code_review
  - bug_fix
  - feature_development
  - refactoring

environment:
  allowed_paths: ["./src", "./tests", "./docs"]
  ignore_patterns: ["*.log", "node_modules/**", ".git/**"]
  log_level: "info"
```

**Usage Examples:**
```bash
# Code review
./agent "Review the code in src/main.py"

# Bug fixing
./agent "Fix the bug where users can't login"

# Feature development
./agent "Add a new endpoint for user registration"

# Code explanation
./agent "Explain how the authentication system works"
```

### 2. Documentation Specialist (`docs-specialist.yaml`)

**Use Case**: Documentation generation and maintenance
**Focus**: Creating and updating documentation

```yaml
agent:
  name: "Documentation Specialist"
  description: "Documentation generation and maintenance"
  goals:
    - "Generate comprehensive documentation"
    - "Update README and API docs"
    - "Create tutorials and guides"
    - "Maintain changelog"
    - "Ensure documentation accuracy"

  tools:
    - "read_file"
    - "edit_file"
    - "analyze_code"
    - "web_search"
    - "validate_docs"

  max_iterations: 8
  timeout: "8m"
  require_approval: true

llm:
  provider: "gemini"
  model: "gemini-1.5-pro"
  temperature: 0.5
  max_tokens: 8192

workflows:
  - api_documentation
  - readme_generation
  - tutorial_creation
  - changelog_update

environment:
  allowed_paths: ["./docs", "./src", "./README.md"]
  ignore_patterns: ["*.log", "build/**", ".git/**"]
  log_level: "info"
```

**Usage Examples:**
```bash
# API documentation
./agent "Generate API documentation for the user service"

# README updates
./agent "Update the README with the new features"

# Tutorial creation
./agent "Create a tutorial for getting started with the project"

# Changelog maintenance
./agent "Update the changelog for version 2.1.0"
```

### 3. Security Analyst (`security-analyst.yaml`)

**Use Case**: Security vulnerability analysis and remediation
**Focus**: Identifying and fixing security issues

```yaml
agent:
  name: "Security Analyst"
  description: "Security vulnerability analysis and remediation"
  goals:
    - "Identify security vulnerabilities"
    - "Review code for security issues"
    - "Suggest security improvements"
    - "Analyze dependencies for vulnerabilities"
    - "Review security configurations"

  tools:
    - "read_file"
    - "run_security_scan"
    - "analyze_dependencies"
    - "check_permissions"
    - "web_search"

  max_iterations: 12
  timeout: "15m"
  require_approval: true  # Security changes need approval
  allowed_paths: ["./src", "./config", "./docker"]

llm:
  provider: "anthropic"
  model: "claude-3-sonnet-20240229"
  temperature: 0.1  # Lower temperature for security analysis
  max_tokens: 6144

workflows:
  - security_audit
  - vulnerability_assessment
  - dependency_check
  - configuration_review

environment:
  allowed_paths: ["./src", "./config", "./docker", "./kubernetes"]
  ignore_patterns: ["*.log", "test_data/**", ".git/**"]
  log_level: "debug"
  structured_logging: true
```

**Usage Examples:**
```bash
# Security audit
./agent "Perform a security audit of the authentication system"

# Vulnerability assessment
./agent "Check for SQL injection vulnerabilities in the user module"

# Dependency analysis
./agent "Analyze dependencies for known security vulnerabilities"

# Configuration review
./agent "Review the security configuration of the application"
```

### 4. DevOps Assistant (`devops-assistant.yaml`)

**Use Case**: Infrastructure and deployment automation
**Focus**: CI/CD, infrastructure, and deployment tasks

```yaml
agent:
  name: "DevOps Assistant"
  description: "Infrastructure and deployment automation"
  goals:
    - "Automate deployment processes"
    - "Configure CI/CD pipelines"
    - "Manage infrastructure as code"
    - "Monitor system health"
    - "Optimize performance"

  tools:
    - "read_file"
    - "edit_file"
    - "run_shell"
    - "run_docker"
    - "run_kubernetes"
    - "web_search"

  max_iterations: 20
  timeout: "20m"
  require_approval: true  # Infrastructure changes need approval

llm:
  provider: "openai"
  model: "gpt-4-turbo"
  temperature: 0.2
  max_tokens: 8192

workflows:
  - ci_cd_setup
  - infrastructure_deployment
  - monitoring_setup
  - performance_optimization

environment:
  allowed_paths: ["./infrastructure", "./docker", "./kubernetes", "./.github"]
  ignore_patterns: ["*.log", "temp/**", ".git/**"]
  log_level: "info"
```

**Usage Examples:**
```bash
# CI/CD setup
./agent "Set up GitHub Actions for continuous integration"

# Infrastructure deployment
./agent "Deploy the application to AWS using Terraform"

# Docker configuration
./agent "Create a Dockerfile for the Python application"

# Kubernetes deployment
./agent "Create Kubernetes manifests for the microservices"
```

### 5. Testing Specialist (`testing-specialist.yaml`)

**Use Case**: Test automation and quality assurance
**Focus**: Writing and maintaining automated tests

```yaml
agent:
  name: "Testing Specialist"
  description: "Test automation and quality assurance"
  goals:
    - "Write comprehensive test suites"
    - "Implement automated testing"
    - "Improve test coverage"
    - "Debug failing tests"
    - "Set up testing frameworks"

  tools:
    - "read_file"
    - "edit_file"
    - "run_shell"
    - "run_tests"
    - "analyze_code"
    - "web_search"

  max_iterations: 10
  timeout: "12m"
  require_approval: false

llm:
  provider: "openai"
  model: "gpt-4o"
  temperature: 0.4
  max_tokens: 6144

workflows:
  - unit_test_creation
  - integration_test_setup
  - test_coverage_analysis
  - test_debugging

environment:
  allowed_paths: ["./src", "./tests", "./test_data"]
  ignore_patterns: ["*.log", "node_modules/**", ".git/**"]
  log_level: "info"
```

**Usage Examples:**
```bash
# Unit tests
./agent "Write unit tests for the user authentication module"

# Integration tests
./agent "Create integration tests for the API endpoints"

# Test coverage
./agent "Analyze and improve test coverage for the project"

# Test debugging
./agent "Debug the failing test in tests/test_user.py"
```

## Custom Tool Examples

### 1. Database Tool (`database-tool.yaml`)

**Purpose**: Database operations and schema management

```yaml
tools:
  database_query:
    enabled: true
    config:
      connection_string: "${DATABASE_URL}"
      allowed_operations: ["SELECT", "INSERT", "UPDATE", "DELETE"]
      require_approval: true

  schema_analysis:
    enabled: true
    config:
      supported_databases: ["postgresql", "mysql", "sqlite"]
      generate_migrations: true

agent:
  tools:
    - "database_query"
    - "schema_analysis"
```

**Usage:**
```bash
./agent "Analyze the database schema and suggest optimizations"
./agent "Create a migration to add user preferences table"
```

### 2. API Integration Tool (`api-integration.yaml`)

**Purpose**: External API integration and testing

```yaml
tools:
  api_call:
    enabled: true
    config:
      allowed_endpoints: ["https://api.github.com", "https://api.example.com"]
      auth_methods: ["bearer", "basic", "api_key"]
      timeout: "30s"

  api_testing:
    enabled: true
    config:
      test_framework: "pytest"
      generate_test_data: true

agent:
  tools:
    - "api_call"
    - "api_testing"
```

**Usage:**
```bash
./agent "Test the GitHub API integration"
./agent "Create tests for the external payment API"
```

## Workflow Examples

### 1. Code Review Workflow

```yaml
workflows:
  code_review:
    name: "Code Review"
    description: "Comprehensive code review process"
    steps:
      - name: "static_analysis"
        tool: "run_linter"
        config:
          rules: ["pylint", "flake8", "black"]
      - name: "security_check"
        tool: "security_scan"
        config:
          severity_threshold: "medium"
      - name: "performance_analysis"
        tool: "performance_check"
        config:
          complexity_threshold: 10
      - name: "documentation_review"
        tool: "doc_check"
        config:
          require_docstrings: true
```

### 2. Deployment Workflow

```yaml
workflows:
  deployment:
    name: "Application Deployment"
    description: "Automated deployment process"
    steps:
      - name: "build"
        tool: "run_shell"
        config:
          command: "docker build -t myapp ."
      - name: "test"
        tool: "run_tests"
        config:
          test_suite: "integration"
      - name: "security_scan"
        tool: "security_scan"
        config:
          scan_type: "container"
      - name: "deploy"
        tool: "run_kubernetes"
        config:
          namespace: "production"
          wait_for_ready: true
```

## Environment-Specific Configurations

### 1. Development Environment

```yaml
environment:
  name: "development"
  debug: true
  dry_run: true
  log_level: "debug"
  cache_enabled: false

llm:
  model: "gpt-4o-mini"  # Use cheaper model for development
  temperature: 0.8       # More creative responses during development
```

### 2. Production Environment

```yaml
environment:
  name: "production"
  debug: false
  dry_run: false
  log_level: "warn"
  cache_enabled: true

llm:
  model: "gpt-4"        # Use more capable model for production
  temperature: 0.3       # More consistent responses
  max_tokens: 8192      # Higher token limit for complex tasks
```

### 3. CI/CD Environment

```yaml
environment:
  name: "ci"
  debug: false
  dry_run: false
  log_level: "info"
  cache_enabled: false

llm:
  provider: "openai"
  model: "gpt-4o-mini"  # Balance cost and capability
  temperature: 0.1      # Very consistent for CI/CD tasks
```

## Best Practices

### 1. Configuration Organization

- **Separate concerns**: Keep agent, LLM, and environment config separate
- **Use environment variables**: Store secrets and environment-specific values in env vars
- **Provide defaults**: Always have sensible defaults for optional configuration
- **Document thoroughly**: Comment configuration files extensively

### 2. Security Considerations

- **Never store secrets in config files**: Use environment variables or secure stores
- **Limit tool permissions**: Only enable necessary tools for each agent type
- **Require approval for dangerous operations**: Database changes, deployments, etc.
- **Validate inputs**: Always validate user inputs and file paths

### 3. Performance Optimization

- **Choose appropriate models**: Use smaller models for simple tasks
- **Set reasonable timeouts**: Prevent hanging operations
- **Enable caching**: Cache LLM responses when appropriate
- **Limit iterations**: Prevent runaway agent execution

### 4. Tool Selection

- **Start minimal**: Begin with essential tools only
- **Add incrementally**: Add tools as needed for specific use cases
- **Test thoroughly**: Test each tool integration before production use
- **Monitor usage**: Track tool usage and performance

This collection of examples demonstrates how the templatized agent system can be configured for various use cases while maintaining consistency and best practices across different scenarios.
