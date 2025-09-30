package mcp

import (
	"fmt"
	"strings"
	"time"
)

// MCPServerTemplate represents a template for creating MCP servers
type MCPServerTemplate struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Type        string           `json:"type"`     // "stdio", "http"
	URL         string           `json:"url"`      // For HTTP servers
	Command     string           `json:"command"`  // For stdio servers
	Args        []string         `json:"args"`     // For stdio servers
	EnvVars     []EnvVarTemplate `json:"env_vars"` // Required environment variables
	Timeout     time.Duration    `json:"timeout"`
	Features    []string         `json:"features"`  // List of capabilities
	AuthType    string           `json:"auth_type"` // "bearer", "basic", "none"
	Docs        string           `json:"docs"`      // Documentation URL
}

// EnvVarTemplate represents a required environment variable
type EnvVarTemplate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret"`  // Should be masked in display
	Default     string `json:"default"` // Default value if any
}

// MCPServerRegistry holds templates for known MCP servers
type MCPServerRegistry struct {
	templates map[string]MCPServerTemplate
}

// NewMCPServerRegistry creates a new server registry with built-in templates
func NewMCPServerRegistry() *MCPServerRegistry {
	registry := &MCPServerRegistry{
		templates: make(map[string]MCPServerTemplate),
	}

	// Load built-in templates
	registry.loadBuiltinTemplates()
	return registry
}

