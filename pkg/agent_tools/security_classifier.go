// Shell command and file path security classifier.
//
// This module provides string-based heuristics for classifying tool calls by risk
// level (SAFE, CAUTION, DANGEROUS). It is designed as a lightweight defense-in-depth
// layer that operates on raw command strings and path arguments WITHOUT accessing the
// filesystem.
//
// # Important Limitations
//
// This classifier intentionally performs NO filesystem operations (no stat, no
// resolve, no symlink following). This keeps it fast and concurrency-safe, but means:
//   - Symlink attacks are not detected. For example, "rm -rf build/" is classified as safe
//     even if "build" is a symlink to "/etc" or "$HOME".
//   - Relative path traversal is not resolved. "rm -rf ../important-project" bypasses
//     all safe-directory checks because the classifier only matches the first path
//     component literally (".." has no special meaning here).
//   - Path normalization is not performed. Multiple slashes ("//"), "." segments,
//     and case variations on case-insensitive filesystems are not normalized.
//   - Environment variable expansion, glob expansion, and shell aliases are not
//     considered. "rm -rf $BUILD_DIR" is classified as CAUTION (command substitution),
//     not DANGEROUS, because the classifier cannot resolve the variable.
//   - The classifier is prefix-based, not semantic. "rm -rf node_modules-new" is
//     safe because it matches "rm -rf node_modules " prefix, even though the actual
//     target is a different directory.
//
// These limitations are acceptable because the classifier's purpose is gate-keeping
// for LLM-initiated operations in a workspace context — NOT a security boundary.
// Actual enforcement (filesystem permissions, user approval, interactive confirmation)
// should be handled by separate layers.
package tools

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// auditLogger is the package-level audit logger for security decisions.
// Set via SetAuditLogger; accessed atomically for concurrent safety.
var auditLogger atomic.Pointer[AuditLogger]

// SetAuditLogger sets the package-level audit logger for recording security
// decisions. Must be called during initialization before concurrent goroutines
// begin calling ClassifyToolCall.
func SetAuditLogger(l *AuditLogger) {
	auditLogger.Store(l)
}

// pipeToShellPattern matches pipe-to-shell patterns that can execute arbitrary code.
// Matches: | followed by optional whitespace, optional path prefix (e.g., /bin/, /usr/bin/),
// optional "env" wrapper, then shell/script interpreter name.
// The shell name must be followed by whitespace, shell metacharacters (;, |, &), or end of string.
// Examples matched: |bash, | bash, |  bash, | /bin/bash, | /usr/bin/env bash, |zsh, |bash -c 'cmd'
// NOT matched: |sort, |shasum, |shfmt (shell name must be followed by a valid boundary)
// standaloneSleepPattern matches `sleep N` and `sleep N{s,m,h,d}` where N is a
// positive integer or decimal. The anchors prevent matching chained or
// embedded forms — `cmd && sleep 5 && cmd2` will not match because the
// caller checks for compound separators first.
var standaloneSleepPattern = regexp.MustCompile(`^sleep\s+\d+(\.\d+)?[smhd]?$`)

// standaloneWaitPattern matches `wait` and `wait <pid>` (a single numeric arg).
// `wait` with no jobs to wait on returns immediately, so this is purely an
// antipattern when issued as a tool call.
var standaloneWaitPattern = regexp.MustCompile(`^wait(\s+\d+)?$`)

// compoundCommandSeparators are the operators that signal a chained or
// piped command. Their presence disqualifies a command from the standalone
// classification, even if part of the command line is a bare sleep/wait.
var compoundCommandSeparators = []string{"&&", "||", ";", "|", "\n"}

// isStandaloneSleepOrWaitCommand reports whether cmd is exactly a bare
// `sleep N[suffix]` or `wait [pid]` invocation with nothing else around it.
//
// Chained or scripted forms (`make && sleep 5`, `bash -c "sleep 60"`,
// `for i in 1 2 3; do sleep $i; done`) are NOT matched — legitimate
// scripting uses are preserved.
func isStandaloneSleepOrWaitCommand(cmd string) bool {
	for _, sep := range compoundCommandSeparators {
		if strings.Contains(cmd, sep) {
			return false
		}
	}
	return standaloneSleepPattern.MatchString(cmd) || standaloneWaitPattern.MatchString(cmd)
}

