// Path and URL-based security classification

package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

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

	// Flag-aware reset detection: --hard, --keep, --merge are destructive
	// because they discard working-tree / index state. These prompt the user
	// but do not hard-block.
	argsStr, _ := args["args"].(string)
	if op == "reset" && (hasToken(argsStr, "--hard") || hasToken(argsStr, "--keep") || hasToken(argsStr, "--merge")) {
		return SecurityResult{
			Risk: SecurityCaution, Reasoning: "Destructive git reset with flag: " + op,
			ShouldPrompt: true,
			RiskType:     "destructive_git_operation", Category: RiskCategoryDestructive,
		}
	}

	// Flag-aware rebase detection: --onto and -i can rewrite
	// history across multiple branches. Prompt, don't block.
	if op == "rebase" && (hasToken(argsStr, "--onto") || hasToken(argsStr, "-i")) {
		return SecurityResult{
			Risk: SecurityCaution, Reasoning: "History-rewriting git rebase with flag: " + op,
			ShouldPrompt: true,
			RiskType:     "destructive_git_operation", Category: RiskCategoryDestructive,
		}
	}

	cautionOps := []string{"reset", "rebase", "cherry_pick", "am", "apply", "rm", "mv", "clean"}
	for _, caution := range cautionOps {
		if op == caution {
			return SecurityResult{Risk: SecurityCaution, Reasoning: "Git operation may affect history: " + op, ShouldPrompt: true, Category: RiskCategoryFileWrite}
		}
	}

	// Force-push and branch deletion prompt the user but do not hard-block.
	// These can lose work but are recoverable via reflog in most cases.
	dangerousOps := []string{"branch_delete", "push --force", "push -f"}
	for _, danger := range dangerousOps {
		if op == danger || (strings.HasPrefix(op, "push") && strings.Contains(opRaw, "--force")) {
			return SecurityResult{Risk: SecurityCaution, Reasoning: "Force-push or branch deletion: " + op, ShouldPrompt: true, Category: RiskCategoryDestructive}
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
				Reasoning:    fmt.Sprintf("screenshot_path %q is outside allowed directories (cwd, /tmp/sprout/*, ~/Downloads)", spRaw),
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

	// (c) Pre-set cookies or headers → Caution
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

	// (e) Default: safe network access
	return SecurityResult{
		Risk:      SecuritySafe,
		Reasoning: "Network access tool with no auth or evaluation primitives",
		Category:  RiskCategoryNetwork,
	}
}

// isScreenshotPathAllowed checks if a cleaned screenshot path falls within
// allowed directories (cwd, /tmp/sprout/*, ~/Downloads).
func isScreenshotPathAllowed(cleanedPath string) bool {
	// Relative paths are always allowed (resolve within cwd)
	if !filepath.IsAbs(cleanedPath) {
		return true
	}

	// /tmp/sprout/* is always allowed (agent scratch/audit/screenshot workspace)
	if strings.HasPrefix(cleanedPath, "/tmp/sprout") {
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
