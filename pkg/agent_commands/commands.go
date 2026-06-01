package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// Command represents a slash command
type Command interface {
	Name() string
	Description() string
	Execute(args []string, chatAgent *agent.Agent) error
}

// JSONCommand extends Command to support JSON output
type JSONCommand interface {
	Command
	ExecuteWithJSONOutput(args []string, chatAgent *agent.Agent, ctx *CommandContext) error
}

// CommandContext provides context for command execution
type CommandContext struct {
	OutputFormat OutputFormat
}

// OutputFormat defines output format for commands
type OutputFormat int

const (
	OutputText OutputFormat = iota
	OutputJSON
)

// UsageProvider is implemented by commands that want to surface a longer
// per-command help string via `/help <name>`. Commands that don't implement
// it fall back to their Description().
type UsageProvider interface {
	Usage() string
}

// CommandRegistry manages all available slash commands
type CommandRegistry struct {
	commands map[string]Command
	aliases  map[string]string // short → canonical command name
}

// NewCommandRegistry creates a new command registry
func NewCommandRegistry() *CommandRegistry {
	registry := &CommandRegistry{
		commands: make(map[string]Command),
		aliases:  make(map[string]string),
	}

	// Register built-in commands
	registry.Register(&HelpCommand{registry: registry})
	registry.Register(&ModelsCommand{})
	registry.Register(&ProvidersCommand{})
	registry.Register(&SessionsCommand{})
	registry.Register(&ClearCommand{})
	registry.Register(&InitCommand{})
	registry.Register(&ExitCommand{})
	registry.Register(&CommitCommand{})
	registry.Register(&ExecCommand{})
	registry.Register(&ShellCommand{})
	registry.Register(&StatsCommand{})

	// Register subagent configuration commands
	registry.Register(&SubagentConfigCommand{configType: "provider"})
	registry.Register(&SubagentConfigCommand{configType: "model"})

	// Register subagent persona commands
	registry.Register(&SubagentPersonasCommand{})
	registry.Register(&SubagentPersonaCommand{})
	registry.Register(&PersonaCommand{})

	// Register change tracking commands
	registry.Register(&ChangesCommand{})
	registry.Register(&StatusCommand{})
	registry.Register(&LogCommand{})
	registry.Register(&RollbackCommand{})

	// Register MCP commands
	registry.Register(&MCPCommand{})

	// Register code review command
	registry.Register(&ReviewCommand{})
	registry.Register(&ReviewDeepCommand{})
	registry.Register(&SelfReviewCommand{})
	registry.Register(&SelfReviewGateCommand{})

	// Register compaction command
	registry.Register(&CompactCommand{})

	// Register indexing command
	registry.Register(&IndexCommand{})

	// Register edit command
	registry.Register(&EditCommand{})

	// Register extend command
	registry.Register(&ExtendCommand{})

	// SP-058: risk profile management
	registry.Register(&RiskProfileCommand{})

	registry.Register(&SetupCommand{})

	// SP-048-2d: short aliases for the most-used commands. Aliases resolve
	// to canonical names during dispatch and appear in tab completion.
	registry.RegisterAlias("m", "model")
	registry.RegisterAlias("p", "provider")
	registry.RegisterAlias("x", "exit")
	registry.RegisterAlias("q", "exit")
	registry.RegisterAlias("?", "help")
	registry.RegisterAlias("h", "help")

	return registry
}

// RegisterAlias maps a short name to a canonical command name. Looking up
// the alias via GetCommand or Execute resolves transparently. Aliases also
// participate in tab completion and "did you mean" suggestions.
func (r *CommandRegistry) RegisterAlias(short, canonical string) {
	if short == "" || canonical == "" {
		return
	}
	r.aliases[short] = canonical
}

// AliasesOf returns the set of aliases that resolve to the given canonical
// command name. Used by /help <name> to show alternate spellings.
func (r *CommandRegistry) AliasesOf(canonical string) []string {
	var out []string
	for short, target := range r.aliases {
		if target == canonical {
			out = append(out, short)
		}
	}
	return out
}

// CompletionCandidates returns all valid slash-command names (canonical
// plus aliases) sorted alphabetically. Used by the tab-completion
// CompletionProvider in the input reader.
func (r *CommandRegistry) CompletionCandidates() []string {
	out := make([]string, 0, len(r.commands)+len(r.aliases))
	for name := range r.commands {
		out = append(out, name)
	}
	for alias := range r.aliases {
		out = append(out, alias)
	}
	// Stable sort so cycle order is deterministic for the user.
	sort.Strings(out)
	return out
}