var pipeToShellPattern = regexp.MustCompile(`\|\s*(?:[^\s|&;]+/\s*)*(?:env\s+)?(?:bash|zsh|dash|fish|ksh|csh|tcsh|python[23]?|perl|ruby|node|sh)(?:\s|[;&|]|$)`)

// pipeToModulePattern matches a pipe into an interpreter run in module
// mode (python -m <module>). In that form stdin is consumed as DATA by
// the named module rather than executed as code — e.g.
// `curl … | python3 -m json.tool` pretty-prints JSON; it does not run
// the downloaded bytes. RE2 has no negative lookahead, so isPipeToShell
// subtracts these matches before deciding.
var pipeToModulePattern = regexp.MustCompile(`\|\s*(?:[^\s|&;]+/\s*)*(?:env\s+)?python[23]?\s+-m\s`)

// isPipeToShell reports whether s pipes output into a shell/interpreter
// that would EXECUTE the piped bytes as code. The python `-m <module>`
// form is treated as data-consuming, not code execution, so a command
// whose only interpreter pipe is a module run (e.g. json.tool) is not
// flagged. Any other pipe-to-interpreter (| bash, bare | python, | sh)
// still matches.
func isPipeToShell(s string) bool {
	lc := strings.ToLower(s)
	if !pipeToShellPattern.MatchString(lc) {
		return false
	}
	// Remove python -m module-mode pipes; if nothing code-executing
	// remains, the command only fed data to a module → not RCE.
	if pipeToModulePattern.MatchString(lc) {
		stripped := pipeToModulePattern.ReplaceAllString(lc, " ")
		if !pipeToShellPattern.MatchString(stripped) {
			return false
		}
	}
	return true
}

// SecurityRisk represents the risk level of a tool call
type SecurityRisk int

const (
	SecuritySafe      SecurityRisk = 0
	SecurityCaution   SecurityRisk = 1
	SecurityDangerous SecurityRisk = 2
)

// String returns a human-readable risk level
func (r SecurityRisk) String() string {
	switch r {
	case SecuritySafe:
		return "SAFE"
	case SecurityCaution:
		return "CAUTION"
	case SecurityDangerous:
		return "DANGEROUS"
	default:
		return "UNKNOWN"
	}
}

// RiskCategory represents the specific category of risk for a classified tool call.
type RiskCategory string

const (
	// RiskCategoryReadOnly — commands that only read data (cat, ls, head, grep, etc.)
	RiskCategoryReadOnly RiskCategory = "read-only"
	// RiskCategoryFileWrite — commands that modify files (write_file, edit_file, mkdir, cp, mv)
	RiskCategoryFileWrite RiskCategory = "file-write"
	// RiskCategoryNetwork — commands that access network (curl, wget, fetch)
	RiskCategoryNetwork RiskCategory = "network"
	// RiskCategoryProcessManagement — commands that manage processes (kill, pkill, docker start/stop)
	RiskCategoryProcessManagement RiskCategory = "process-management"
	// RiskCategoryDestructive — commands that destroy data (rm -rf, git reset --hard)
	RiskCategoryDestructive RiskCategory = "destructive"
	// RiskCategoryPrivileged — commands requiring elevated permissions (sudo, chmod, chown)
	RiskCategoryPrivileged RiskCategory = "privileged"
	// RiskCategoryUnknown — default when category cannot be determined
	RiskCategoryUnknown RiskCategory = "unknown"
)

// SecurityResult contains the classification result for a tool call
type SecurityResult struct {
	Risk         SecurityRisk
	Reasoning    string
	ShouldBlock  bool
	ShouldPrompt bool
	IsHardBlock  bool
	RiskType     string       // Deprecated: Use Category instead. Risk category for user-facing messages
	Category     RiskCategory // Granular risk category for the classified operation

	// IntentConfirmation marks a tool call as requiring explicit user
	// confirmation before proceeding, but NOT because it's dangerous.
	// Used for operations that are safe but consequential — like launching
	// a long-running autonomous workflow. The approval prompt uses
	// intent-focused framing instead of security-warning framing.
	IntentConfirmation bool
}

// IsDestructive returns true if the operation's risk category is destructive.
func (r SecurityResult) IsDestructive() bool {
	return r.Category == RiskCategoryDestructive
}

