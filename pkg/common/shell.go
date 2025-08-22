package common

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/utils"
)

// ShellExecutor provides common shell command execution functionality
type ShellExecutor struct {
	config    *config.Config
	logger    *utils.Logger
	allowlist []string
	blocklist []string
	timeout   time.Duration
	maxLength int
}

// NewShellExecutor creates a new shell executor
func NewShellExecutor(cfg *config.Config, logger *utils.Logger) *ShellExecutor {
	securityConfig := cfg.GetSecurityConfig()

	return &ShellExecutor{
		config:    cfg,
		logger:    logger,
		allowlist: securityConfig.ShellAllowlist,
		blocklist: securityConfig.BlockedCommands,
		timeout:   time.Duration(cfg.GetPerformanceConfig().ShellTimeoutSecs) * time.Second,
		maxLength: 1000,
	}
}

// ShellResult contains the result of a shell command execution
type ShellResult struct {
	Command  string
	Output   string
	Error    error
	ExitCode int
	Duration time.Duration
	Success  bool
}

// ExecuteCommand executes a single shell command
func (se *ShellExecutor) ExecuteCommand(ctx context.Context, command string) *ShellResult {
	startTime := time.Now()

	// Validate command
	if err := se.validateCommand(command); err != nil {
		return &ShellResult{
			Command:  command,
			Error:    err,
			Duration: time.Since(startTime),
			Success:  false,
		}
	}

	// Log command execution
	if se.logger != nil {
		se.logger.LogProcessStep(fmt.Sprintf("Executing command: %s", command))
	}

	// Execute command
	execCtx, cancel := context.WithTimeout(ctx, se.timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()

	duration := time.Since(startTime)
	exitCode := 0

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	success := err == nil

	// Log result
	if se.logger != nil {
		if success {
			se.logger.LogProcessStep(fmt.Sprintf("Command completed successfully in %v", duration))
		} else {
			se.logger.LogProcessStep(fmt.Sprintf("Command failed (exit code %d) in %v: %v", exitCode, duration, err))
		}
	}

	return &ShellResult{
		Command:  command,
		Output:   string(output),
		Error:    err,
		ExitCode: exitCode,
		Duration: duration,
		Success:  success,
	}
}

// ExecuteCommands executes multiple shell commands sequentially
func (se *ShellExecutor) ExecuteCommands(ctx context.Context, commands []string) []*ShellResult {
	results := make([]*ShellResult, len(commands))

	for i, command := range commands {
		results[i] = se.ExecuteCommand(ctx, command)

		// Stop on first error if not continuing on error
		if !results[i].Success {
			break
		}
	}

	return results
}

// ExecuteScript executes a multi-line shell script
func (se *ShellExecutor) ExecuteScript(ctx context.Context, script string) *ShellResult {
	// Split script into individual commands
	lines := strings.Split(script, "\n")
	var commands []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			commands = append(commands, line)
		}
	}

	if len(commands) == 0 {
		return &ShellResult{
			Command: "script",
			Output:  "No commands to execute",
			Success: true,
		}
	}

	// Execute all commands
	startTime := time.Now()
	results := se.ExecuteCommands(ctx, commands)

	// Combine results
	var output strings.Builder
	var lastError error
	var lastExitCode int
	totalSuccess := true

	for i, result := range results {
		output.WriteString(fmt.Sprintf("Command %d: %s\n", i+1, result.Command))
		output.WriteString(fmt.Sprintf("Output: %s\n", result.Output))

		if !result.Success {
			totalSuccess = false
			lastError = result.Error
			lastExitCode = result.ExitCode
			if i < len(results)-1 {
				output.WriteString("Stopping execution due to error\n")
				break
			}
		}
		output.WriteString("\n")
	}

	return &ShellResult{
		Command:  "script",
		Output:   output.String(),
		Error:    lastError,
		ExitCode: lastExitCode,
		Duration: time.Since(startTime),
		Success:  totalSuccess,
	}
}

