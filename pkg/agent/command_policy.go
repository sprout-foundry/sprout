package agent

import (
	"path"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// commandPolicySeverity ranks policy actions by severity.
// Higher value = more restrictive. Used to select the most restrictive
// action when multiple subcommands match different rules.
var commandPolicySeverity = map[configuration.CommandPolicyAction]int{
	configuration.CommandPolicyDeny:  3,
	configuration.CommandPolicyAsk:   2,
	configuration.CommandPolicyAllow: 1,
}

// EvaluateCommandPolicy checks user-defined command policies against a
// shell command. Returns the matched action, the matched pattern, and
// whether a match was found.
//
// Algorithm:
//  1. Split the command on &&, ||, ;, | (quote-aware) using SplitChainedCommand.
//  2. For each subcommand, check rules in order (first-match-wins).
//  3. Pattern matching uses Go path.Match (glob), case-insensitive.
//  4. Return the highest-severity action across all subcommands:
//     deny > ask > allow.
//  5. If no subcommand matched any rule, return ("", "", false).
func EvaluateCommandPolicy(
	command string,
	policies *configuration.CommandPolicies,
) (configuration.CommandPolicyAction, string, bool) {
	if policies == nil || len(policies.Rules) == 0 {
		return "", "", false
	}

	parts := tools.SplitChainedCommand(command)
	if len(parts) == 0 {
		return "", "", false
	}

	var bestAction configuration.CommandPolicyAction
	var bestPattern string

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Lowercase for case-insensitive matching.
		partLower := strings.ToLower(part)

		// First-match-wins: check rules in order.
		for _, rule := range policies.Rules {
			rulePatternLower := strings.ToLower(rule.Pattern)
			matched, err := path.Match(rulePatternLower, partLower)
			if err != nil {
				continue // Invalid glob pattern, skip this rule.
			}
			if matched {
				// Keep the most restrictive action across all subcommands.
				if commandPolicySeverity[rule.Action] > commandPolicySeverity[bestAction] {
					bestAction = rule.Action
					bestPattern = rule.Pattern
				}
				break // First-match-wins for this subcommand.
			}
		}
	}

	if bestAction == "" {
		return "", "", false
	}

	return bestAction, bestPattern, true
}