// riskCategoryFromRiskType maps a RiskType string (from getShellCommandRiskType)
// to a RiskCategory. Returns RiskCategoryUnknown if the risk type is unrecognized.
func riskCategoryFromRiskType(riskType string) RiskCategory {
	switch riskType {
	case "mass_deletion", "source_code_destruction", "directory_deletion", "destructive_git_operation":
		return RiskCategoryDestructive
	case "privilege_escalation", "insecure_permissions":
		return RiskCategoryPrivileged
	case "remote_code_execution", "arbitrary_code_execution", "system_integrity":
		return RiskCategoryDestructive
	case "disk_destruction", "system_instability", "critical_system_operation":
		return RiskCategoryDestructive
	default:
		return RiskCategoryUnknown
	}
}

// classifyAction returns a human-readable action string for audit logging.
func classifyAction(result SecurityResult) string {
	switch {
	case result.ShouldBlock:
		return "denied"
	case result.ShouldPrompt:
		return "prompted"
	default:
		return "allowed"
	}
}

// ClassifyToolCall classifies a tool call for security purposes based on the
// tool name and its arguments. It returns a SecurityResult indicating the risk
// level, reasoning, and whether the operation should be blocked or prompt the user.
//
// Classification is purely string-based (no filesystem access). See the
// package-level documentation for known limitations of this approach.
//
// Only tools whose arguments carry risk (shell commands, file writes, git ops)
// need explicit classification. All other registered tools default to SAFE —
// if a tool is in the registry, it's already vetted. The only real security
// value is inspecting the *arguments* to those risky tools.
func ClassifyToolCall(toolName string, args map[string]interface{}) SecurityResult {
	var result SecurityResult
	switch toolName {
	case "shell_command":
		result = classifyShellCommand(args)
	case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		result = classifyWriteOperation(args)
	case "mkdir":
		result = SecurityResult{Risk: SecuritySafe, Reasoning: "Directory creation in workspace", Category: RiskCategoryFileWrite}
	case "fetch_url", "web_search":
		result = SecurityResult{Risk: SecuritySafe, Reasoning: "Network access tool", Category: RiskCategoryNetwork}
	case "browse_url":
		result = classifyBrowseURL(args)
	case "git":
		result = classifyGitOperation(args)
	case "run_automate":
		// Autonomous workflows are safe (user created them) but consequential
		// (run for hours unsupervised). Always require intent confirmation.
		result = SecurityResult{
			Risk:               SecuritySafe,
			Reasoning:          "Autonomous workflow execution — requires confirmation before starting",
			Category:           RiskCategoryProcessManagement,
			IntentConfirmation: true,
		}
	default:
		// Tools whose arguments don't need runtime inspection are SAFE.
		// The tool registry already validates that only registered tools
		// reach this point — unregistered tools are rejected before
		// security classification runs.
		result = SecurityResult{Risk: SecuritySafe, Reasoning: "Registered tool with no argument-level risk", Category: RiskCategoryUnknown}
	}

	// Log the security decision (nil-safe, atomic load)
	if l := auditLogger.Load(); l != nil {
		if err := l.LogEntry(AuditEntry{
			Timestamp: time.Now(),
			Tool:      toolName,
			RiskLevel: result.Risk.String(),
			Category:  string(result.Category),
			Action:    classifyAction(result),
			Reasoning: result.Reasoning,
			Source:    "classifier",
		}); err != nil {
			log.Printf("audit log write failed: %v", err)
		}
	}

	return result
}

