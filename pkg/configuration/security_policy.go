package configuration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SecurityPolicyAction represents the action to take when a rule matches
type SecurityPolicyAction string

const (
	PolicyAllow  SecurityPolicyAction = "allow"
	PolicyDeny   SecurityPolicyAction = "deny"
	PolicyPrompt SecurityPolicyAction = "prompt"
)

// IsValid returns true if the action is a recognized SecurityPolicyAction
// value: "allow", "deny", "prompt", or empty string.
func (a SecurityPolicyAction) IsValid() bool {
	switch a {
	case PolicyAllow, PolicyDeny, PolicyPrompt, "":
		return true
	default:
		return false
	}
}

// SecurityPolicy defines workspace-level security rules
type SecurityPolicy struct {
	DefaultAction  string         `json:"default_action,omitempty"`
	Rules          []SecurityRule `json:"rules,omitempty"`
	AllowedPaths   []string       `json:"allowed_paths,omitempty"`
	DeniedPaths    []string       `json:"denied_paths,omitempty"`
	DeniedCommands []string       `json:"denied_commands,omitempty"`
	MaxRiskLevel   string         `json:"max_risk_level,omitempty"`
}

// SecurityRule defines a pattern-based rule for command evaluation
type SecurityRule struct {
	Pattern string `json:"pattern"`
	Action  string `json:"action"`
	Reason  string `json:"reason,omitempty"`
}

// LoadSecurityPolicy reads .sprout/security-policy.json from workspaceRoot.
// If the file doesn't exist, returns nil, nil (no error).
// Validates that actions and risk levels are recognized values, and
// normalizes AllowedPaths and DeniedPaths via filepath.Clean.
func LoadSecurityPolicy(workspaceRoot string) (*SecurityPolicy, error) {
	path := filepath.Join(workspaceRoot, ConfigDirName, "security-policy.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read security policy: %w", err)
	}

	var policy SecurityPolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("parse security policy: %w", err)
	}

	// Validate DefaultAction
	if policy.DefaultAction != "" && !SecurityPolicyAction(policy.DefaultAction).IsValid() {
		return nil, fmt.Errorf("invalid default_action %q", policy.DefaultAction)
	}

	// Validate MaxRiskLevel
	if policy.MaxRiskLevel != "" {
		switch strings.ToLower(strings.TrimSpace(policy.MaxRiskLevel)) {
		case "safe", "caution", "dangerous":
			// valid
		default:
			return nil, fmt.Errorf("invalid max_risk_level %q", policy.MaxRiskLevel)
		}
	}

	// Validate rule actions
	for i, rule := range policy.Rules {
		if !SecurityPolicyAction(rule.Action).IsValid() {
			return nil, fmt.Errorf("rule %d has invalid action %q", i, rule.Action)
		}
	}

	// Normalize paths via filepath.Clean
	for i, p := range policy.AllowedPaths {
		policy.AllowedPaths[i] = filepath.Clean(p)
	}
	for i, p := range policy.DeniedPaths {
		policy.DeniedPaths[i] = filepath.Clean(p)
	}

	return &policy, nil
}

// DefaultSecurityPolicy returns a conservative default policy
func DefaultSecurityPolicy() *SecurityPolicy {
	return &SecurityPolicy{
		DefaultAction: "prompt",
		MaxRiskLevel:  "safe",
	}
}

// getDefaultAction returns the effective default action for the policy.
// If DefaultAction is empty or whitespace-only, returns PolicyPrompt.
func (p *SecurityPolicy) getDefaultAction() SecurityPolicyAction {
	defaultAction := strings.TrimSpace(p.DefaultAction)
	if defaultAction == "" {
		return PolicyPrompt
	}
	return SecurityPolicyAction(defaultAction)
}

// evaluateWithRule evaluates command against rules and returns the matched
// action and the matched rule (nil if no rule matched).
func (p *SecurityPolicy) evaluateWithRule(command string) (SecurityPolicyAction, *SecurityRule) {
	baseCmd := ""
	if spaceIdx := strings.Index(command, " "); spaceIdx > 0 {
		baseCmd = command[:spaceIdx]
	}
	for i := range p.Rules {
		rule := &p.Rules[i]
		// Try full command match first
		if matched, err := filepath.Match(rule.Pattern, command); err == nil && matched {
			return SecurityPolicyAction(rule.Action), rule
		}
		// Try base command match
		if baseCmd != "" {
			if matched, err := filepath.Match(rule.Pattern, baseCmd); err == nil && matched {
				return SecurityPolicyAction(rule.Action), rule
			}
		}
	}
	return p.getDefaultAction(), nil
}

