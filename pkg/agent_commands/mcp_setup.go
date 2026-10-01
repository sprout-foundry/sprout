package commands

// mcp_setup.go — the /mcp add path: addServer dispatch, the registry
// server setup (setupServerFromRegistry), and the template-based setup
// (setupServerFromTemplate) with its interactive prompts. Split out of mcp.go.
import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/secretdetect"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// addServer handles MCP server addition
func (m *MCPCommand) addServer(chatAgent *agent.Agent) error {
	reader := bufio.NewReader(os.Stdin)

	m.println("[>>] MCP Server Setup")
	m.println("==================")
	m.println()

	// Load existing config (no longer needed for MCP)

	mcpConfig, err := mcp.LoadMCPConfig()
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	// Create server registry
	registry := mcp.NewMCPServerRegistry()

	return m.setupServerFromRegistry(&mcpConfig, registry, reader)
}

// setupServerFromRegistry sets up an MCP server using the template registry
func (m *MCPCommand) setupServerFromRegistry(mcpConfig *mcp.MCPConfig, registry *mcp.MCPServerRegistry, reader *bufio.Reader) error {
	// Show available templates
	templates := registry.ListTemplates()
	m.println("Select MCP server type:")
	m.println()

	for i, template := range templates {
		m.printf("%d. %s\n", i+1, template.Name)
		m.printf("   %s\n", template.Description)
		if len(template.Features) > 0 {
			m.printf("   Features: %s\n", strings.Join(template.Features, ", "))
		}
		m.println()
	}

	m.print("Choice (1-" + strconv.Itoa(len(templates)) + "): ")
	choice, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	choiceNum, err := strconv.Atoi(strings.TrimSpace(choice))
	if err != nil || choiceNum < 1 || choiceNum > len(templates) {
		return fmt.Errorf("invalid choice: %s", choice)
	}

	template := templates[choiceNum-1]
	return m.setupServerFromTemplate(mcpConfig, template, reader)
}

// setupServerFromTemplate sets up an MCP server from a specific template
func (m *MCPCommand) setupServerFromTemplate(mcpConfig *mcp.MCPConfig, template mcp.MCPServerTemplate, reader *bufio.Reader) error {
	m.println()
	console.GlyphInfo.Fprintf(m.out(), "%s Setup", template.Name)
	m.println(strings.Repeat("=", len(template.Name)+7))
	m.println()

	if template.Docs != "" {
		m.printf("  Documentation: %s\n", template.Docs)
		m.println()
	}

	// Get server name
	m.printf("Enter server name (default: %s): ", strings.ToLower(strings.ReplaceAll(template.Name, " ", "-")))
	nameInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read server name: %w", err)
	}

	serverName := strings.TrimSpace(nameInput)
	if serverName == "" {
		// Generate default name from template
		serverName = strings.ToLower(strings.ReplaceAll(template.Name, " ", "-"))
		serverName = strings.ReplaceAll(serverName, "(", "")
		serverName = strings.ReplaceAll(serverName, ")", "")
		// Take first word for common cases
		if strings.Contains(serverName, "-") {
			serverName = strings.Split(serverName, "-")[0]
		}
	}

	// Check if server already exists
	if _, exists := mcpConfig.Servers[serverName]; exists {
		m.printf("Server '%s' already exists. Reconfigure? %s: ", serverName, utils.DefaultChoiceHint(false))
		confirm, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			m.println("Setup cancelled.")
			return nil
		}
	}

	// Collect environment variables
	envValues := make(map[string]string)
	for _, envVar := range template.EnvVars {
		var value string

		// Check if already set in environment
		if existingValue := os.Getenv(envVar.Name); existingValue != "" {
			if envVar.Secret {
				m.printf("Using existing %s from environment\n", envVar.Name)
			} else {
				m.printf("Using existing %s from environment: %s\n", envVar.Name, existingValue)
			}
			value = existingValue
		} else {
			// Prompt user for value
			m.printf("%s:\n", envVar.Description)
			if envVar.Required {
				m.print("Enter " + envVar.Name + ": ")
			} else {
				defaultText := ""
				if envVar.Default != "" {
					defaultText = fmt.Sprintf(" (default: %s)", envVar.Default)
				}
				m.printf("Enter %s%s: ", envVar.Name, defaultText)
			}

			input, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", envVar.Name, err)
			}
			value = strings.TrimSpace(input)

			if value == "" && envVar.Required {
				return fmt.Errorf("%s is required", envVar.Name)
			}
		}

		if value != "" {
			envValues[envVar.Name] = value
		}
	}

	// Handle custom values for generic templates
	var customURL, customCommand string
	var customArgs []string

	if template.ID == "http-generic" {
		m.print("Enter MCP server URL: ")
		urlInput, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read URL: %w", err)
		}
		customURL = strings.TrimSpace(urlInput)
		if customURL == "" {
			return errors.New("URL is required for HTTP servers")
		}
	}

	if template.ID == "stdio-generic" {
		m.print("Enter command: ")
		cmdInput, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read command: %w", err)
		}
		customCommand = strings.TrimSpace(cmdInput)
		if customCommand == "" {
			return errors.New("command is required for stdio servers")
		}

		m.print("Enter arguments (space-separated, or press Enter for none): ")
		argsInput, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read arguments: %w", err)
		}
		argsStr := strings.TrimSpace(argsInput)
		if argsStr != "" {
			customArgs = strings.Fields(argsStr)
		}
	}

	// Create server config from template
	serverConfig := template.CreateServerConfig(serverName, envValues, customURL, customCommand, customArgs)

	// Add server to config
	mcpConfig.Servers[serverName] = serverConfig
	mcpConfig.Enabled = true

	// Save config
	if err := mcp.SaveMCPConfig(mcpConfig); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}

	m.println()
	console.GlyphSuccess.Fprintf(m.out(), "%s configured successfully!", template.Name)
	if serverConfig.Type == "http" {
		m.printf("Type: Remote HTTP server\n")
		m.printf("URL: %s\n", secretdetect.RedactOpaque(serverConfig.URL))
	} else {
		m.printf("Command: %s %v\n", serverConfig.Command, secretdetect.RedactOpaque(fmt.Sprintf("%v", serverConfig.Args)))
	}
	m.println()
	m.printf("To test the configuration, run: /mcp test %s\n", serverName)

	if len(template.Features) > 0 {
		m.println()
		console.GlyphInfo.Fprintln(m.out(), "Features available:")
		for _, feature := range template.Features {
			m.printf("• %s\n", feature)
		}
	}

	return nil
}