// classifyShellCommand classifies shell commands by risk level
func classifyShellCommand(args map[string]interface{}) SecurityResult {
	// check_background-only calls are read-only: just retrieve output from a PTY session.
	// No command is needed when checking background output.
	if cbRaw, ok := args["check_background"].(string); ok && cbRaw != "" {
		cmdRaw, hasCommand := args["command"].(string)
		if !hasCommand || cmdRaw == "" {
			return SecurityResult{Risk: SecuritySafe, Reasoning: "Read-only background session output check", Category: RiskCategoryReadOnly}
		}
	}

	// stop_background-only calls are session management: sends Ctrl+C and closes the session.
	// No shell command is executed.
	if sbRaw, ok := args["stop_background"].(string); ok && sbRaw != "" {
		return SecurityResult{Risk: SecuritySafe, Reasoning: "Background session termination (no shell execution)", Category: RiskCategoryProcessManagement}
	}

	cmdRaw, ok := args["command"].(string)
	if !ok || cmdRaw == "" {
		return SecurityResult{Risk: SecurityCaution, Reasoning: "Empty or invalid command", ShouldPrompt: true, Category: RiskCategoryUnknown}
	}

	cmd := strings.TrimSpace(cmdRaw)

	// Standalone `sleep N` / `wait` are an antipattern when invoked as a tool
	// call. They appear to "succeed" because the 2-minute shell deadline
	// adopts them into a background session and returns a promotion message,
	// but the agent did NOT actually wait the requested duration. Models
	// commonly reach for sleep as a poll-spacer between background-session
	// checks; the correct API for that case is
	// `shell_command(check_background=<id>, wait_seconds=<seconds>)`.
	//
	// This is NOT a security issue — it's a usage guidance issue. We return
	// SecuritySafe so no security elevation/prompts trigger. The shell handler
	// catches this before execution and returns the guidance as a plain tool
	// error message to the model.
	if isStandaloneSleepOrWaitCommand(cmd) {
		return SecurityResult{
			Risk: SecuritySafe,
			Reasoning: "Standalone sleep/wait is not appropriate as a shell_command tool call. " +
				"For waiting on a background session, use shell_command(check_background=\"<session_id>\", wait_seconds=<seconds>) — that blocks (up to 10 min) without burning tokens on retries. " +
				"For inserting a delay between commands inside a script, chain with && (e.g., \"cmd1 && sleep 5 && cmd2\"). " +
				"Standalone sleep here will be cut off at the 2-minute shell deadline and adopted as a background session; the agent will NOT have actually waited the requested duration.",
			Category: RiskCategoryProcessManagement,
		}
	}

	if isCriticalSystemOperation("shell_command", args) {
		rt := getShellCommandRiskType(cmd, SecurityDangerous, true)
		return SecurityResult{
			Risk: SecurityDangerous, Reasoning: "Critical system operation detected",
			ShouldBlock: true, ShouldPrompt: true, IsHardBlock: true,
			RiskType: rt, Category: riskCategoryFromRiskType(rt),
		}
	}

	risks := classifyChainedCommand(cmd)
	maxRisk := maxRisk(risks)
	isPrivilegedInstall := containsPrivilegedPackageInstall(cmd)
	isCritical := isCriticalSystemOperation("shell_command", args)

	// Only DANGEROUS commands trigger blocking/prompts.
	// Exception: privileged package installation is CAUTION but still prompts.
	shouldPrompt := maxRisk == SecurityDangerous || isPrivilegedInstall

	// Determine category based on risk level and command characteristics
	var category RiskCategory
	if isPrivilegedInstall {
		category = RiskCategoryPrivileged
	} else if maxRisk == SecurityDangerous {
		category = riskCategoryFromRiskType(getShellCommandRiskType(cmd, maxRisk, isCritical))
	} else if maxRisk == SecuritySafe {
		category = RiskCategoryReadOnly
	} else {
		category = RiskCategoryUnknown
	}
	return SecurityResult{
		Risk:         maxRisk,
		Reasoning:    getShellCommandReasoning(cmd, maxRisk),
		ShouldBlock:  maxRisk == SecurityDangerous,
		ShouldPrompt: shouldPrompt,
		RiskType:     getShellCommandRiskType(cmd, maxRisk, isCritical),
		Category:     category,
	}
}

