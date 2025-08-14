package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
)

// executeShellCommands runs the specified shell commands
func executeShellCommands(context *AgentContext, commands []string) error {
	context.Logger.LogProcessStep(fmt.Sprintf("🔧 Executing %d shell commands...", len(commands)))

	for i, command := range commands {
		context.Logger.LogProcessStep(fmt.Sprintf("Running command %d: %s", i+1, command))

		if command == "" {
			continue
		}

		// Quick sandbox checks: deny risky patterns (with limited allowlist exceptions)
		if containsRiskyShell(command) && !isAllowedDestructive(command, context.Config) {
			context.Logger.LogProcessStep(fmt.Sprintf("⛔ Blocked risky command: %s", command))
			context.Errors = append(context.Errors, "blocked_risky_command: "+command)
			continue
		}

		// Use shell to execute command to handle pipes, redirects, etc., with a timeout
		ctx, cancel := contextWithTimeout(45 * time.Second)
		defer cancel()
		// Sandbox: run in workspace root only
		wd, _ := os.Getwd()
		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Dir = filepath.Clean(wd)
		output, err := cmd.CombinedOutput()

		// Truncate output immediately to prevent huge outputs from overwhelming the system
		outputStr := string(output)
		const maxOutputSize = 10000 // 10KB limit
		if len(outputStr) > maxOutputSize {
			outputStr = outputStr[:maxOutputSize] + "\n... (output truncated - limit 10KB)"
		}

		if err != nil {
			errorMsg := fmt.Sprintf("Command failed: %s (output: %s)", err.Error(), outputStr)
			context.Errors = append(context.Errors, errorMsg)
			context.Logger.LogProcessStep(fmt.Sprintf("❌ Command %d failed: %s", i+1, errorMsg))
		} else {
			result := fmt.Sprintf("Command %d succeeded: %s", i+1, outputStr)
			context.ExecutedOperations = append(context.ExecutedOperations, result)
			context.Logger.LogProcessStep(fmt.Sprintf("✅ Command %d: %s", i+1, outputStr))
		}
	}

	return nil
}

// contextWithTimeout provides a cancellable context with the given timeout.
// Declared as a helper to keep call sites concise.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// isSimpleShellCommand returns true for trivial, safe commands we allow for fast-path execution
func isSimpleShellCommand(s string) bool {
	t := strings.TrimSpace(strings.ToLower(s))
	if t == "" {
		return false
	}
	// Very conservative allowlist patterns
	if strings.HasPrefix(t, "echo ") {
		return true
	}
	if t == "ls" || strings.HasPrefix(t, "ls ") {
		return true
	}
	if strings.HasPrefix(t, "pwd") {
		return true
	}
	if strings.HasPrefix(t, "whoami") {
		return true
	}
	// Basic grep/find read-only searches
	if strings.HasPrefix(t, "grep ") {
		return true
	}
	if strings.HasPrefix(t, "find ") {
		return true
	}
	return false
}

// containsRiskyShell blocks dangerous patterns; this is a conservative denylist
func containsRiskyShell(s string) bool {
	t := strings.ToLower(s)
	risky := []string{
		" rm -rf ", " rm -r ", " chmod ", " chown ", " mv /", " dd if=", " mkfs", " :(){:|:&};:",
		" >/dev/", "; rm ", " | rm ", "curl ", "wget ", " > /dev/sda", "sudo ", "systemctl ",
	}
	for _, r := range risky {
		if strings.Contains(t, r) {
			return true
		}
	}
	// disallow absolute path writes
	if strings.Contains(t, ">/") {
		return true
	}
	return false
}

// isAllowedDestructive whitelists a very small set of common, safe-ish cleanup commands
// Only allows:
//   - rm -rf node_modules   (also accepts -fr, optional leading ./, optional trailing slash)
//   - rm -f package-lock.json (in workspace root; accepts optional leading ./)
func isAllowedDestructive(s string, cfg *config.Config) bool {
	t := strings.TrimSpace(strings.ToLower(s))
	// Normalize multiple spaces
	for strings.Contains(t, "  ") {
		t = strings.ReplaceAll(t, "  ", " ")
	}
	// Allow rm -rf node_modules (or -fr), with optional ./ and trailing slash
	if strings.HasPrefix(t, "rm ") {
		// reject sudo explicitly
		if strings.Contains(t, "sudo") {
			// Config-driven allowlist
			if cfg != nil && len(cfg.ShellAllowlist) > 0 {
				for _, allowed := range cfg.ShellAllowlist {
					if strings.TrimSpace(strings.ToLower(allowed)) == t {
						return true
					}
				}
			}
			// Env hook for quick overrides (comma-separated exact matches)
			if extra := strings.TrimSpace(os.Getenv("LEDIT_SHELL_ALLOWLIST")); extra != "" {
				for _, line := range strings.Split(extra, ",") {
					if strings.TrimSpace(strings.ToLower(line)) == t {
						return true
					}
				}
			}
			return false
		}
		// tokenize lightly
		parts := strings.Fields(t)
		if len(parts) >= 3 && parts[0] == "rm" {
			flag := parts[1]
			target := parts[2]
			// rm -rf node_modules
			if flag == "-rf" || flag == "-fr" {
				if target == "node_modules" || target == "./node_modules" || target == "node_modules/" || target == "./node_modules/" {
					return true
				}
			}
			// rm -f package-lock.json
			if flag == "-f" {
				if target == "package-lock.json" || target == "./package-lock.json" {
					return true
				}
			}
		}
	}
	// Also allow the exact forms via simple match to be safe against tokenization edge cases
	if t == "rm -rf node_modules" || t == "rm -fr node_modules" || t == "rm -rf ./node_modules" || t == "rm -fr ./node_modules" ||
		t == "rm -rf node_modules/" || t == "rm -fr node_modules/" || t == "rm -rf ./node_modules/" || t == "rm -fr ./node_modules/" ||
		t == "rm -f package-lock.json" || t == "rm -f ./package-lock.json" {
		return true
	}
	return false
}
