//go:build !js

package cmd

// mcp_add_servers.go — the git + custom MCP server setup flows for
// `sprout mcp add`: setupGitMCPServer, setupCustomMCPServer, and the
// shared install-method prompt (promptInstallMethod, also used by the
// browser setup flows in mcp_add_browsers.go). Split out of mcp_add.go.
import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/secretdetect"
)

func setupGitMCPServer(mcpConfig *mcp.MCPConfig, reader *bufio.Reader) error {
	fmt.Println()
	fmt.Println("Git MCP Server Setup")
	fmt.Println("========================")
	fmt.Println()

	// Check if Git server already exists
	if _, exists := mcpConfig.Servers["git"]; exists {
		fmt.Print("Git MCP server is already configured. Reconfigure? (y/N): ")
		confirm, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			fmt.Println("Setup cancelled.")
			return nil
		}
	}

	// Get repository path (optional)
	fmt.Print("Enter repository path (optional, leave empty to use current directory): ")
	repoInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read repository path: %w", err)
	}
	repoPath := strings.TrimSpace(repoInput)

	// Installation method picker
	installChoice, ok, err := promptInstallMethod(reader, []console.SelectItem{
		{Label: "uvx (recommended)", Value: "1"},
		{Label: "pip/pipx", Value: "2"},
	})
	if err != nil {
		return fmt.Errorf("failed to read installation method: %w", err)
	}
	if !ok {
		fmt.Println()
		console.GlyphInfo.Print("Setup cancelled.")
		return nil
	}

	var serverConfig mcp.MCPServerConfig

	switch installChoice {
	case "1", "":
		args := []string{"mcp-server-git"}
		if repoPath != "" {
			args = append(args, "--repository", repoPath)
		}
		serverConfig = mcp.MCPServerConfig{
			Name:        "git",
			Command:     "uvx",
			Args:        args,
			AutoStart:   true,
			MaxRestarts: 3,
			Timeout:     30 * time.Second,
		}
	case "2":
		args := []string{"-m", "mcp_server_git"}
		if repoPath != "" {
			args = append(args, "--repository", repoPath)
		}
		serverConfig = mcp.MCPServerConfig{
			Name:        "git",
			Command:     "python",
			Args:        args,
			AutoStart:   true,
			MaxRestarts: 3,
			Timeout:     30 * time.Second,
		}
	default:
		return fmt.Errorf("invalid choice: %s", installChoice)
	}

	// Add server to config
	mcpConfig.Servers["git"] = serverConfig
	mcpConfig.Enabled = true

	// Save config
	if err := mcp.SaveMCPConfig(mcpConfig); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}

	fmt.Println()
	console.GlyphSuccess.Fprintln(os.Stdout, "Git MCP Server configured successfully!")
	fmt.Printf("Command: %s %v\n", serverConfig.Command, secretdetect.RedactOpaque(fmt.Sprintf("%v", serverConfig.Args)))
	fmt.Println()
	fmt.Println("To test the configuration, run: sprout mcp test git")
	fmt.Println()

	// Installation instructions
	if installChoice == "1" {
		console.GlyphInfo.Print("Installation (if not already installed):")
		fmt.Println("No installation needed - uvx will install automatically")
	} else {
		console.GlyphInfo.Print("Installation (if not already installed):")
		fmt.Println("pip install mcp-server-git")
	}

	return nil
}

// promptInstallMethod shows the install-method picker used by the setup*MCPServer
// helpers. On a TTY it delegates to console.SelectList for a slick interactive UI;
// in non-TTY contexts (e.g. piped stdin in tests) it falls back to a direct
// bufio.Reader numeric read so EOF surfaces as a wrapped error rather than being
// silently absorbed by the picker's runFallback path.
//
// Returns:
//   - (value, true, nil) on confirm
//   - ("", false, nil) on Esc/Ctrl+C cancellation, or empty input in non-TTY
//   - ("", false, err) on read failure (typically io.EOF or parse failure)
//
// Callers treat ("", false, nil) as a graceful cancel that returns nil to the
// user, and any non-nil error as a setup failure.
func promptInstallMethod(reader *bufio.Reader, items []console.SelectItem) (string, bool, error) {
	if StdinIsTerminal() {
		sl := console.NewSelectList(console.SelectListOptions{
			Title:      "Pick an installation method",
			Items:      items,
			Searchable: false,
		})
		return sl.Run(context.Background())
	}

	fmt.Println("Pick an installation method:")
	for i, item := range items {
		label := item.Label
		if item.Detail != "" {
			label = fmt.Sprintf("%s  —  %s", label, item.Detail)
		}
		fmt.Printf("  %d. %s\n", i+1, label)
	}
	fmt.Printf("Choice (1-%d): ", len(items))

	raw, err := reader.ReadString('\n')
	if err != nil {
		return "", false, err
	}
	choice := strings.TrimSpace(raw)
	if choice == "" {
		return "", false, nil
	}
	n, err := strconv.Atoi(choice)
	if err != nil || n < 1 || n > len(items) {
		return "", false, fmt.Errorf("invalid choice: %s", choice)
	}
	return items[n-1].Value, true, nil
}