// classifyChainedCommand splits and classifies chained commands
func classifyChainedCommand(cmd string) []SecurityRisk {
	if risk, ok := classifyReadOnlyForLoop(cmd); ok {
		return []SecurityRisk{risk}
	}

	// Check for pipe-to-shell patterns (case-insensitive to prevent bypass).
	// Strip quoted sections first to avoid false positives from | characters
	// inside grep patterns, regex alternation, etc. (e.g., grep "a|b|c" | head).
	// Use regex to handle any whitespace and multiple shell interpreters.
	cmdLower := strings.ToLower(cmd)
	stripped := stripQuotedSections(cmdLower)
	if isPipeToShell(stripped) {
		return []SecurityRisk{SecurityDangerous}
	}

	// Split on &&, ||, ;, | but respect quotes
	var parts []string
	current := &strings.Builder{}
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(cmd); i++ {
		c := cmd[i]

		if !inQuote && (c == '\'' || c == '"') {
			inQuote = true
			quoteChar = c
			current.WriteByte(c)
			continue
		}

		if inQuote && c == quoteChar {
			inQuote = false
			quoteChar = 0
			current.WriteByte(c)
			continue
		}

		if !inQuote {
			if c == '&' && i+1 < len(cmd) && cmd[i+1] == '&' {
				if current.Len() > 0 {
					parts = append(parts, strings.TrimSpace(current.String()))
					current.Reset()
				}
				i++
				continue
			}
			if c == '|' && i+1 < len(cmd) && cmd[i+1] == '|' {
				if current.Len() > 0 {
					parts = append(parts, strings.TrimSpace(current.String()))
					current.Reset()
				}
				i++
				continue
			}
			if c == ';' || c == '|' {
				if current.Len() > 0 {
					parts = append(parts, strings.TrimSpace(current.String()))
					current.Reset()
				}
				continue
			}
		}
		current.WriteByte(c)
	}

	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}

	var risks []SecurityRisk
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		risks = append(risks, classifySingleCommand(part))
	}
	return risks
}

// classifySingleCommand classifies a single command (no chaining)
func classifySingleCommand(cmd string) SecurityRisk {
	cmdLower := strings.ToLower(cmd)

	if risk, ok := classifyReadOnlyForLoop(cmd); ok {
		return risk
	}

	// Command substitution ($() or backticks) - cannot fully inspect inner commands
	if strings.Contains(cmd, "$(") || strings.ContainsAny(cmd, "`") {
		return SecurityCaution
	}

	// Heredoc syntax (<<) - cannot fully inspect heredoc content
	if strings.Contains(cmd, "<<") {
		return SecurityCaution
	}

	// Check for output redirection to system directories
	if strings.Contains(cmd, "> /etc/") || strings.Contains(cmd, ">> /etc/") ||
		strings.Contains(cmd, "> /usr/") || strings.Contains(cmd, ">> /usr/") ||
		strings.Contains(cmd, "> /bin/") || strings.Contains(cmd, ">> /bin/") ||
		strings.Contains(cmd, "> /sbin/") || strings.Contains(cmd, ">> /sbin/") ||
		strings.Contains(cmd, "> /var/") || strings.Contains(cmd, ">> /var/") ||
		strings.Contains(cmd, "> /opt/") || strings.Contains(cmd, ">> /opt/") ||
		strings.Contains(cmd, "> /root/") || strings.Contains(cmd, ">> /root/") ||
		strings.Contains(cmd, "> /boot/") || strings.Contains(cmd, ">> /boot/") {
		return SecurityDangerous
	}
	if (strings.Contains(cmd, "> /dev/") || strings.Contains(cmd, ">> /dev/")) &&
		!strings.Contains(cmd, "> /dev/null") && !strings.Contains(cmd, ">> /dev/null") &&
		!strings.Contains(cmd, "> /dev/stdout") && !strings.Contains(cmd, ">> /dev/stdout") &&
		!strings.Contains(cmd, "> /dev/stderr") && !strings.Contains(cmd, ">> /dev/stderr") {
		return SecurityDangerous
	}

	// Check for path traversal in redirection targets (e.g., > /tmp/../etc/passwd)
	if containsRedirection(cmd) && hasRedirectionTraversalToSystemDir(cmd) {
		return SecurityDangerous
	}

	if isPrivilegedPackageInstall(cmdLower) {
		return SecurityCaution
	}

	if isDangerousPattern(cmdLower) {
		return SecurityDangerous
	}

	// Check caution patterns BEFORE safe patterns, so that specific
	// caution-level commands (like "docker rm") override broad safe matches.
	if isCautionPattern(cmdLower) {
		return SecurityCaution
	}

	if isSafeShellCommand(cmdLower) {
		return SecuritySafe
	}

	return SecurityCaution
}