// Register adds a command to the registry
func (r *CommandRegistry) Register(cmd Command) {
	r.commands[cmd.Name()] = cmd
}

// Execute processes a slash command input
// Supports both / and ! prefixes (e.g. /exec ls or !exec ls)
func (r *CommandRegistry) Execute(input string, chatAgent *agent.Agent) error {
	trimmed := strings.TrimSpace(input)

	// Check for slash or bang prefix
	var prefix string
	if strings.HasPrefix(trimmed, "/") {
		prefix = "/"
	} else if strings.HasPrefix(trimmed, "!") {
		prefix = "!"
	} else {
		return errors.New("not a valid command (must start with / or !)")
	}

	// Parse command and arguments
	parts := strings.Fields(trimmed[1:]) // Remove leading prefix
	if len(parts) == 0 {
		return errors.New("empty command")
	}

	commandName := parts[0]
	args := parts[1:]

	// If using ! as prefix, default to exec command (to match other tools that use ! for shell commands)
	if prefix == "!" && commandName != "exec" {
		// Reconstruct the full command after the bang as exec arguments
		fullCommand := strings.Join(parts, " ")
		args = []string{fullCommand}
		commandName = "exec"
	}

	// Resolve aliases (SP-048-2d) before dispatching.
	if canonical, ok := r.aliases[commandName]; ok {
		commandName = canonical
	}

	// Find and execute command
	cmd, exists := r.commands[commandName]
	if !exists {
		// SP-048-2b: did-you-mean suggestions in the error message.
		suggestions := r.SuggestCommands(commandName, 2)
		if len(suggestions) > 0 {
			return fmt.Errorf("unknown command: %s — did you mean /%s?", commandName, strings.Join(suggestions, " or /"))
		}
		return fmt.Errorf("unknown command: %s", commandName)
	}

	// Check if command supports JSON output (--json flag is in args)
	if jsonCmd, ok := cmd.(JSONCommand); ok && contains(args, "--json") {
		ctx := &CommandContext{
			OutputFormat: OutputJSON,
		}
		// Filter out --json flag for the command
		filteredArgs := filterArgs(args, "--json")
		return jsonCmd.ExecuteWithJSONOutput(filteredArgs, chatAgent, ctx)
	}

	// Default execution for commands without context support
	return cmd.Execute(args, chatAgent)
}

// IsSlashCommand checks if input starts with a slash or bang
func (r *CommandRegistry) IsSlashCommand(input string) bool {
	trimmed := strings.TrimSpace(input)
	if strings.HasPrefix(trimmed, "!") {
		return len(strings.TrimSpace(trimmed[1:])) > 0
	}
	if !strings.HasPrefix(trimmed, "/") {
		return false
	}

	remainder := strings.TrimSpace(trimmed[1:])
	if remainder == "" {
		return false
	}

	commandName := strings.Fields(remainder)[0]
	if strings.Contains(commandName, "/") || strings.Contains(commandName, "\\") {
		return false
	}
	if !isLikelySlashCommandName(commandName) {
		return false
	}
	return true
}

func isLikelySlashCommandName(name string) bool {
	for _, r := range name {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// GetCommand returns a command by name. If name matches an alias, the
// canonical command is returned (SP-048-2d).
func (r *CommandRegistry) GetCommand(name string) (Command, bool) {
	if canonical, ok := r.aliases[name]; ok {
		name = canonical
	}
	cmd, exists := r.commands[name]
	return cmd, exists
}

// ListCommands returns all available commands
func (r *CommandRegistry) ListCommands() []Command {
	commands := make([]Command, 0, len(r.commands))
	for _, cmd := range r.commands {
		commands = append(commands, cmd)
	}
	return commands
}

// Helper functions for working with args and output

// contains checks if a string is in a slice
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// filterArgs removes specified items from a slice
func filterArgs(slice []string, item string) []string {
	filtered := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != item {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

// OutputWriter captures and buffers output
type OutputWriter struct {
	Buffer bytes.Buffer
}

// Write implements io.Writer
func (ow *OutputWriter) Write(p []byte) (n int, err error) {
	return ow.Buffer.Write(p)
}

// String returns the captured output
func (ow *OutputWriter) String() string {
	return ow.Buffer.String()
}

// WriteToOutput writes a string to os.Stdout
func WriteToOutput(output string) {
	os.Stdout.WriteString(output)
}

// WriteJSONToOutput writes a JSON representation of value to stdout
func WriteJSONToOutput(value interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