// validateCommand validates a shell command for security
func (se *ShellExecutor) validateCommand(command string) error {
	// Check command length
	if len(command) > se.maxLength {
		return fmt.Errorf("command too long: %d characters (max: %d)", len(command), se.maxLength)
	}

	// Check against blocklist
	for _, blocked := range se.blocklist {
		if se.containsCommand(command, blocked) {
			return fmt.Errorf("command blocked by security policy: %s", blocked)
		}
	}

	// Check against allowlist if defined
	if len(se.allowlist) > 0 {
		allowed := false
		for _, allowedCmd := range se.allowlist {
			if se.containsCommand(command, allowedCmd) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("command not in allowlist: %s", command)
		}
	}

	return nil
}

// containsCommand checks if a command contains a specific substring
func (se *ShellExecutor) containsCommand(command, substring string) bool {
	return strings.Contains(strings.ToLower(command), strings.ToLower(substring))
}

// IsCommandAllowed checks if a command is allowed
func (se *ShellExecutor) IsCommandAllowed(command string) bool {
	return se.validateCommand(command) == nil
}

// GetTimeout returns the current timeout setting
func (se *ShellExecutor) GetTimeout() time.Duration {
	return se.timeout
}

// SetTimeout sets the command timeout
func (se *ShellExecutor) SetTimeout(timeout time.Duration) {
	se.timeout = timeout
}

// GetMaxLength returns the maximum command length
func (se *ShellExecutor) GetMaxLength() int {
	return se.maxLength
}

// SetMaxLength sets the maximum command length
func (se *ShellExecutor) SetMaxLength(length int) {
	se.maxLength = length
}

// Common shell operations

// ListFiles lists files in a directory
func (se *ShellExecutor) ListFiles(ctx context.Context, dir string) *ShellResult {
	command := fmt.Sprintf("ls -la %s", dir)
	return se.ExecuteCommand(ctx, command)
}

// FindFiles finds files matching a pattern
func (se *ShellExecutor) FindFiles(ctx context.Context, pattern, path string) *ShellResult {
	command := fmt.Sprintf("find %s -name '%s' -type f", path, pattern)
	return se.ExecuteCommand(ctx, command)
}

// GrepSearch performs a text search in files
func (se *ShellExecutor) GrepSearch(ctx context.Context, pattern, path string) *ShellResult {
	command := fmt.Sprintf("grep -r '%s' %s", pattern, path)
	return se.ExecuteCommand(ctx, command)
}

// CreateDirectory creates a directory
func (se *ShellExecutor) CreateDirectory(ctx context.Context, path string) *ShellResult {
	command := fmt.Sprintf("mkdir -p %s", path)
	return se.ExecuteCommand(ctx, command)
}

// RemoveFile removes a file or directory
func (se *ShellExecutor) RemoveFile(ctx context.Context, path string) *ShellResult {
	command := fmt.Sprintf("rm -rf %s", path)
	return se.ExecuteCommand(ctx, command)
}

// CopyFile copies a file or directory
func (se *ShellExecutor) CopyFile(ctx context.Context, src, dst string) *ShellResult {
	command := fmt.Sprintf("cp -r %s %s", src, dst)
	return se.ExecuteCommand(ctx, command)
}

// MoveFile moves a file or directory
func (se *ShellExecutor) MoveFile(ctx context.Context, src, dst string) *ShellResult {
	command := fmt.Sprintf("mv %s %s", src, dst)
	return se.ExecuteCommand(ctx, command)
}

// ChangePermissions changes file permissions
func (se *ShellExecutor) ChangePermissions(ctx context.Context, path, permissions string) *ShellResult {
	command := fmt.Sprintf("chmod %s %s", permissions, path)
	return se.ExecuteCommand(ctx, command)
}

// GetFileInfo gets detailed information about a file
func (se *ShellExecutor) GetFileInfo(ctx context.Context, path string) *ShellResult {
	command := fmt.Sprintf("stat %s", path)
	return se.ExecuteCommand(ctx, command)
}

