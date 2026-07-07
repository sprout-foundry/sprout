// Package agent — shell command parsing and classification.
//
// This file provides pure-function utilities for splitting shell commands
// into logical parts and classifying each part by destructive intent.
// It contains no Agent wiring, no broker, no events, and no UI.
package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// ---------------------------------------------------------------------------
// CommandKind
// ---------------------------------------------------------------------------

// CommandKind categorizes a shell command part by its destructive intent.
type CommandKind string

const (
	CommandKindRm            CommandKind = "rm"
	CommandKindGitPush       CommandKind = "git_push"
	CommandKindGitReset      CommandKind = "git_reset"
	CommandKindKubectl       CommandKind = "kubectl"
	CommandKindDocker        CommandKind = "docker"
	CommandKindChmod         CommandKind = "chmod"
	CommandKindChown         CommandKind = "chown"
	CommandKindWriteRedirect CommandKind = "write_redirect"
	CommandKindHttpPost      CommandKind = "http_post"
	CommandKindUnknown       CommandKind = "unknown"
)

// ---------------------------------------------------------------------------
// ShellPart
// ---------------------------------------------------------------------------

// ShellPart represents one logical command in a potentially-pipelined shell line.
type ShellPart struct {
	ID       string      // stable ID for UI tracking (e.g. "part-0")
	Text     string      // raw text of this part (e.g. "rm -rf foo")
	Kind     CommandKind // classified kind
	Semantic string      // human-readable description (e.g. "Recursively delete foo")
}

// ---------------------------------------------------------------------------
// ShellProposal
// ---------------------------------------------------------------------------

// ShellProposal is a parsed shell command submitted for approval.
type ShellProposal struct {
	Command   string                  // original full command
	Parts     []ShellPart             // split + classified
	RiskLevel configuration.RiskLevel // folded from the most-destructive part
}

// ---------------------------------------------------------------------------
// Classification patterns
// ---------------------------------------------------------------------------

// classificationPatterns holds compiled regex patterns and their associated
// CommandKind. Patterns are matched in order; the first match wins.
type classificationPattern struct {
	pattern *regexp.Regexp
	kind    CommandKind
}

// compileClassificationPatterns builds the pattern table at package init.
//
// The rm pattern requires destructive flags (r/R or f/F in the flag word).
// Bare "rm foo" does NOT match — it would be a false positive.
var classificationPatterns = []classificationPattern{
	// rm with recursive or force flags (the flag group is REQUIRED).
	{regexp.MustCompile(`(?i)^rm\s+(-[a-zA-Z]*[rRfF][a-zA-Z]*\s+)+`), CommandKindRm},
	// git push --force or git push -f
	{regexp.MustCompile(`(?i)^git\s+push\s+(--force|-f)\b`), CommandKindGitPush},
	// git reset --hard
	{regexp.MustCompile(`(?i)^git\s+reset\s+--hard\b`), CommandKindGitReset},
	// kubectl delete
	{regexp.MustCompile(`(?i)^kubectl\s+delete\b`), CommandKindKubectl},
	// docker rm or docker system prune
	{regexp.MustCompile(`(?i)^docker\s+(rm|system\s+prune)\b`), CommandKindDocker},
	// chmod with octal mode (3+ digits)
	{regexp.MustCompile(`(?i)^chmod\s+(-[a-zA-Z]*\s+)?[0-7]*[0-7][0-7][0-7]`), CommandKindChmod},
	// chown to root (owner is root, or group is root)
	{regexp.MustCompile(`(?i)^chown\s+(root|[^:]+\s*:\s*root)\b`), CommandKindChown},
	// Write redirect (>) — matched by a helper that excludes >> (append).
	{regexp.MustCompile(`>`), CommandKindWriteRedirect},
	// HTTP POST via curl or wget.
	{regexp.MustCompile(`(?i)(curl\s+.*-X\s+POST|wget\s+--post)`), CommandKindHttpPost},
}

// ClassifyShellSegment returns the CommandKind for a single shell segment
// by matching it against the classification pattern table.
func ClassifyShellSegment(segment string) CommandKind {
	segment = strings.TrimSpace(segment)

	for _, cp := range classificationPatterns {
		if !cp.pattern.MatchString(segment) {
			continue
		}
		// If this is the write_redirect pattern, verify it's not >> (append).
		if cp.kind == CommandKindWriteRedirect && isOnlyAppendRedirect(segment) {
			continue
		}
		return cp.kind
	}
	return CommandKindUnknown
}

// isOnlyAppendRedirect reports whether all > characters in segment are
// part of >> (append redirect). If there's any standalone > that's not
// followed by another >, it's a write redirect.
func isOnlyAppendRedirect(segment string) bool {
	for i := 0; i < len(segment); i++ {
		if segment[i] == '>' {
			// Check if this is >> (append).
			if i+1 < len(segment) && segment[i+1] == '>' {
				i++ // skip the second >
				continue
			}
			// Found a standalone > that's not part of >>.
			return false
		}
	}
	return true
}

