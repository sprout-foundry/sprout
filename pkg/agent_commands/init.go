package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
)

// InitCommand implements the /init slash command
type InitCommand struct{}

// Name returns the command name
func (i *InitCommand) Name() string {
	return "init"
}

// Description returns the command description
func (i *InitCommand) Description() string {
	return "Generate or improve AGENTS.md with intelligent codebase analysis"
}

// Execute runs the init command using the LLM to analyze the codebase
func (i *InitCommand) Execute(args []string, chatAgent *agent.Agent) error {
	fmt.Println("[tool] Analyzing codebase and generating AGENTS.md...")
	fmt.Println("[read] The agent will explore your project and create/update AGENTS.md")
	fmt.Println()

	// Check for existing context files to include as reference
	existingFiles := i.discoverExistingContextFiles()

	// Build the prompt for the LLM
	prompt := i.buildInitPrompt(existingFiles)

	// Inject the prompt so the agent processes it like user input
	// This allows the agent to use its tools to explore and write
	err := chatAgent.InjectInputContext(prompt)
	if err != nil {
		return fmt.Errorf("failed to inject init prompt: %w", err)
	}

	return nil
}

// discoverExistingContextFiles finds existing context/instruction files
func (i *InitCommand) discoverExistingContextFiles() []string {
	contextFiles := []string{
		"AGENTS.md",
		"CLAUDE.md",
		".claude/project.md",
		".cursorrules",
		".cursor/rules",
		".github/copilot-instructions.md",
		"README.md",
	}

	var found []string
	for _, file := range contextFiles {
		if _, err := os.Stat(file); err == nil {
			found = append(found, file)
		}
	}
	return found
}

// buildInitPrompt creates the prompt for generating AGENTS.md
func (i *InitCommand) buildInitPrompt(existingFiles []string) string {
	var sb strings.Builder

	sb.WriteString(`Analyze this codebase and create or improve the AGENTS.md file, which will be given to future instances of this AI coding agent to operate effectively in this repository.

## What to include

1. **Build, Test, and Development Commands**
   - How to build the project
   - How to run tests (including running a single test)
   - How to lint/format code
   - Any other commonly needed development commands

2. **High-Level Architecture**
   - The "big picture" structure that requires reading multiple files to understand
   - Key abstractions and how they relate
   - Important patterns or conventions used
   - Module/package organization

3. **Project-Specific Conventions**
   - Naming conventions (if non-standard)
   - File organization patterns
   - Any non-obvious workflow requirements

## Important Guidelines

- **Be concise** - Aim for under 200-300 lines. Less is more.
- **Don't repeat obvious things** - Skip generic advice like "write tests" or "handle errors"
- **Don't list every file** - Focus on architecture, not directory listings
- **Point to files, don't copy** - Use file references for details that might change
- **Only include what's universally useful** - Task-specific info should be in separate docs
- **Be accurate** - Only include information you can verify from the actual codebase

`)

	// Add info about existing context files
	if len(existingFiles) > 0 {
		sb.WriteString("## Existing Context Files\n\n")
		sb.WriteString("The following files already exist and contain useful context. Read them and incorporate important parts:\n\n")
		for _, file := range existingFiles {
			sb.WriteString(fmt.Sprintf("- `%s`\n", file))
		}
		sb.WriteString("\n")
	}

	// Add specific instructions based on what exists
	hasAgentsMd := false
	for _, f := range existingFiles {
		if f == "AGENTS.md" {
			hasAgentsMd = true
			break
		}
	}

	if hasAgentsMd {
		sb.WriteString(`## Task

Read the existing AGENTS.md and suggest improvements. Look for:
- Outdated information
- Missing critical commands or architecture details
- Verbose sections that could be trimmed
- Generic advice that could be removed
- Important info from other context files that should be incorporated

Update AGENTS.md with your improvements. Focus on making it more useful and concise.
`)
	} else {
		sb.WriteString(`## Task

Explore the codebase to understand:
- Project type and language(s)
- Build system and dependencies (go.mod, package.json, Cargo.toml, etc.)
- Test framework and how to run tests
- Key entry points and module structure
- Any existing documentation or context files

Then create a new AGENTS.md file with the essential information for working in this codebase.
`)
	}

	sb.WriteString(`
## Output

Write the final AGENTS.md file directly using the write_file tool. Do NOT show me the content - just write it and confirm completion.

Start by reading key files to understand the project, then write AGENTS.md.`)

	return sb.String()
}