func classifyReadOnlyForLoop(cmd string) (SecurityRisk, bool) {
	trimmed := strings.TrimSpace(cmd)
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "for ") || !strings.Contains(lower, " do ") || !strings.HasSuffix(lower, "done") {
		return SecuritySafe, false
	}

	max := SecuritySafe

	for _, sub := range extractCommandSubstitutions(trimmed) {
		risk := maxRisk(classifyChainedCommand(sub))
		if risk > max {
			max = risk
		}
	}

	doIndex := strings.Index(lower, " do ")
	doneIndex := strings.LastIndex(lower, " done")
	if doIndex == -1 || doneIndex == -1 || doneIndex <= doIndex+4 {
		return SecurityCaution, true
	}

	body := strings.TrimSpace(trimmed[doIndex+4 : doneIndex])
	if body == "" {
		return SecurityCaution, true
	}

	bodyRisk := classifyReadOnlyLoopBody(body)
	if bodyRisk > max {
		max = bodyRisk
	}

	return max, true
}

func classifyReadOnlyLoopBody(body string) SecurityRisk {
	parts := strings.Split(body, ";")
	max := SecuritySafe

	for _, raw := range parts {
		part := strings.TrimSpace(raw)
		if part == "" {
			continue
		}
		for _, branch := range strings.Split(part, "&&") {
			for _, option := range strings.Split(branch, "||") {
				cmd := strings.TrimSpace(option)
				if cmd == "" {
					continue
				}
				risk := classifySingleCommand(cmd)
				if risk > max {
					max = risk
				}
			}
		}
	}

	return max
}

// classifyWriteOperation classifies file write operations
func classifyWriteOperation(args map[string]interface{}) SecurityResult {
	pathRaw, ok := args["path"].(string)
	if !ok || pathRaw == "" {
		return SecurityResult{Risk: SecurityCaution, Reasoning: "Empty or invalid path", ShouldPrompt: true, Category: RiskCategoryFileWrite}
	}

	path := pathRaw

	// Check for critical system files and directories
	for _, critical := range []string{
		"/etc/shadow", "/etc/passwd", "/etc/sudoers", "/etc/ssh/sshd_config",
		"/root/.ssh/authorized_keys", "/etc/hosts", "/etc/resolv.conf",
		"/usr/", "/etc/", "/bin/", "/sbin/", "/var/", "/opt/", "/boot/", "/lib/", "/lib64/",
	} {
		if path == critical || strings.HasPrefix(path, critical) {
			// Allow macOS temp directories (/var/folders/...) and /tmp paths
			if (strings.HasPrefix(path, "/var/folders/") || strings.HasPrefix(path, "/var/tmp/")) && strings.HasPrefix(critical, "/var/") {
				continue
			}
			return SecurityResult{Risk: SecurityDangerous, Reasoning: "Writing to critical system file or directory: " + path, ShouldBlock: true, ShouldPrompt: true, IsHardBlock: true, RiskType: "system_integrity", Category: RiskCategoryDestructive}
		}
	}

	if strings.HasPrefix(path, "/tmp/") || strings.HasPrefix(path, "/private/tmp/") || strings.HasPrefix(path, "/var/folders/") || strings.HasPrefix(path, "/private/var/folders/") || path == "/tmp" {
		return SecurityResult{Risk: SecuritySafe, Reasoning: "Writing to temporary directory", Category: RiskCategoryFileWrite}
	}

	return SecurityResult{Risk: SecuritySafe, Reasoning: "Workspace file operation", Category: RiskCategoryFileWrite}
}

// hasToken splits s on whitespace and reports whether any resulting token
// exactly equals token. This prevents substring false-positives (e.g.
// "--hardlink" must NOT match "--hard").
func hasToken(s string, token string) bool {
	for _, t := range strings.Fields(s) {
		if t == token {
			return true
		}
	}
	return false
}

