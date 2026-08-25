package commands

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// HelpCommand implements the /help slash command
type HelpCommand struct {
	registry *CommandRegistry
	stdout   io.Writer
}

func (h *HelpCommand) SetOutput(w io.Writer) { h.stdout = w }

func (h *HelpCommand) out() io.Writer {
	if h.stdout != nil {
		return h.stdout
	}
	return os.Stdout
}

// Name returns the command name
func (h *HelpCommand) Name() string {
	return "help"
}

// SafeDuringSteer returns true - /help is read-only
func (h *HelpCommand) SafeDuringSteer() bool {
	return true
}

// Description returns the command description
func (h *HelpCommand) Description() string {
	return "Show help information and available slash commands"
}

// Usage returns the detailed help text shown by `/help help`.
func (h *HelpCommand) Usage() string {
	return strings.Join([]string{
		"/help              List all slash commands.",
		"/help <command>    Show detailed help for a specific command.",
		"/help <alias>      Resolves aliases (e.g. /help m → /help model).",
		"",
		"Aliases: /h, /?",
	}, "\n")
}

// Execute runs the help command. With no args, prints the global help and
// command list. With one arg, prints per-command help (SP-048-2c).
func (h *HelpCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if len(args) > 0 {
		return h.printCommandHelp(strings.TrimPrefix(args[0], "/"))
	}
	fmt.Fprintln(h.out())
	console.GlyphInfo.Fprintln(h.out(), "Sprout - AI Coding Agent")
	fmt.Fprint(h.out(), `
A command-line coding assistant that uses AI to help you build software.

USAGE:
  Interactive mode:     ./sprout
  Non-interactive:      ./sprout "your query here"
  Custom model:         ./sprout --provider openrouter --model qwen/qwen3-coder-30b "your query"
  Piped input:         echo "your query" | ./sprout

EXAMPLES:
  # Interactive mode
  ./sprout
  > Create a simple Go HTTP server

  # Non-interactive
  ./sprout "Create a simple Go HTTP server"

  # Use specific provider/model
  ./sprout --provider openrouter --model qwen/qwen3-coder-30b "Fix the bug"

  # Piped input
  echo "Explain this code" | ./sprout

AVAILABLE TOOLS:
  • shell_command - Execute shell commands
  • read_file - Read file contents  
  • write_file - Create new files
  • edit_file - Modify existing files
  • TodoWrite/TodoRead - Task management
  • run_subagent - Delegate to subagent
  • run_parallel_subagents - Run multiple subagents in parallel
  • list_skills/activate_skill - Load skill instructions into context

KEY COMMANDS:
  /help       - Show this help message (aliases: /h, /?)
  /commit     - Interactive commit workflow (alias: /c)
  /model      - Switch model (alias: /m)
  /provider   - Switch provider (alias: /p)
  /persona    - Configure personas (provider/model/tools/prompt). Aliases: /subagent-persona, /subagent-personas
  /search     - Semantic code search (alias: /s)
  /review     - AI code review on staged changes (alias: /r)
  /skill      - Install, update, remove, and list skills
  /info       - Quick agent state overview (model, provider, context, cost)

Type 'exit' or 'quit' to end the session.

`)

	// List all registered commands, sorted, with aliases inline.
	commands := h.registry.ListCommands()
	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name() < commands[j].Name()
	})
	fmt.Fprintln(h.out(), "AVAILABLE SLASH COMMANDS:")
	for _, cmd := range commands {
		aliases := h.registry.AliasesOf(cmd.Name())
		sort.Strings(aliases)
		if len(aliases) > 0 {
			aliasParts := make([]string, len(aliases))
			for i, a := range aliases {
				aliasParts[i] = "/" + a
			}
			fmt.Fprintf(h.out(), "  /%s (%s) - %s\n", cmd.Name(), strings.Join(aliasParts, ", "), cmd.Description())
		} else {
			fmt.Fprintf(h.out(), "  /%s - %s\n", cmd.Name(), cmd.Description())
		}
	}

	fmt.Fprintln(h.out())
	fmt.Fprintln(h.out(), "Tip: type /help <command> for per-command details, or press Tab after / to autocomplete.")
	fmt.Fprintln(h.out())

	return nil
}

// Complete provides argument completions for /help. Suggests command
// names (canonical and aliases) from the registry.
func (h *HelpCommand) Complete(args []string, chatAgent *agent.Agent) []string {
	prefix := ""
	if len(args) > 0 {
		prefix = args[len(args)-1]
	}
	candidates := h.registry.CompletionCandidates()
	var matches []string
	for _, name := range candidates {
		if prefix == "" || strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	return matches
}

// printCommandHelp emits a single command's detailed help. Commands that
// implement UsageProvider supply their own multi-line usage text; others
// fall back to Description.
func (h *HelpCommand) printCommandHelp(name string) error {
	// Resolve aliases so /help m → /help models.
	resolved := name
	if canonical, ok := h.registry.aliases[name]; ok {
		resolved = canonical
	}

	cmd, exists := h.registry.GetCommand(resolved)
	if !exists {
		suggestions := h.registry.SuggestCommands(name, 2)
		if len(suggestions) > 0 {
			return fmt.Errorf("no such command: /%s — did you mean /%s?", name, strings.Join(suggestions, " or /"))
		}
		return fmt.Errorf("no such command: /%s", name)
	}

	fmt.Fprintln(h.out())
	console.GlyphInfo.Fprintf(h.out(), "/%s — %s", cmd.Name(), cmd.Description())
	fmt.Fprintln(h.out())

	aliases := h.registry.AliasesOf(cmd.Name())
	if len(aliases) > 0 {
		sort.Strings(aliases)
		aliasParts := make([]string, len(aliases))
		for i, a := range aliases {
			aliasParts[i] = "/" + a
		}
		fmt.Fprintf(h.out(), "Aliases: %s\n\n", strings.Join(aliasParts, ", "))
	}

	if u, ok := cmd.(UsageProvider); ok {
		fmt.Fprintln(h.out(), u.Usage())
	} else {
		fmt.Fprintln(h.out(), "(No additional usage details available.)")
	}
	fmt.Fprintln(h.out())
	return nil
}
