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
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/secretdetect"
)

func setupGitMCPServer(mcpConfig *mcp.MCPConfig, reader *bufio.Reader) error {
	fmt.Println()
	console.Heading(os.Stdout, "Git MCP Server Setup")
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

	// SP-169: framing question, asked once after the server is configured.
	if err := applyTrustUntrustedPrompt(&serverConfig, reader); err != nil {
		return err
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
// helpers. See promptChoice for the TTY / non-TTY contract.
func promptInstallMethod(reader *bufio.Reader, items []console.SelectItem) (string, bool, error) {
	return promptChoice(reader, "Pick an installation method", items)
}

// promptChoice shows a single-choice picker. On a TTY it delegates to
// console.SelectList for a slick interactive UI; in non-TTY contexts (e.g.
// piped stdin in tests) it falls back to a direct bufio.Reader numeric read
// so EOF surfaces as a wrapped error rather than being silently absorbed by
// the picker's runFallback path.
//
// Returns:
//   - (value, true, nil) on confirm
//   - ("", false, nil) on Esc/Ctrl+C cancellation, or empty input in non-TTY
//   - ("", false, err) on read failure (typically io.EOF or parse failure)
//
// Callers treat ("", false, nil) as a graceful cancel that returns nil to the
// user, and any non-nil error as a setup failure.
func promptChoice(reader *bufio.Reader, title string, items []console.SelectItem) (string, bool, error) {
	if StdinIsTerminal() {
		sl := console.NewSelectList(console.SelectListOptions{
			Title:      title,
			Items:      items,
			Searchable: false,
		})
		return sl.Run(context.Background())
	}

	fmt.Println(title)
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

// looksLikeHTTPURL reports whether s parses as an http(s) URL with a host —
// the shape of a web endpoint, which belongs in a server's URL field, not its
// command.
func looksLikeHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func validateHTTPURL(raw string) error {
	if !looksLikeHTTPURL(raw) {
		return fmt.Errorf("%q is not a valid HTTP URL — include the scheme and host, e.g. https://example.com/mcp", raw)
	}
	return nil
}

func setupCustomMCPServer(mcpConfig *mcp.MCPConfig, reader *bufio.Reader, registry *mcp.MCPServerRegistry) error {
	fmt.Println()
	console.Heading(os.Stdout, "Custom MCP Server Setup")
	fmt.Println()

	serverType, ok, err := promptChoice(reader, "What kind of MCP server are you adding?", []console.SelectItem{
		{
			Label:  "stdio",
			Detail: "a local command (npx, uvx, a binary) sprout talks to over stdin/stdout",
			Value:  "stdio",
		},
		{
			Label:  "http",
			Detail: "a remote URL sprout talks to over HTTP (Streamable HTTP / SSE)",
			Value:  "http",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to read server type: %w", err)
	}
	if !ok {
		fmt.Println()
		console.GlyphInfo.Print("Setup cancelled.")
		return nil
	}

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

	var serverConfig mcp.MCPServerConfig
	if serverType == "http" {
		serverConfig, err = promptHTTPCustomServer(serverName, reader)
	} else {
		serverConfig, err = promptStdioCustomServer(serverName, reader)
	}
	if err != nil {
		return err
	}

	// SP-169: the framing question is asked once, after the server is otherwise
	// configured — the last thing the user decides about this server.
	if err := applyTrustUntrustedPrompt(&serverConfig, reader); err != nil {
		return err
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
	if serverConfig.Type == "http" {
		fmt.Printf("URL: %s\n", serverConfig.URL)
	} else {
		fmt.Printf("Command: %s %v\n", serverConfig.Command, secretdetect.RedactOpaque(fmt.Sprintf("%v", serverConfig.Args)))
	}
	fmt.Println()
	fmt.Printf("To test the configuration, run: sprout mcp test %s\n", serverName)

	return nil
}

func promptStdioCustomServer(serverName string, reader *bufio.Reader) (mcp.MCPServerConfig, error) {
	fmt.Print("Enter command to run the server: ")
	commandInput, err := reader.ReadString('\n')
	if err != nil {
		return mcp.MCPServerConfig{}, fmt.Errorf("failed to read command: %w", err)
	}
	command := strings.TrimSpace(commandInput)

	if command == "" {
		return mcp.MCPServerConfig{}, errors.New("command is required")
	}
	if looksLikeHTTPURL(command) {
		return mcp.MCPServerConfig{}, fmt.Errorf("%q looks like a URL; did you mean an HTTP server? Pick http when asked for the server type", command)
	}

	fmt.Print("Enter command arguments (space-separated, or press Enter for none): ")
	argsInput, err := reader.ReadString('\n')
	if err != nil {
		return mcp.MCPServerConfig{}, fmt.Errorf("failed to read arguments: %w", err)
	}
	argsStr := strings.TrimSpace(argsInput)

	var args []string
	if argsStr != "" {
		args = strings.Fields(argsStr)
	}

	env, err := promptEnvVars(reader)
	if err != nil {
		return mcp.MCPServerConfig{}, err
	}

	fmt.Print("Enter working directory (or press Enter for default): ")
	workDirInput, err := reader.ReadString('\n')
	if err != nil {
		return mcp.MCPServerConfig{}, fmt.Errorf("failed to read working directory: %w", err)
	}
	workDir := strings.TrimSpace(workDirInput)

	autoStart, timeout, err := promptAutoStartAndTimeout(reader)
	if err != nil {
		return mcp.MCPServerConfig{}, err
	}

	return mcp.MCPServerConfig{
		Name:        serverName,
		Type:        "stdio",
		Command:     command,
		Args:        args,
		Env:         env,
		WorkingDir:  workDir,
		AutoStart:   autoStart,
		MaxRestarts: 3,
		Timeout:     timeout,
	}, nil
}

func promptHTTPCustomServer(serverName string, reader *bufio.Reader) (mcp.MCPServerConfig, error) {
	fmt.Print("Enter server URL: ")
	urlInput, err := reader.ReadString('\n')
	if err != nil {
		return mcp.MCPServerConfig{}, fmt.Errorf("failed to read URL: %w", err)
	}
	serverURL := strings.TrimSpace(urlInput)

	if serverURL == "" {
		return mcp.MCPServerConfig{}, errors.New("server URL is required for an HTTP server")
	}
	if err := validateHTTPURL(serverURL); err != nil {
		return mcp.MCPServerConfig{}, err
	}

	headers, err := promptHTTPHeaders(reader)
	if err != nil {
		return mcp.MCPServerConfig{}, err
	}
	// Header values are frequently credentials (Authorization: Bearer …),
	// so store secret-looking ones in the credential store and keep only a
	// placeholder in the config file — the same protection the Env map gets
	// at save time. buildRequestHeaders resolves the placeholder on request.
	for name, value := range headers {
		if value == "" || mcp.IsSecretRef(value) {
			continue
		}
		if mcp.IsSecretEnvVar(name) {
			key := mcp.CredentialKey(serverName, name)
			if err := credentials.SetToActiveBackend(key, value); err != nil {
				return mcp.MCPServerConfig{}, fmt.Errorf("failed to store credential for header %s: %w", name, err)
			}
			headers[name] = mcp.SecretRef(serverName, name)
		}
	}

	env, err := promptEnvVars(reader)
	if err != nil {
		return mcp.MCPServerConfig{}, err
	}

	autoStart, timeout, err := promptAutoStartAndTimeout(reader)
	if err != nil {
		return mcp.MCPServerConfig{}, err
	}

	return mcp.MCPServerConfig{
		Name:        serverName,
		Type:        "http",
		URL:         serverURL,
		Env:         env,
		Headers:     headers,
		AutoStart:   autoStart,
		MaxRestarts: 3,
		Timeout:     timeout,
	}, nil
}

func promptEnvVars(reader *bufio.Reader) (map[string]string, error) {
	fmt.Print("Enter environment variables (KEY=VALUE format, comma-separated, or press Enter for none): ")
	envInput, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read environment variables: %w", err)
	}
	envStr := strings.TrimSpace(envInput)

	env := make(map[string]string)
	if envStr != "" {
		for _, pair := range strings.Split(envStr, ",") {
			pair = strings.TrimSpace(pair)
			if parts := strings.SplitN(pair, "=", 2); len(parts) == 2 {
				env[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}
	return env, nil
}

func promptHTTPHeaders(reader *bufio.Reader) (map[string]string, error) {
	fmt.Print("Enter HTTP headers (Name: VALUE, separated by \", \", or press Enter for none): ")
	headersInput, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read HTTP headers: %w", err)
	}
	headersStr := strings.TrimSpace(headersInput)

	headers := make(map[string]string)
	if headersStr != "" {
		// Split on comma+space so values that legitimately contain commas
		// (e.g. "Accept: text/html,application/xhtml+xml") survive intact;
		// a lone comma can't be told apart from a separator inside a value.
		for _, pair := range strings.Split(headersStr, ", ") {
			pair = strings.TrimSpace(pair)
			if parts := strings.SplitN(pair, ":", 2); len(parts) == 2 {
				name := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				if name != "" {
					headers[name] = value
				}
			}
		}
	}
	return headers, nil
}

// promptTrustUntrustedContent asks the SP-169 question once, after the server
// is otherwise configured: should this server's tool output be framed as
// untrusted data?
//
// Default yes. The question is asked because the answer is a judgement about a
// third party, and the explanatory text matters as much as the toggle — a
// setting whose consequence is opaque gets turned off, so it says plainly that
// framing is a label and not a restriction.
//
// Non-interactive runs (piped stdin, autonomous/workflow runs) do not prompt:
// they take the default, which is the conservative choice and costs nothing.
func promptTrustUntrustedContent(serverName string, reader *bufio.Reader) (bool, error) {
	if !StdinIsTerminal() {
		return true, nil
	}

	fmt.Print(trustUntrustedPromptText(serverName))
	fmt.Print("Frame this server's output as untrusted data? (Y/n): ")

	raw, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(raw) == "" {
		// EOF with no answer: keep the default rather than failing the setup —
		// the server is already configured by this point.
		fmt.Println()
		return true, nil
	}
	return strings.ToLower(strings.TrimSpace(raw)) != "n", nil
}

// trustUntrustedPromptText is the question and its explanation, kept separate
// from the prompt call so the wording is assertable without a TTY. The text is
// load-bearing: a toggle whose consequence is opaque gets turned off, so it says
// plainly that framing is a label and not a restriction.
func trustUntrustedPromptText(serverName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Treat output from %q as untrusted content?\n", serverName)
	b.WriteString("Tools on this server return text this agent did not write. Framing marks\n")
	b.WriteString("that text as data, so instructions inside it are less likely to be followed.\n")
	b.WriteString("It does not block tools or change what they can do.\n")
	return b.String()
}

// applyTrustUntrustedPrompt asks the framing question and records the answer on
// the server config. It is the single call site shape shared by every add flow.
func applyTrustUntrustedPrompt(serverConfig *mcp.MCPServerConfig, reader *bufio.Reader) error {
	trust, err := promptTrustUntrustedContent(serverConfig.Name, reader)
	if err != nil {
		return err
	}
	serverConfig.SetTrustUntrustedContent(trust)
	return nil
}

func promptAutoStartAndTimeout(reader *bufio.Reader) (bool, time.Duration, error) {
	fmt.Print("Auto-start this server? (Y/n): ")
	autoStartInput, err := reader.ReadString('\n')
	if err != nil {
		return false, 0, fmt.Errorf("failed to read auto-start preference: %w", err)
	}
	autoStart := strings.ToLower(strings.TrimSpace(autoStartInput)) != "n"

	fmt.Print("Server timeout in seconds (default: 30): ")
	timeoutInput, err := reader.ReadString('\n')
	if err != nil {
		return false, 0, fmt.Errorf("failed to read timeout: %w", err)
	}
	timeoutStr := strings.TrimSpace(timeoutInput)

	timeout := 30 * time.Second
	if timeoutStr != "" {
		if timeoutSecs, err := strconv.Atoi(timeoutStr); err == nil && timeoutSecs > 0 {
			timeout = time.Duration(timeoutSecs) * time.Second
		}
	}
	return autoStart, timeout, nil
}
