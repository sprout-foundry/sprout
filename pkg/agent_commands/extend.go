package commands

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// Prompter abstracts user input for the extend command flow.
type Prompter interface {
	Prompt(prompt string) (string, error)
}

// AgentPrompter uses the agent's stdin for reading responses.
type AgentPrompter struct {
	agent *agent.Agent
}

// NewAgentPrompter creates an AgentPrompter for the given agent.
func NewAgentPrompter(a *agent.Agent) *AgentPrompter {
	return &AgentPrompter{agent: a}
}

func (p *AgentPrompter) Prompt(prompt string) (string, error) {
	fmt.Print(prompt + ": ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return "", nil
	}
	return strings.TrimSpace(scanner.Text()), nil
}

// ExtendCommand guides the user through creating a custom agent role via a
// 7-question interactive flow.
type ExtendCommand struct {
	roleManager *configuration.RoleManager // if nil, created in Execute
	prompter    Prompter                   // if nil, created in Execute
}

// Name returns the command name.
func (c *ExtendCommand) Name() string {
	return "extend"
}

// Description returns the command description.
func (c *ExtendCommand) Description() string {
	return "Create or modify agent roles with guided configuration"
}

// Execute runs the 7-question guided flow to create a custom agent role.
func (c *ExtendCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return fmt.Errorf("[extend] agent not available")
	}

	// Resolve role manager
	rm := c.roleManager
	if rm == nil {
		cm := chatAgent.GetConfigManager()
		if cm != nil {
			rm = cm.GetRoleManager()
		}
		if rm == nil {
			return fmt.Errorf("[extend] role manager not available")
		}
	}

	// Resolve prompter
	p := c.prompter
	if p == nil {
		p = NewAgentPrompter(chatAgent)
	}

	fmt.Println("\n=== Extend: Create a New Agent Role ===")
	fmt.Println("Press Enter with empty input to cancel at any step")
	fmt.Println()

	// Q1: Role name (required)
	name, err := p.Prompt("1/7 Role name (required)")
	if err != nil {
		return fmt.Errorf("[extend] input error: %w", err)
	}
	if name == "" {
		fmt.Println("[extend] cancelled (empty role name)")
		return nil
	}
	if !configuration.IsValidRoleName(name) {
		return fmt.Errorf("[extend] invalid role name %q: only alphanumeric, hyphens, underscores, and dots allowed", name)
	}

	// Q2: Description
	desc, err := p.Prompt("2/7 Description")
	if err != nil {
		return fmt.Errorf("[extend] input error: %w", err)
	}

	// Q3: System prompt
	sysPrompt, err := p.Prompt("3/7 System prompt")
	if err != nil {
		return fmt.Errorf("[extend] input error: %w", err)
	}

	// Q4: Allowed tools
	toolsInput, err := p.Prompt("4/7 Allowed tools (comma-separated, or 'all')")
	if err != nil {
		return fmt.Errorf("[extend] input error: %w", err)
	}
	var allowedTools []string
	if strings.ToLower(toolsInput) != "all" && toolsInput != "" {
		for _, t := range strings.Split(toolsInput, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				allowedTools = append(allowedTools, t)
			}
		}
	}

	// Q5: Provider
	provider, err := p.Prompt("5/7 Provider override (or 'default')")
	if err != nil {
		return fmt.Errorf("[extend] input error: %w", err)
	}
	if strings.ToLower(provider) == "default" {
		provider = ""
	}

	// Q6: Model
	model, err := p.Prompt("6/7 Model override (or 'default')")
	if err != nil {
		return fmt.Errorf("[extend] input error: %w", err)
	}
	if strings.ToLower(model) == "default" {
		model = ""
	}

	// Q7: Max iterations
	maxIterStr, err := p.Prompt("7/7 Max iterations (or 'default')")
	if err != nil {
		return fmt.Errorf("[extend] input error: %w", err)
	}
	maxIterations := 0
	if strings.ToLower(maxIterStr) != "default" && maxIterStr != "" {
		v, parseErr := strconv.Atoi(maxIterStr)
		if parseErr != nil {
			return fmt.Errorf("[extend] invalid max iterations: %s", maxIterStr)
		}
		if v < 0 {
			return fmt.Errorf("[extend] max iterations must be non-negative, got %d", v)
		}
		maxIterations = v
	}

	// Check for duplicate role name before proceeding
	if rm.Exists(name) {
		overwrite, err := p.Prompt("Role already exists, overwrite? (y/n)")
		if err != nil {
			return fmt.Errorf("[extend] input error: %w", err)
		}
		if !strings.EqualFold(overwrite, "y") && !strings.EqualFold(overwrite, "yes") {
			fmt.Println("[extend] cancelled (overwrite not confirmed)")
			return nil
		}
	}

	// Show summary
	fmt.Println("\n=== Role Summary ===")
	fmt.Printf("Name:            %s\n", name)
	fmt.Printf("Description:     %s\n", desc)
	fmt.Printf("System Prompt:   %s\n", sysPrompt)
	if len(allowedTools) > 0 {
		fmt.Printf("Allowed Tools:     %s\n", strings.Join(allowedTools, ", "))
	} else {
		fmt.Println("Allowed Tools:     all")
	}
	fmt.Printf("Provider:        %s\n", displayValue(provider))
	fmt.Printf("Model:           %s\n", displayValue(model))
	fmt.Printf("Max Iterations:  %s\n", displayValue(strconv.Itoa(maxIterations)))

	// Confirm
	confirm, err := p.Prompt("Save this role? (y/n)")
	if err != nil {
		return fmt.Errorf("[extend] input error: %w", err)
	}
	if !strings.EqualFold(confirm, "y") && !strings.EqualFold(confirm, "yes") {
		fmt.Println("[extend] cancelled (not confirmed)")
		return nil
	}

	// Build RoleConfig and save
	role := configuration.RoleConfig{
		Name:        name,
		Description: desc,
		SystemPrompt: sysPrompt,
		Tools: configuration.RoleToolsConfig{
			AllowedTools: allowedTools,
		},
		Provider: provider,
		Model:    model,
		Constraints: configuration.RoleConstraints{
			MaxIterations: maxIterations,
		},
	}

	// Save the role. The source parameter is currently unused by Save(),
	// so we pass an empty string.
	if err := rm.Save(role, ""); err != nil {
		return fmt.Errorf("[extend] failed to save role: %w", err)
	}
	fmt.Printf("\n✓ Role %q saved successfully!\n", name)
	return nil
}

func displayValue(v string) string {
	if v == "" {
		return "(default)"
	}
	return v
}