// ClassifyShellSegmentWithSemantic returns the CommandKind and a brief
// human-readable description for the segment.
func ClassifyShellSegmentWithSemantic(segment string) (CommandKind, string) {
	segment = strings.TrimSpace(segment)
	kind := ClassifyShellSegment(segment)
	switch kind {
	case CommandKindRm:
		rest := extractRmTarget(segment)
		if rest != "" {
			return kind, fmt.Sprintf("Recursively delete: %s", rest)
		}
		return kind, "Recursively delete target"
	case CommandKindGitPush:
		return kind, "Force-push to remote git repository"
	case CommandKindGitReset:
		return kind, "Hard reset git repository"
	case CommandKindKubectl:
		return kind, "Delete Kubernetes resource"
	case CommandKindDocker:
		return kind, "Remove Docker container or prune system"
	case CommandKindChmod:
		return kind, "Change file permissions"
	case CommandKindChown:
		return kind, "Change file ownership to root"
	case CommandKindWriteRedirect:
		return kind, "Redirect output to file (overwrite)"
	case CommandKindHttpPost:
		return kind, "HTTP POST request (sends data to remote server)"
	default:
		return kind, "Unknown / non-destructive command"
	}
}

// extractRmTarget pulls the target argument(s) after the rm flags.
func extractRmTarget(segment string) string {
	// Strip "rm" and its flags, then trim.
	s := strings.TrimPrefix(segment, "rm")
	s = strings.TrimSpace(s)
	// Skip all leading flag tokens (start with -).
	for s != "" && s[0] == '-' {
		idx := strings.IndexAny(s, " ")
		if idx == -1 {
			return "" // only flags, no target
		}
		s = strings.TrimLeft(s[idx:], " ")
	}
	return strings.TrimSpace(s)
}

// ---------------------------------------------------------------------------
// Shell splitting
// ---------------------------------------------------------------------------

// SplitShellIntoParts tokenizes a shell command at &&, ||, ;, and |
// boundaries, respecting balanced parentheses and quoted strings.
//
// Inside quotes (single or double) all metacharacters are treated as
// literal text. Inside parentheses (depth > 0), the pipe character is
// treated as literal.
//
// Empty input produces an empty slice. Consecutive separators with no
// content are skipped. Each part is trimmed of leading/trailing whitespace.
func SplitShellIntoParts(cmd string) []ShellPart {
	if strings.TrimSpace(cmd) == "" {
		return nil
	}

	var rawParts []string
	var sb strings.Builder
	depth := 0 // paren depth
	inSingleQuote := false
	inDoubleQuote := false
	i := 0

	for i < len(cmd) {
		ch := cmd[i]

		// Handle quote toggling.
		if ch == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
			sb.WriteByte(ch)
			i++
			continue
		}
		if ch == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			sb.WriteByte(ch)
			i++
			continue
		}

		if inSingleQuote || inDoubleQuote {
			// Inside quotes: everything is literal.
			sb.WriteByte(ch)
			i++
			continue
		}

		// Track paren depth.
		if ch == '(' {
			depth++
			sb.WriteByte(ch)
			i++
			continue
		}
		if ch == ')' {
			if depth > 0 {
				depth--
			}
			sb.WriteByte(ch)
			i++
			continue
		}

		// Check for metacharacters.
		if depth == 0 {
			// && separator
			if ch == '&' && i+1 < len(cmd) && cmd[i+1] == '&' {
				rawParts = append(rawParts, sb.String())
				sb.Reset()
				i += 2
				continue
			}
			// || separator
			if ch == '|' && i+1 < len(cmd) && cmd[i+1] == '|' {
				rawParts = append(rawParts, sb.String())
				sb.Reset()
				i += 2
				continue
			}
			// ; separator
			if ch == ';' {
				rawParts = append(rawParts, sb.String())
				sb.Reset()
				i++
				continue
			}
			// | separator (pipe) — only when depth == 0.
			if ch == '|' {
				rawParts = append(rawParts, sb.String())
				sb.Reset()
				i++
				continue
			}
		}

		sb.WriteByte(ch)
		i++
	}

	// Flush remaining content.
	if strings.TrimSpace(sb.String()) != "" {
		rawParts = append(rawParts, sb.String())
	}

	// Build ShellPart slice with trimmed text and sequential IDs.
	parts := make([]ShellPart, 0, len(rawParts))
	for _, raw := range rawParts {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		parts = append(parts, ShellPart{
			ID:   fmt.Sprintf("part-%d", len(parts)),
			Text: trimmed,
		})
	}

	return parts
}

// ---------------------------------------------------------------------------
// Risk helpers
// ---------------------------------------------------------------------------

// kindRiskLevel maps a CommandKind to its corresponding RiskLevel.
func kindRiskLevel(kind CommandKind) configuration.RiskLevel {
	switch kind {
	case CommandKindRm, CommandKindGitReset, CommandKindKubectl:
		return configuration.RiskLevelCritical
	case CommandKindDocker, CommandKindGitPush:
		return configuration.RiskLevelHigh
	case CommandKindChmod, CommandKindChown, CommandKindWriteRedirect, CommandKindHttpPost:
		return configuration.RiskLevelMedium
	default:
		return configuration.RiskLevelLow
	}
}