// classifyGitOperation classifies git operations
func classifyGitOperation(args map[string]interface{}) SecurityResult {
	opRaw, ok := args["operation"].(string)
	if !ok || opRaw == "" {
		return SecurityResult{Risk: SecurityCaution, Reasoning: "Empty or invalid git operation", ShouldPrompt: true, Category: RiskCategoryUnknown}
	}

	op := strings.ToLower(strings.TrimSpace(opRaw))

	safeOps := []string{"commit", "add", "status", "log", "diff", "show", "branch", "remote", "stash", "tag", "revert", "fetch", "merge", "pull", "push"}
	for _, safe := range safeOps {
		if op == safe {
			return SecurityResult{Risk: SecuritySafe, Reasoning: "Safe git operation: " + op, Category: RiskCategoryReadOnly}
		}
	}

	// Flag-aware dangerous-reset detection: --hard, --keep, --merge are
	// destructive because they discard working-tree / index state.
	argsStr, _ := args["args"].(string)
	if op == "reset" && (hasToken(argsStr, "--hard") || hasToken(argsStr, "--keep") || hasToken(argsStr, "--merge")) {
		return SecurityResult{
			Risk: SecurityDangerous, Reasoning: "Destructive git reset with flag: " + op,
			ShouldBlock: true, ShouldPrompt: true, IsHardBlock: true,
			RiskType: "destructive_git_operation", Category: RiskCategoryDestructive,
		}
	}

	// Flag-aware dangerous-rebase detection: --onto and -i can rewrite
	// history across multiple branches.
	if op == "rebase" && (hasToken(argsStr, "--onto") || hasToken(argsStr, "-i")) {
		return SecurityResult{
			Risk: SecurityDangerous, Reasoning: "Destructive git rebase with flag: " + op,
			ShouldBlock: true, ShouldPrompt: true, IsHardBlock: true,
			RiskType: "destructive_git_operation", Category: RiskCategoryDestructive,
		}
	}

	cautionOps := []string{"reset", "rebase", "cherry_pick", "am", "apply", "rm", "mv", "clean"}
	for _, caution := range cautionOps {
		if op == caution {
			return SecurityResult{Risk: SecurityCaution, Reasoning: "Git operation may affect history: " + op, ShouldPrompt: true, Category: RiskCategoryFileWrite}
		}
	}

	// Note: "clean" is intentionally only CAUTION-level here. Dangerous variants
	// like "git clean -ff" and "git clean -fd" are caught by the shell-level
	// security classifier (isDangerousPattern), which processes the full git
	// command string including flags.
	dangerousOps := []string{"branch_delete", "push --force", "push -f"}
	for _, danger := range dangerousOps {
		if op == danger || (strings.HasPrefix(op, "push") && strings.Contains(opRaw, "--force")) {
			return SecurityResult{Risk: SecurityDangerous, Reasoning: "Dangerous git operation that may force-push or delete: " + op, ShouldBlock: true, ShouldPrompt: true, Category: RiskCategoryDestructive}
		}
	}

	return SecurityResult{Risk: SecurityCaution, Reasoning: "Unknown git operation: " + op, ShouldPrompt: true, Category: RiskCategoryUnknown}
}

// isCriticalSystemOperation reports whether a shell tool call is a
// critical system operation that must always be hard-blocked. The
// canonical pattern list lives in configuration.IsCriticalOperation so
// the static classifier (this gate) and the persona risk cascade
// (configuration.EvaluateOperationRisk) agree on what "critical" means —
// see the unification note on IsCriticalOperation.
func isCriticalSystemOperation(toolName string, args map[string]interface{}) bool {
	if toolName != "shell_command" {
		return false
	}

	cmdRaw, ok := args["command"].(string)
	if !ok || cmdRaw == "" {
		return false
	}

	return configuration.IsCriticalOperation(cmdRaw)
}