// Evaluate checks the command against rules in order. For each rule, it first
// tries matching the full command string, then the base command (first word).
// First matching rule wins. This means a rule like {Pattern: "git", Action: "deny"}
// will match "git commit" via base matching before a later rule
// {Pattern: "git commit*", Action: "allow"} gets evaluated.
//
// Place more specific patterns before broader ones to get the intended behavior.
// If no rule matches, returns DefaultAction converted to SecurityPolicyAction.
// If DefaultAction is empty, defaults to "prompt".
func (p *SecurityPolicy) Evaluate(command string) SecurityPolicyAction {
	if p == nil {
		return PolicyPrompt
	}
	action, _ := p.evaluateWithRule(command)
	return action
}

// IsPathAllowed checks whether the given path is allowed by this policy.
// If AllowedPaths is empty, returns true (no restrictions).
// Otherwise checks if the cleaned path starts with any of the allowed paths.
// Also ensures the path is not in DeniedPaths.
//
// NOTE: This uses filepath.Clean for normalization but does NOT resolve
// symlinks. A symlink from an allowed path to a system directory could
// bypass this check.
func (p *SecurityPolicy) IsPathAllowed(path string) bool {
	if p == nil {
		return true
	}

	cleanPath := filepath.Clean(path)

	// Check denied paths first — they always win
	if p.IsPathDenied(cleanPath) {
		return false
	}

	// If no allowlist is set, everything (that wasn't denied) is allowed
	if len(p.AllowedPaths) == 0 {
		return true
	}

	// Check against allowlist
	for _, allowed := range p.AllowedPaths {
		cleanAllowed := filepath.Clean(allowed)
		if cleanPath == cleanAllowed || strings.HasPrefix(cleanPath, cleanAllowed+string(filepath.Separator)) {
			return true
		}
	}

	return false
}

// IsPathDenied checks if the cleaned path starts with any of the denied paths.
// Handles parent traversal: if "/etc" is denied, "/etc/passwd" is also denied.
//
// NOTE: This uses filepath.Clean for normalization but does NOT resolve
// symlinks. A symlink from an allowed path to a system directory could
// bypass this check.
func (p *SecurityPolicy) IsPathDenied(path string) bool {
	if p == nil {
		return false
	}

	cleanPath := filepath.Clean(path)

	for _, denied := range p.DeniedPaths {
		cleanDenied := filepath.Clean(denied)
		if cleanPath == cleanDenied || strings.HasPrefix(cleanPath, cleanDenied+string(filepath.Separator)) {
			return true
		}
	}

	return false
}

// IsCommandDenied checks if the base command (first word) matches any entry
// in DeniedCommands (case-insensitive).
func (p *SecurityPolicy) IsCommandDenied(command string) bool {
	if p == nil {
		return false
	}

	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}

	base := strings.ToLower(fields[0])

	for _, denied := range p.DeniedCommands {
		if strings.ToLower(denied) == base {
			return true
		}
	}

	return false
}

// MaxAllowedRisk converts MaxRiskLevel string to SecurityRisk int equivalent.
// "safe"=0, "caution"=1, "dangerous"=2. Defaults to 0 if empty or unrecognized.
func (p *SecurityPolicy) MaxAllowedRisk() int {
	if p == nil || p.MaxRiskLevel == "" {
		return 0 // safe
	}

	switch strings.ToLower(strings.TrimSpace(p.MaxRiskLevel)) {
	case "safe":
		return 0
	case "caution":
		return 1
	case "dangerous":
		return 2
	default:
		return 0 // safe
	}
}

// CombinedAssessment holds the result of combining the security classifier
// with the workspace security policy.
//
// IMPORTANT: To avoid a circular import (configuration -> agent_tools ->
// configuration), the ClassifierResult field is a generic map instead of a
// typed reference to agent_tools.SecurityResult. Callers that need the typed
// result should use CombinedSecurityAssessmentWithClassifier below.
type CombinedAssessment struct {
	ClassifierRisk     int    // SecurityRisk value (0=safe, 1=caution, 2=dangerous)
	ClassifierBlocked  bool   // ShouldBlock from the classifier
	ClassifierPrompt   bool   // ShouldPrompt from the classifier
	ClassifierCategory string // RiskCategory string
	PolicyAction       SecurityPolicyAction
	PolicyRule         *SecurityRule // the matched rule, if any
	PathAllowed        bool
	CommandDenied      bool
	OverrideAction     string // reason if policy overrides classifier
}