// ---------------------------------------------------------------------------
// NewShellProposal
// ---------------------------------------------------------------------------

// NewShellProposal creates a ShellProposal by splitting the command into
// parts, classifying each part (kind + semantic), and folding the overall
// RiskLevel from the most-destructive part.
func NewShellProposal(cmd string) ShellProposal {
	parts := SplitShellIntoParts(cmd)

	if len(parts) == 0 {
		return ShellProposal{
			Command:   cmd,
			Parts:     nil,
			RiskLevel: configuration.RiskLevelLow,
		}
	}

	var maxRisk configuration.RiskLevel = configuration.RiskLevelLow
	for i := range parts {
		kind, semantic := ClassifyShellSegmentWithSemantic(parts[i].Text)
		parts[i].Kind = kind
		parts[i].Semantic = semantic
		partRisk := kindRiskLevel(kind)
		if partRisk.Rank() > maxRisk.Rank() {
			maxRisk = partRisk
		}
	}

	return ShellProposal{
		Command:   cmd,
		Parts:     parts,
		RiskLevel: maxRisk,
	}
}

// ---------------------------------------------------------------------------
// ShellProposal methods
// ---------------------------------------------------------------------------

// MostDestructivePart returns a pointer to the part with the highest
// RiskLevel. Returns nil if the proposal has no parts. Ties return the
// first part in command order.
func (p ShellProposal) MostDestructivePart() *ShellPart {
	if len(p.Parts) == 0 {
		return nil
	}

	maxIdx := 0
	maxRank := kindRiskLevel(p.Parts[0].Kind).Rank()
	for i := 1; i < len(p.Parts); i++ {
		rank := kindRiskLevel(p.Parts[i].Kind).Rank()
		if rank > maxRank {
			maxRank = rank
			maxIdx = i
		}
	}
	return &p.Parts[maxIdx]
}

// HighRiskParts returns all parts whose RiskLevel is >= High
// (Critical or High). Returns nil if none qualify.
func (p ShellProposal) HighRiskParts() []ShellPart {
	var result []ShellPart
	for _, part := range p.Parts {
		risk := kindRiskLevel(part.Kind)
		if risk.Rank() >= configuration.RiskLevelHigh.Rank() {
			result = append(result, part)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Agent.RequestShellApproval
// ---------------------------------------------------------------------------

// RequestShellApproval asks the user (CLI or WebUI) to approve each part of
// the shell command individually. Returns a map from part ID to approved bool.
//
// Flow:
//  1. If no parts, returns empty map and nil error.
//  2. If the WebUI has an active surface, dispatch via the security
//     approval manager (for now, return a "all approved" map — SP-093-3
//     implements the real WebUI per-part dialog).
//  3. Otherwise, call console.PromptShellApprovalParts (the CLI picker).
//
// Errors come from the picker (e.g. context cancelled); a per-part
// rejection does NOT return an error — it's encoded in the decisions map.
func (a *Agent) RequestShellApproval(ctx context.Context, p ShellProposal) (map[string]bool, error) {
	if len(p.Parts) == 0 {
		return map[string]bool{}, nil
	}

	// WebUI surface — stub for now (SP-093-3 wires real per-part UI).
	isSubagent := a.IsSubagent()
	hasWebUI := !a.isNonInteractive() && !isSubagent && a.HasActiveWebUIClients()
	if hasWebUI {
		if a.debug {
			a.debugLog("[SHELL-PART] RequestShellApproval: WebUI active — returning stub all-approved (SP-093-3)\n")
		}
		decisions := make(map[string]bool, len(p.Parts))
		for _, part := range p.Parts {
			decisions[part.ID] = true
		}
		return decisions, nil
	}

	// CLI surface — use the per-part picker.
	// Project ShellParts into console.ShellPartInfo (avoids cyclic import).
	parts := make([]console.ShellPartInfo, len(p.Parts))
	for i, part := range p.Parts {
		parts[i] = console.ShellPartInfo{
			ID:        part.ID,
			Text:      part.Text,
			Kind:      string(part.Kind),
			Semantic:  part.Semantic,
			RiskLabel: kindRiskLabel(part.Kind),
		}
	}
	return console.PromptShellApprovalParts(ctx, parts)
}

// kindRiskLabel returns a short risk-tier label for CLI display.
func kindRiskLabel(kind CommandKind) string {
	switch kind {
	case CommandKindRm, CommandKindGitReset, CommandKindKubectl:
		return "CRITICAL"
	case CommandKindDocker, CommandKindGitPush:
		return "HIGH"
	case CommandKindChmod, CommandKindChown,
		CommandKindWriteRedirect, CommandKindHttpPost:
		return "MEDIUM"
	default:
		return "LOW"
	}
}
