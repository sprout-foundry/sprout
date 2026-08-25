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
	"log"
	"strings"
	"sync/atomic"
	"time"
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
		return SecurityResult{Risk: SecuritySafe, Reasoning: "No shell command (check_background or stop_background operation)", Category: RiskCategoryReadOnly}
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

	// CAUTION and DANGEROUS commands prompt the user for approval.
	// Only DANGEROUS commands additionally set ShouldBlock (so they can
	// be blocked if no approval manager is available). IsHardBlock is
	// set only by isCriticalSystemOperation above (rm -rf /, mkfs, etc.).
	shouldPrompt := maxRisk >= SecurityCaution || isPrivilegedInstall

	// Determine category based on risk level and command characteristics
	var category RiskCategory
	if isPrivilegedInstall {
		category = RiskCategoryPrivileged
	} else if isSudoCommand(cmd) {
		category = RiskCategoryPrivileged
	} else if maxRisk == SecurityDangerous {
		category = riskCategoryFromRiskType(getShellCommandRiskType(cmd, maxRisk, isCritical))
	} else if maxRisk == SecurityCaution {
		// CAUTION commands that were downgraded from DANGEROUS still get
		// meaningful categories (destructive, privileged, etc.)
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