func setupCustomMCPServer(mcpConfig *mcp.MCPConfig, reader *bufio.Reader, registry *mcp.MCPServerRegistry) error {
	fmt.Println()
	console.GlyphInfo.Print("Custom MCP Server Setup")
	fmt.Println("==========================")
	fmt.Println()

	// Server name
	fmt.Print("Enter server name: ")
	nameInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read server name: %w", err)
	}
	serverName := strings.TrimSpace(nameInput)

	if serverName == "" {
		return errors.New("server name is required")
	}

	// Check if server already exists
	if _, exists := mcpConfig.Servers[serverName]; exists {
		fmt.Printf("Server '%s' already exists. Reconfigure? (y/N): ", serverName)
		confirm, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			fmt.Println("Setup cancelled.")
			return nil
		}
	}

	// Command
	fmt.Print("Enter command to run the server: ")
	commandInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read command: %w", err)
	}
	command := strings.TrimSpace(commandInput)

	if command == "" {
		return errors.New("command is required")
	}

	// Arguments
	fmt.Print("Enter command arguments (space-separated, or press Enter for none): ")
	argsInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read arguments: %w", err)
	}
	argsStr := strings.TrimSpace(argsInput)

	var args []string
	if argsStr != "" {
		args = strings.Fields(argsStr)
	}

	// Environment variables
	fmt.Print("Enter environment variables (KEY=VALUE format, comma-separated, or press Enter for none): ")
	envInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read environment variables: %w", err)
	}
	envStr := strings.TrimSpace(envInput)

	env := make(map[string]string)
	if envStr != "" {
		envPairs := strings.Split(envStr, ",")
		for _, pair := range envPairs {
			pair = strings.TrimSpace(pair)
			if parts := strings.SplitN(pair, "=", 2); len(parts) == 2 {
				env[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}

	// Working directory
	fmt.Print("Enter working directory (or press Enter for default): ")
	workDirInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read working directory: %w", err)
	}
	workDir := strings.TrimSpace(workDirInput)

	// Auto-start
	fmt.Print("Auto-start this server? (Y/n): ")
	autoStartInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read auto-start preference: %w", err)
	}
	autoStart := strings.ToLower(strings.TrimSpace(autoStartInput)) != "n"

	// Timeout
	fmt.Print("Server timeout in seconds (default: 30): ")
	timeoutInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read timeout: %w", err)
	}
	timeoutStr := strings.TrimSpace(timeoutInput)

	timeout := 30 * time.Second
	if timeoutStr != "" {
		if timeoutSecs, err := strconv.Atoi(timeoutStr); err == nil && timeoutSecs > 0 {
			timeout = time.Duration(timeoutSecs) * time.Second
		}
	}

	// Create server config
	serverConfig := mcp.MCPServerConfig{
		Name:        serverName,
		Command:     command,
		Args:        args,
		Env:         env,
		WorkingDir:  workDir,
		AutoStart:   autoStart,
		MaxRestarts: 3,
		Timeout:     timeout,
	}

	// Add server to config
	mcpConfig.Servers[serverName] = serverConfig
	mcpConfig.Enabled = true

	// Save config
	if err := mcp.SaveMCPConfig(mcpConfig); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}

	fmt.Println()
	console.GlyphSuccess.Fprintf(os.Stdout, "Custom MCP Server '%s' configured successfully!", serverName)
	fmt.Printf("Command: %s %v\n", serverConfig.Command, secretdetect.RedactOpaque(fmt.Sprintf("%v", serverConfig.Args)))
	fmt.Println()
	fmt.Printf("To test the configuration, run: sprout mcp test %s\n", serverName)

	return nil
}
