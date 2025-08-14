package agent

import (
	stdctx "context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
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
		// Timeout is configurable; default to 45s
		timeout := 45 * time.Second
		if context.Config != nil && context.Config.ShellTimeoutSecs > 0 {
			timeout = time.Duration(context.Config.ShellTimeoutSecs) * time.Second
		}
		ctx, cancel := contextWithTimeout(timeout)
		defer cancel()
		// Sandbox: run in workspace root only
		wd, _ := os.Getwd()
		// Apply lightweight resource limits via ulimit when available (sh built-in)
		limitedCmd := withUlimitPrefix(command, timeout)
		cmd := exec.CommandContext(ctx, "sh", "-c", limitedCmd)
		cmd.Dir = filepath.Clean(wd)
		// Kill entire process group on timeout; set PGID
		if runtime.GOOS != "windows" {
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		}
		// Sanitize environment to reduce credential leakage
		cmd.Env = sanitizeEnv(os.Environ())
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
			// Extra cleanup on timeout: kill the process group
			if ctx.Err() == stdctx.DeadlineExceeded && cmd.Process != nil && runtime.GOOS != "windows" {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
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
func contextWithTimeout(d time.Duration) (stdctx.Context, stdctx.CancelFunc) {
	return stdctx.WithTimeout(stdctx.Background(), d)
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
	if strings.Contains(t, ">>/") {
		return true
	}
	if strings.Contains(t, " tee /") || strings.Contains(t, " tee ../") {
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

// withUlimitPrefix prefixes the command with conservative ulimit settings when supported.
// Uses CPU time roughly equal to timeout seconds and caps open files and file size.
func withUlimitPrefix(cmd string, timeout time.Duration) string {
	// On POSIX shells, 'ulimit' is a built-in. Keep portable subset.
	secs := int(timeout.Seconds())
	if secs <= 0 {
		secs = 45
	}
	// Limit CPU seconds and open files; cap file size (~10MB blocks of 512 bytes ≈ 20480)
	prefix := fmt.Sprintf("ulimit -t %d; ulimit -n 256; ulimit -f 20480; ", secs)
	return prefix + cmd
}

// sanitizeEnv removes common sensitive variables and constrains PATH to safe defaults.
func sanitizeEnv(environ []string) []string {
	blockedPrefixes := []string{
		"AWS_", "AZURE_", "GCP_", "GOOGLE_", "OPENAI_", "ANTHROPIC_", "HUGGINGFACE_",
		"GITHUB_", "GH_", "NPM_TOKEN", "API_KEY", "SECRET", "KEY", "TOKEN",
	}
	out := make([]string, 0, len(environ))
	havePath := false
	for _, kv := range environ {
		parts := strings.SplitN(kv, "=", 2)
		key := parts[0]
		blocked := false
		for _, p := range blockedPrefixes {
			if strings.HasPrefix(strings.ToUpper(key), p) {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		if strings.EqualFold(key, "PATH") {
			havePath = true
			// Constrain PATH to common system bins plus current PATH tail (best effort)
			// Avoid completely wiping in case tools are needed
			out = append(out, "PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin")
			continue
		}
		out = append(out, kv)
	}
	if !havePath {
		out = append(out, "PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin")
	}
	return out
}