// loadBuiltinTemplates loads the built-in server templates
func (r *MCPServerRegistry) loadBuiltinTemplates() {
	// GitHub MCP Server (Remote)
	r.templates["github-remote"] = MCPServerTemplate{
		ID:          "github-remote",
		Name:        "GitHub MCP Server (Remote)",
		Description: "Official GitHub MCP server for repository management, issues, PRs, and code analysis",
		Type:        "http",
		URL:         "https://api.githubcopilot.com/mcp/",
		EnvVars: []EnvVarTemplate{
			{
				Name:        "GITHUB_PERSONAL_ACCESS_TOKEN",
				Description: "GitHub Personal Access Token with repo, read:user, read:org, issues permissions",
				Required:    true,
				Secret:      true,
			},
		},
		Timeout:  30 * time.Second,
		Features: []string{"Repository management", "Issues & PRs", "GitHub Actions", "Code analysis", "Security findings"},
		AuthType: "bearer",
		Docs:     "https://github.com/github/github-mcp-server",
	}

	// GitHub MCP Server (Local Docker)
	r.templates["github-docker"] = MCPServerTemplate{
		ID:          "github-docker",
		Name:        "GitHub MCP Server (Docker)",
		Description: "Local Docker instance of GitHub MCP server",
		Type:        "stdio",
		Command:     "docker",
		Args:        []string{"run", "-i", "--rm", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN", "ghcr.io/github/github-mcp-server"},
		EnvVars: []EnvVarTemplate{
			{
				Name:        "GITHUB_PERSONAL_ACCESS_TOKEN",
				Description: "GitHub Personal Access Token",
				Required:    true,
				Secret:      true,
			},
		},
		Timeout:  30 * time.Second,
		Features: []string{"Repository management", "Issues & PRs", "GitHub Actions", "Code analysis"},
		AuthType: "none", // Handled via env var
		Docs:     "https://github.com/github/github-mcp-server",
	}

	// Git MCP Server (uvx)
	r.templates["git-uvx"] = MCPServerTemplate{
		ID:          "git-uvx",
		Name:        "Git MCP Server",
		Description: "Local Git operations (status, commit, diff, log, branch management)",
		Type:        "stdio",
		Command:     "uvx",
		Args:        []string{"mcp-server-git"},
		EnvVars:     []EnvVarTemplate{}, // No required env vars
		Timeout:     30 * time.Second,
		Features:    []string{"Git status", "Git commit", "Git diff", "Git log", "Branch management"},
		AuthType:    "none",
		Docs:        "https://github.com/modelcontextprotocol/servers/tree/main/src/git",
	}

	// Generic HTTP Server Template
	r.templates["http-generic"] = MCPServerTemplate{
		ID:          "http-generic",
		Name:        "Generic HTTP MCP Server",
		Description: "Custom HTTP-based MCP server",
		Type:        "http",
		URL:         "",                 // User will specify
		EnvVars:     []EnvVarTemplate{}, // User will specify
		Timeout:     30 * time.Second,
		Features:    []string{"Custom HTTP MCP functionality"},
		AuthType:    "bearer", // Most common
		Docs:        "https://modelcontextprotocol.io/",
	}

	// Generic stdio Server Template
	r.templates["stdio-generic"] = MCPServerTemplate{
		ID:          "stdio-generic",
		Name:        "Generic Command-line MCP Server",
		Description: "Custom command-line MCP server",
		Type:        "stdio",
		Command:     "",                 // User will specify
		Args:        []string{},         // User will specify
		EnvVars:     []EnvVarTemplate{}, // User will specify
		Timeout:     30 * time.Second,
		Features:    []string{"Custom command-line MCP functionality"},
		AuthType:    "none",
		Docs:        "https://modelcontextprotocol.io/",
	}
}

// GetTemplate returns a server template by ID
func (r *MCPServerRegistry) GetTemplate(id string) (MCPServerTemplate, bool) {
	template, exists := r.templates[id]
	return template, exists
}

// ListTemplates returns all available templates
func (r *MCPServerRegistry) ListTemplates() []MCPServerTemplate {
	templates := make([]MCPServerTemplate, 0, len(r.templates))
	for _, template := range r.templates {
		templates = append(templates, template)
	}
	return templates
}

// GetTemplatesByType returns templates of a specific type
func (r *MCPServerRegistry) GetTemplatesByType(serverType string) []MCPServerTemplate {
	templates := make([]MCPServerTemplate, 0)
	for _, template := range r.templates {
		if template.Type == serverType {
			templates = append(templates, template)
		}
	}
	return templates
}

// SearchTemplates searches for templates by name or description
func (r *MCPServerRegistry) SearchTemplates(query string) []MCPServerTemplate {
	query = strings.ToLower(query)
	templates := make([]MCPServerTemplate, 0)

	for _, template := range r.templates {
		if strings.Contains(strings.ToLower(template.Name), query) ||
			strings.Contains(strings.ToLower(template.Description), query) {
			templates = append(templates, template)
		}
	}
	return templates
}

// AddTemplate adds a custom template to the registry
func (r *MCPServerRegistry) AddTemplate(template MCPServerTemplate) error {
	if template.ID == "" {
		return fmt.Errorf("template ID cannot be empty")
	}
	if template.Name == "" {
		return fmt.Errorf("template name cannot be empty")
	}
	if template.Type == "" {
		template.Type = "stdio" // Default
	}

	r.templates[template.ID] = template
	return nil
}

// CreateServerConfig creates a server config from a template with user values
func (template *MCPServerTemplate) CreateServerConfig(name string, envValues map[string]string, customURL string, customCommand string, customArgs []string) MCPServerConfig {
	config := MCPServerConfig{
		Name:        name,
		Type:        template.Type,
		URL:         template.URL,
		Command:     template.Command,
		Args:        make([]string, len(template.Args)),
		Env:         make(map[string]string),
		AutoStart:   true,
		MaxRestarts: 3,
		Timeout:     template.Timeout,
	}

	// Copy args
	copy(config.Args, template.Args)

	// Use custom values if provided
	if customURL != "" {
		config.URL = customURL
	}
	if customCommand != "" {
		config.Command = customCommand
	}
	if len(customArgs) > 0 {
		config.Args = customArgs
	}

	// Set environment variables
	for _, envVar := range template.EnvVars {
		if value, exists := envValues[envVar.Name]; exists && value != "" {
			config.Env[envVar.Name] = value
		} else if envVar.Default != "" {
			config.Env[envVar.Name] = envVar.Default
		}
	}

	return config
}