// classifyBrowseURL classifies browse_url tool calls by inspecting URL targets,
// screenshot paths, eval scripts, and authentication parameters.
func classifyBrowseURL(args map[string]interface{}) SecurityResult {
	urlRaw, _ := args["url"].(string)
	urlLower := strings.ToLower(urlRaw)

	// (a) Screenshot path outside allowed directories → Dangerous
	if spRaw, ok := args["screenshot_path"].(string); ok && spRaw != "" {
		sp := filepath.Clean(spRaw)
		if !isScreenshotPathAllowed(sp) {
			return SecurityResult{
				Risk:         SecurityDangerous,
				Reasoning:    fmt.Sprintf("screenshot_path %q is outside allowed directories (cwd, /tmp/sprout_examples, ~/Downloads)", spRaw),
				ShouldBlock:  true,
				ShouldPrompt: true,
				Category:     RiskCategoryFileWrite,
			}
		}
	}

	// (b) file:// URL without allow_file_url opt-in → Caution
	if strings.HasPrefix(urlLower, "file://") {
		allowFile, _ := args["allow_file_url"].(bool)
		if !allowFile {
			return SecurityResult{
				Risk:         SecurityCaution,
				Reasoning:    "file:// URLs can read arbitrary local files — set allow_file_url=true to confirm intent",
				ShouldPrompt: true,
				Category:     RiskCategoryNetwork,
			}
		}
	}

	// (c) Eval step with network egress → Caution
	if stepsRaw, ok := args["steps"].([]interface{}); ok {
		for _, rawStep := range stepsRaw {
			if stepMap, ok := rawStep.(map[string]interface{}); ok {
				if action, ok := stepMap["action"].(string); ok && strings.ToLower(action) == "eval" {
					if script, ok := stepMap["script"].(string); ok {
						if primitive := detectNetworkEgress(script); primitive != "" {
							return SecurityResult{
								Risk:         SecurityCaution,
								Reasoning:    fmt.Sprintf("eval step contains network egress (%s) — browser-side requests may bypass server CORS / fetch_url allowlists", primitive),
								ShouldPrompt: true,
								Category:     RiskCategoryNetwork,
							}
						}
					}
				}
			}
		}
	}

	// (d) Pre-set cookies or headers → Caution
	if cookiesRaw, ok := args["cookies"].(map[string]interface{}); ok && len(cookiesRaw) > 0 {
		return SecurityResult{
			Risk:         SecurityCaution,
			Reasoning:    "Pre-navigation cookies/headers authenticate to a remote service. Review the target URL and credentials before proceeding",
			ShouldPrompt: true,
			Category:     RiskCategoryNetwork,
		}
	}
	if headersRaw, ok := args["headers"].(map[string]interface{}); ok && len(headersRaw) > 0 {
		return SecurityResult{
			Risk:         SecurityCaution,
			Reasoning:    "Pre-navigation cookies/headers authenticate to a remote service. Review the target URL and credentials before proceeding",
			ShouldPrompt: true,
			Category:     RiskCategoryNetwork,
		}
	}

	// (e) Localhost URL → Caution
	if isLocalhostURL(urlRaw) {
		return SecurityResult{
			Risk:         SecurityCaution,
			Reasoning:    "localhost URL may reach private services on this machine",
			ShouldPrompt: true,
			Category:     RiskCategoryNetwork,
		}
	}

	// (f) Default: safe network access
	return SecurityResult{
		Risk:      SecuritySafe,
		Reasoning: "Network access tool with no auth or evaluation primitives",
		Category:  RiskCategoryNetwork,
	}
}

// isScreenshotPathAllowed checks if a cleaned screenshot path falls within
// allowed directories (cwd, /tmp/sprout_examples, ~/Downloads).
func isScreenshotPathAllowed(cleanedPath string) bool {
	// Relative paths are always allowed (resolve within cwd)
	if !filepath.IsAbs(cleanedPath) {
		return true
	}

	// /tmp/sprout_examples is always allowed
	if strings.HasPrefix(cleanedPath, "/tmp/sprout_examples") {
		return true
	}

	// ~/Downloads is allowed
	if homeDir, err := os.UserHomeDir(); err == nil {
		downloads := filepath.Join(homeDir, "Downloads")
		if strings.HasPrefix(cleanedPath, downloads) {
			return true
		}
	}

	// CWD is allowed
	if cwd, err := os.Getwd(); err == nil {
		if strings.HasPrefix(cleanedPath, cwd) {
			return true
		}
	}

	return false
}

// detectNetworkEgress checks if a JS script contains network egress primitives.
// Returns the matched primitive name, or empty string if none found.
func detectNetworkEgress(script string) string {
	lower := strings.ToLower(script)
	primitives := []string{"fetch(", "xmlhttprequest", "navigator.sendbeacon", "websocket", "eventsource", "import(", "new image().src=", "<script src=", "<iframe src="}
	for _, p := range primitives {
		if strings.Contains(lower, p) {
			return p
		}
	}
	return ""
}

// isLocalhostURL reports whether url targets a local address.
func isLocalhostURL(url string) bool {
	lower := strings.ToLower(url)
	return strings.HasPrefix(lower, "http://localhost") ||
		strings.HasPrefix(lower, "http://127.0.0.1") ||
		strings.HasPrefix(lower, "http://[::1]") ||
		strings.HasPrefix(lower, "https://localhost") ||
		strings.HasPrefix(lower, "https://127.0.0.1") ||
		strings.HasPrefix(lower, "https://[::1]")
}