// CheckDiskUsage checks disk usage for a path
func (se *ShellExecutor) CheckDiskUsage(ctx context.Context, path string) *ShellResult {
	command := fmt.Sprintf("du -sh %s", path)
	return se.ExecuteCommand(ctx, command)
}

// ArchiveFiles creates a compressed archive
func (se *ShellExecutor) ArchiveFiles(ctx context.Context, archivePath string, files []string) *ShellResult {
	fileList := strings.Join(files, " ")
	command := fmt.Sprintf("tar -czf %s %s", archivePath, fileList)
	return se.ExecuteCommand(ctx, command)
}

// ExtractArchive extracts a compressed archive
func (se *ShellExecutor) ExtractArchive(ctx context.Context, archivePath, extractPath string) *ShellResult {
	command := fmt.Sprintf("tar -xzf %s -C %s", archivePath, extractPath)
	return se.ExecuteCommand(ctx, command)
}

// Git operations

// GitStatus gets git status
func (se *ShellExecutor) GitStatus(ctx context.Context) *ShellResult {
	return se.ExecuteCommand(ctx, "git status")
}

// GitAdd adds files to git
func (se *ShellExecutor) GitAdd(ctx context.Context, files []string) *ShellResult {
	fileList := strings.Join(files, " ")
	command := fmt.Sprintf("git add %s", fileList)
	return se.ExecuteCommand(ctx, command)
}

// GitCommit commits changes
func (se *ShellExecutor) GitCommit(ctx context.Context, message string) *ShellResult {
	command := fmt.Sprintf("git commit -m '%s'", message)
	return se.ExecuteCommand(ctx, command)
}

// GitPush pushes changes
func (se *ShellExecutor) GitPush(ctx context.Context, remote, branch string) *ShellResult {
	command := fmt.Sprintf("git push %s %s", remote, branch)
	return se.ExecuteCommand(ctx, command)
}

// GitPull pulls changes
func (se *ShellExecutor) GitPull(ctx context.Context, remote, branch string) *ShellResult {
	command := fmt.Sprintf("git pull %s %s", remote, branch)
	return se.ExecuteCommand(ctx, command)
}

// GitDiff shows git diff
func (se *ShellExecutor) GitDiff(ctx context.Context, files []string) *ShellResult {
	var command string
	if len(files) > 0 {
		fileList := strings.Join(files, " ")
		command = fmt.Sprintf("git diff %s", fileList)
	} else {
		command = "git diff"
	}
	return se.ExecuteCommand(ctx, command)
}

// Process management

// StartProcess starts a background process
func (se *ShellExecutor) StartProcess(ctx context.Context, command string) *ShellResult {
	bgCommand := fmt.Sprintf("%s &", command)
	return se.ExecuteCommand(ctx, bgCommand)
}

// KillProcess kills a process by PID
func (se *ShellExecutor) KillProcess(ctx context.Context, pid int) *ShellResult {
	command := fmt.Sprintf("kill %d", pid)
	return se.ExecuteCommand(ctx, command)
}

// GetProcessInfo gets information about a process
func (se *ShellExecutor) GetProcessInfo(ctx context.Context, pid int) *ShellResult {
	command := fmt.Sprintf("ps -p %d -o pid,ppid,cmd,etime,pcpu,pmem", pid)
	return se.ExecuteCommand(ctx, command)
}

// Network operations

// PingHost pings a host
func (se *ShellExecutor) PingHost(ctx context.Context, host string, count int) *ShellResult {
	command := fmt.Sprintf("ping -c %d %s", count, host)
	return se.ExecuteCommand(ctx, command)
}

// CurlURL makes an HTTP request
func (se *ShellExecutor) CurlURL(ctx context.Context, url string) *ShellResult {
	command := fmt.Sprintf("curl -s %s", url)
	return se.ExecuteCommand(ctx, command)
}

// Nslookup performs DNS lookup
func (se *ShellExecutor) Nslookup(ctx context.Context, domain string) *ShellResult {
	command := fmt.Sprintf("nslookup %s", domain)
	return se.ExecuteCommand(ctx, command)
}