// CombinedSecurityAssessment evaluates a tool call against the security policy.
//
// classifierRisk is the risk level from the security classifier (0=safe,
// 1=caution, 2=dangerous). classifierBlocked indicates whether the classifier
// says the call should be blocked. classifierPrompt indicates whether the
// classifier says the call should prompt the user. classifierCategory is the
// risk category string from the classifier (e.g. "read-only", "file-write").
//
// This interface avoids a circular import between configuration and agent_tools.
func CombinedSecurityAssessment(
	toolName string,
	args map[string]interface{},
	policy *SecurityPolicy,
	classifierRisk int,
	classifierBlocked bool,
	classifierPrompt bool,
	classifierCategory string,
) *CombinedAssessment {
	assessment := &CombinedAssessment{
		ClassifierRisk:     classifierRisk,
		ClassifierBlocked:  classifierBlocked,
		ClassifierPrompt:   classifierPrompt,
		ClassifierCategory: classifierCategory,
		PolicyAction:       PolicyPrompt,
		PathAllowed:        true,
		CommandDenied:      false,
	}

	if policy == nil {
		return assessment
	}

	// For shell_command, evaluate against policy rules and denied commands
	if toolName == "shell_command" {
		if cmdRaw, ok := args["command"].(string); ok && cmdRaw != "" {
			cmd := strings.TrimSpace(cmdRaw)

			// Check denied commands
			if policy.IsCommandDenied(cmd) {
				assessment.CommandDenied = true
			}

			// Evaluate against policy rules (first match wins, store the rule)
			action := policy.Evaluate(cmd)
			assessment.PolicyAction = action

			// Find and store the matched rule
			for i := range policy.Rules {
				rule := &policy.Rules[i]
				matched, err := filepath.Match(rule.Pattern, cmd)
				if err == nil && matched {
					assessment.PolicyRule = rule
					break
				}
				// Also try matching the base command
				base := strings.Fields(cmd)
				if len(base) > 0 {
					matched, err = filepath.Match(rule.Pattern, base[0])
					if err == nil && matched {
						assessment.PolicyRule = rule
						break
					}
				}
			}

			// Determine override: if policy is stricter than classifier, note it
			if assessment.PolicyAction == PolicyDeny && !classifierBlocked {
				assessment.OverrideAction = "policy denies command despite classifier allowing it"
			} else if assessment.PolicyAction == PolicyAllow && classifierBlocked {
				assessment.OverrideAction = "policy allows command despite classifier flagging it"
			}

			// Check max risk level — if classifier risk exceeds policy max, escalate
			maxAllowed := policy.MaxAllowedRisk()
			if classifierRisk > maxAllowed {
				msg := fmt.Sprintf("classifier risk level %d exceeds policy max %d", classifierRisk, maxAllowed)
				if assessment.OverrideAction != "" {
					assessment.OverrideAction += "; " + msg
				} else {
					assessment.OverrideAction = msg
				}
			}
		}
	} else {
		// For non-shell commands, use default policy action
		defaultAction := strings.TrimSpace(policy.DefaultAction)
		if defaultAction == "" {
			assessment.PolicyAction = PolicyPrompt
		} else {
			assessment.PolicyAction = SecurityPolicyAction(defaultAction)
		}
	}

	// For file operations, check path allowlist/denylist
	if toolName == "write_file" || toolName == "edit_file" ||
		toolName == "write_structured_file" || toolName == "patch_structured_file" {
		if pathRaw, ok := args["path"].(string); ok && pathRaw != "" {
			assessment.PathAllowed = policy.IsPathAllowed(pathRaw)
			if !assessment.PathAllowed {
				msg := "path denied by workspace security policy"
				if assessment.OverrideAction != "" {
					assessment.OverrideAction += "; " + msg
				} else {
					assessment.OverrideAction = msg
				}
			}
		}
	}

	return assessment
}
