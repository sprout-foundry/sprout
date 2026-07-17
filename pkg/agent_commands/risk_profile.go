package commands

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// RiskProfileCommand implements /risk-profile for inspecting or switching
// the SP-058 shell-command gating profile mid-session. The CLI flag
// --risk-profile sets the same field at startup; this command lets the
// user adjust without restarting.
type RiskProfileCommand struct{}

func (c *RiskProfileCommand) Name() string {
	return "risk-profile"
}

// SafeDuringSteer returns true - /risk-profile is config change, no turn interaction
func (c *RiskProfileCommand) SafeDuringSteer() bool {
	return true
}

func (c *RiskProfileCommand) Description() string {
	return "Show or change the shell-command risk profile (readonly|cautious|default|permissive|unrestricted)"
}

func (c *RiskProfileCommand) Usage() string {
	return strings.Join([]string{
		"/risk-profile               Show the active profile and the list of built-ins.",
		"/risk-profile <name>        Apply <name> as a session override (does not persist).",
		"/risk-profile clear         Drop the override; fall back to config.risk_profile or 'default'.",
		"",
		"Profiles (strictest → loosest):",
		"  readonly       Only read operations; writes are blocked.",
		"  cautious       Most operations prompt; subagent writes blocked.",
		"  default        Built-in defaults (matches the historical behavior).",
		"  permissive     High trust; almost everything passes without prompting.",
		"  unrestricted   No risk cascade gating; only Critical (rm -rf /, fork bomb) blocks.",
		"",
		"Persona-defined rules still override the profile (e.g. EA's autonomy rules).",
		"See docs/SECURITY.md#risk-profiles for the full behavior matrix.",
	}, "\n")
}

func (c *RiskProfileCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}

	if len(args) == 0 {
		return c.show(chatAgent)
	}

	first := strings.ToLower(strings.TrimSpace(args[0]))
	switch first {
	case "clear", "none", "default-config":
		chatAgent.SetRiskProfileOverride("")
		active := chatAgent.GetActiveRiskProfile()
		console.GlyphSuccess.Printf("Cleared risk-profile override; active profile is now %q", string(active))
		return nil
	case "list", "show":
		return c.show(chatAgent)
	}

	if !configuration.IsValidRiskProfile(first) {
		return fmt.Errorf("unknown risk profile %q (valid: %s)", args[0], strings.Join(builtinProfileNames(), ", "))
	}

	chatAgent.SetRiskProfileOverride(configuration.RiskProfile(first))
	console.GlyphSuccess.Printf("Risk-profile override set to %q (session only — does not persist)", first)
	return nil
}

func (c *RiskProfileCommand) show(chatAgent *agent.Agent) error {
	active := chatAgent.GetActiveRiskProfile()
	console.GlyphInfo.Printf("Active risk profile: %q", string(active))

	cfg := chatAgent.GetConfig()
	if cfg != nil && cfg.RiskProfile != "" {
		fmt.Printf("   config.risk_profile: %q\n", cfg.RiskProfile)
	} else {
		fmt.Println("   config.risk_profile: (unset — falls back to \"default\")")
	}
	fmt.Println("   Built-in profiles:")
	for _, name := range builtinProfileNames() {
		marker := "  "
		if name == string(active) {
			marker = "* "
		}
		fmt.Printf("     %s%s\n", marker, name)
	}
	if cfg != nil && len(cfg.RiskProfiles) > 0 {
		userDefined := make([]string, 0, len(cfg.RiskProfiles))
		for k := range cfg.RiskProfiles {
			userDefined = append(userDefined, k)
		}
		sort.Strings(userDefined)
		fmt.Println("   User-defined profiles (from config.risk_profiles):")
		for _, name := range userDefined {
			marker := "  "
			if name == string(active) {
				marker = "* "
			}
			fmt.Printf("     %s%s\n", marker, name)
		}
	}
	return nil
}

func builtinProfileNames() []string {
	return []string{
		string(configuration.RiskProfileReadonly),
		string(configuration.RiskProfileCautious),
		string(configuration.RiskProfileDefault),
		string(configuration.RiskProfilePermissive),
		string(configuration.RiskProfileUnrestricted),
	}
}
