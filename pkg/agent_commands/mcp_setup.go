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

	fmt.Println("[>>] MCP Server Setup")
	fmt.Println("==================")
	fmt.Println()

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
	fmt.Println("Select MCP server type:")
	fmt.Println()

	for i, template := range templates {
		fmt.Printf("%d. %s\n", i+1, template.Name)
		fmt.Printf("   %s\n", template.Description)
		if len(template.Features) > 0 {
			fmt.Printf("   Features: %s\n", strings.Join(template.Features, ", "))
		}
		fmt.Println()
	}

	fmt.Print("Choice (1-" + strconv.Itoa(len(templates)) + "): ")
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
	fmt.Println()
	console.GlyphInfo.Printf("%s Setup", template.Name)
	fmt.Println(strings.Repeat("=", len(template.Name)+7))
	fmt.Println()

	if template.Docs != "" {
		fmt.Printf("[lib] Documentation: %s\n", template.Docs)
		fmt.Println()
	}

	// Get server name
	fmt.Printf("Enter server name (default: %s): ", strings.ToLower(strings.ReplaceAll(template.Name, " ", "-")))
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
		fmt.Printf("Server '%s' already exists. Reconfigure? %s: ", serverName, utils.DefaultChoiceHint(false))
		confirm, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			fmt.Println("Setup cancelled.")
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
				fmt.Printf("Using existing %s from environment\n", envVar.Name)
			} else {
				fmt.Printf("Using existing %s from environment: %s\n", envVar.Name, existingValue)
			}
			value = existingValue
		} else {
			// Prompt user for value
			fmt.Printf("%s:\n", envVar.Description)
			if envVar.Required {
				fmt.Print("Enter " + envVar.Name + ": ")
			} else {
				defaultText := ""
				if envVar.Default != "" {
					defaultText = fmt.Sprintf(" (default: %s)", envVar.Default)
				}
				fmt.Printf("Enter %s%s: ", envVar.Name, defaultText)
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
		fmt.Print("Enter MCP server URL: ")
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
		fmt.Print("Enter command: ")
		cmdInput, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read command: %w", err)
		}
		customCommand = strings.TrimSpace(cmdInput)
		if customCommand == "" {
			return errors.New("command is required for stdio servers")
		}

		fmt.Print("Enter arguments (space-separated, or press Enter for none): ")
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

	fmt.Println()
	console.GlyphSuccess.Printf("%s configured successfully!", template.Name)
	if serverConfig.Type == "http" {
		fmt.Printf("Type: Remote HTTP server\n")
		fmt.Printf("URL: %s\n", secretdetect.RedactOpaque(serverConfig.URL))
	} else {
		fmt.Printf("Command: %s %v\n", serverConfig.Command, secretdetect.RedactOpaque(fmt.Sprintf("%v", serverConfig.Args)))
	}
	fmt.Println()
	fmt.Printf("To test the configuration, run: /mcp test %s\n", serverName)

	if len(template.Features) > 0 {
		fmt.Println()
		console.GlyphInfo.Print("Features available:")
		for _, feature := range template.Features {
			fmt.Printf("• %s\n", feature)
		}
	}

	return nil
}
