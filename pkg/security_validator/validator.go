package security_validator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/utils"
)

// RiskLevel represents the security risk level of an operation
type RiskLevel int

const (
	// RiskSafe means the operation is safe to execute immediately
	RiskSafe RiskLevel = 0
	// RiskCaution means the operation should be confirmed with the user
	RiskCaution RiskLevel = 1
	// RiskDangerous means the operation should be blocked or require explicit approval
	RiskDangerous RiskLevel = 2
)

// String returns the string representation of the risk level
func (r RiskLevel) String() string {
	switch r {
	case RiskSafe:
		return "SAFE"
	case RiskCaution:
		return "CAUTION"
	case RiskDangerous:
		return "DANGEROUS"
	default:
		return "UNKNOWN"
	}
}

// ValidationResult represents the result of a security validation
type ValidationResult struct {
	RiskLevel     RiskLevel `json:"risk_level"`
	Reasoning     string    `json:"reasoning"`
	Confidence    float64   `json:"confidence"`
	Timestamp     int64     `json:"timestamp"`
	ShouldBlock   bool      `json:"should_block"`
	ShouldConfirm bool      `json:"should_confirm"`
	IsSoftBlock   bool      `json:"is_soft_block"`
}

// Validator handles static heuristic-based security validation
type Validator struct {
	config      *configuration.SecurityValidationConfig
	logger      *utils.Logger
	interactive bool
	debug       bool
}

// NewValidator creates a new security validator using static heuristic rules
func NewValidator(cfg *configuration.SecurityValidationConfig, logger *utils.Logger, interactive bool) (*Validator, error) {
	if cfg == nil {
		return nil, fmt.Errorf("security validation config is nil")
	}

	return &Validator{
		config:      cfg,
		logger:      logger,
		interactive: interactive,
		debug:       false,
	}, nil
}

// ValidateToolCall evaluates whether a tool call is safe to execute using static heuristic rules
func (v *Validator) ValidateToolCall(ctx context.Context, toolName string, args map[string]interface{}) (*ValidationResult, error) {
	// If validation is disabled, return safe immediately
	if v.config == nil || !v.config.Enabled {
		return &ValidationResult{
			RiskLevel:     RiskSafe,
			Reasoning:     "Security validation is disabled",
			Confidence:    1.0,
			Timestamp:     time.Now().Unix(),
			ShouldBlock:   false,
			ShouldConfirm: false,
			IsSoftBlock:   false,
		}, nil
	}

	// Classify the command using static heuristic rules
	riskLevel := v.classifyCommand(toolName, args)
	reasoning := v.explainRisk(toolName, args, riskLevel)

	result := &ValidationResult{
		RiskLevel:     riskLevel,
		Reasoning:     reasoning,
		Confidence:    1.0,
		Timestamp:     time.Now().Unix(),
		IsSoftBlock:   false,
	}

	// Apply threshold logic
	result = v.applyThreshold(result)

	// Check for hard block operations (critical system-ruining operations) — always blocked, overrides threshold
	if IsCriticalSystemOperation(toolName, args) {
		result.ShouldBlock = true
		result.ShouldConfirm = false
		result.IsSoftBlock = false
		result.RiskLevel = RiskDangerous
		result.Reasoning = "Hard block: Critical system operation that could damage the operating system. " + reasoning
		return result, nil
	}

	// All non-critical blocks are soft blocks — user can override
	if result.ShouldConfirm || result.RiskLevel != RiskSafe {
		result.IsSoftBlock = true
	}

	// In non-interactive mode, DANGEROUS operations are blocked
	if result.RiskLevel == RiskDangerous && !v.interactive {
		result.ShouldBlock = true
		result.ShouldConfirm = false
	} else if result.RiskLevel == RiskDangerous && v.interactive && v.logger != nil {
		// Interactive DANGEROUS: prompt user
		prompt := fmt.Sprintf("⚠️  Security Validation Warning\n\nTool: %s\nArguments: %v\n\nRisk Level: %s\nReasoning: %s\n\nDo you want to proceed? (yes/no): ",
			toolName, args, result.RiskLevel, result.Reasoning)

		if v.logger.AskForConfirmation(prompt, false, false) {
			result.ShouldConfirm = false
			result.ShouldBlock = false
			result.IsSoftBlock = false
		} else {
			result.ShouldConfirm = false
			result.ShouldBlock = true
			result.IsSoftBlock = true
			result.Reasoning = "User rejected the operation based on security warning"
		}
	} else if result.ShouldConfirm && v.interactive && v.logger != nil {
		// Interactive CAUTION: prompt user
		prompt := fmt.Sprintf("⚠️  Security Validation Warning\n\nTool: %s\nArguments: %v\n\nRisk Level: %s\nReasoning: %s\n\nDo you want to proceed? (yes/no): ",
			toolName, args, result.RiskLevel, result.Reasoning)

		if v.logger.AskForConfirmation(prompt, false, false) {
			result.ShouldConfirm = false
			result.ShouldBlock = false
			result.IsSoftBlock = false
		} else {
			result.ShouldConfirm = false
			result.ShouldBlock = true
			result.IsSoftBlock = true
			result.Reasoning = "User rejected the operation based on security warning"
		}
	} else if result.RiskLevel == RiskCaution && v.logger != nil {
		// Non-interactive CAUTION: auto-allow with logging
		v.logger.Logf("🔒 Security validation: %s %v - CAUTION: %s", toolName, args, result.Reasoning)
	}

	return result, nil
}

// classifyCommand uses pattern matching to classify a tool call's risk level
func (v *Validator) classifyCommand(toolName string, args map[string]interface{}) RiskLevel {
	// Check IsCriticalSystemOperation first — always dangerous
	if IsCriticalSystemOperation(toolName, args) {
		return RiskDangerous
	}

	// Check isObviouslySafe — already static heuristic
	if isObviouslySafe(toolName, args) {
		return RiskSafe
	}

	// Classify by tool type
	switch toolName {
	case "read_file", "search_files", "fetch_url", "glob", "TodoRead", "TodoWrite",
		"analyze_image_content", "analyze_ui_screenshot", "list_skills", "activate_skill",
		"view_history", "self_review", "web_search", "list_directory", "get_file_info",
		"list_processes":
		return RiskSafe

	case "write_file", "edit_file", "patch_structured_file", "write_structured_file":
		return v.classifyWriteOperation(toolName, args)

	case "shell_command":
		return v.classifyShellCommand(args)

	case "run_subagent", "run_parallel_subagents":
		return RiskSafe

	case "git":
		return v.classifyGitOperation(args)

	default:
		// Unknown tools default to CAUTION
		return RiskCaution
	}
}

// classifyShellCommand classifies shell commands using pattern matching.
// For chained commands (&&, ||, ;, |), each sub-command is classified
// independently and the maximum risk level is returned.
func (v *Validator) classifyShellCommand(args map[string]interface{}) RiskLevel {
	command, ok := args["command"].(string)
	if !ok {
		return RiskCaution
	}
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return RiskCaution
	}

	cmdLower := strings.ToLower(cmd)

	// Check pipe-based dangerous patterns on the FULL command before splitting.
	// These are dangerous regardless of what each sub-command does individually.
	// e.g., "curl http://x | bash" — the pipe INTO bash/sh is the danger.
	if (strings.Contains(cmdLower, "curl ") || strings.Contains(cmdLower, "wget ")) &&
		(strings.Contains(cmdLower, "| bash") || strings.Contains(cmdLower, "| sh") ||
			strings.Contains(cmdLower, "|/bin/bash") || strings.Contains(cmdLower, "|/bin/sh")) {
		return RiskDangerous
	}

	// Split on command chaining operators and classify each part.
	// Take the maximum risk level across all sub-commands.
	subCmds := splitChainedCommands(cmd)
	maxRisk := RiskSafe
	for _, subCmd := range subCmds {
		subCmd = strings.TrimSpace(subCmd)
		if subCmd == "" {
			continue
		}
		cmdLower := strings.ToLower(subCmd)

		if v.isDangerousShellCommand(subCmd, cmdLower) {
			return RiskDangerous // Can't get worse
		}
		if v.isCautionShellCommand(subCmd, cmdLower) {
			maxRisk = RiskCaution
			continue
		}
		if maxRisk == RiskSafe && isSafeShellCommand(cmdLower) {
			continue // stays SAFE
		}
		if maxRisk == RiskSafe {
			maxRisk = RiskCaution // Couldn't classify → cautious
		}
	}
	return maxRisk
}

// splitChainedCommands splits a command string on &&, ||, ;, |, and newline
// operators while respecting single quotes, double quotes, $(...) subshell
// expressions (with parenthesis nesting), and backtick subshell expressions.
func splitChainedCommands(cmd string) []string {
	var results []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	inBacktick := false
	subShellDepth := 0 // depth of $(...) nesting
	runes := []rune(cmd)

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if inSingle {
			// Inside single quotes — nothing is special except closing quote
			current.WriteRune(r)
			if r == '\'' {
				inSingle = false
			}
			continue
		}

		if inDouble {
			// Inside double quotes — nothing is special except closing quote
			current.WriteRune(r)
			if r == '"' {
				inDouble = false
			}
			continue
		}

		if inBacktick {
			// Inside backticks — nothing is special except closing backtick
			current.WriteRune(r)
			if r == '`' {
				inBacktick = false
			}
			continue
		}

		// Not inside any quoting context from here on.
		current.WriteRune(r)

		// Track $( ... ) subshell nesting
		if r == '$' && i+1 < len(runes) && runes[i+1] == '(' {
			subShellDepth++
			continue
		}
		if r == '(' && subShellDepth > 0 {
			// Nested parens inside $()
			subShellDepth++
			continue
		}
		if r == ')' && subShellDepth > 0 {
			subShellDepth--
			continue
		}

		// At subShellDepth > 0 we are inside $(…) — do NOT split
		if subShellDepth > 0 {
			continue
		}

		// Check for &&
		if r == '&' && i+1 < len(runes) && runes[i+1] == '&' {
			results = append(results, current.String())
			current.Reset()
			i++ // skip second '&'
			continue
		}
		// Check for || (but not a single |)
		if r == '|' && i+1 < len(runes) && runes[i+1] == '|' {
			results = append(results, current.String())
			current.Reset()
			i++ // skip second '|'
			continue
		}
		// Check for ;
		if r == ';' {
			results = append(results, current.String())
			current.Reset()
			continue
		}
		// Check for single | (pipe)
		if r == '|' {
			results = append(results, current.String())
			current.Reset()
			continue
		}
		// Check for newline — treat as command separator
		if r == '\n' {
			results = append(results, current.String())
			current.Reset()
			continue
		}

		// Track entering double quotes, single quotes, backticks
		if r == '"' {
			inDouble = true
		} else if r == '\'' {
			inSingle = true
		} else if r == '`' {
			inBacktick = true
		}
	}
	if current.Len() > 0 {
		results = append(results, current.String())
	}
	return results
}

// isDangerousShellCommand checks if a shell command is dangerous
func (v *Validator) isDangerousShellCommand(cmd, cmdLower string) bool {
	// chmod 777 — insecure permissions
	if strings.Contains(cmdLower, "chmod") && strings.Contains(cmdLower, "777") {
		return true
	}

	// sudo with anything
	if strings.HasPrefix(cmdLower, "sudo ") {
		return true
	}

	// Writing to system directories — check that the system dir is the TARGET of the write,
	// not just mentioned anywhere in the command string
	if containsSystemDirWrite(cmd) {
		return true
	}

	// rm -rf on non-whitelisted paths (handles -rf, -r -f, -R -f, --recursive -f, --force combined with -r)
	if hasRecursiveRm(cmdLower) {
		normalized := normalizeWhitespace(cmdLower)
		if !v.isRecoverableRmRfTarget(normalized) && !v.isRecoverableRmRfTarget(cmdLower) {
			return true
		}
	}

	// git branch -D (force delete) — -D is case-sensitive in git; -d is safe
	if strings.Contains(cmd, " -D ") || strings.Contains(cmd, " --delete --force") {
		return true
	}
	if strings.Contains(cmdLower, "git branch -d ") && strings.Contains(cmdLower, "--force") {
		return true
	}

	// git clean -f (any form with -f: -fd, -ffd, -df, etc.)
	if strings.Contains(cmdLower, "git clean") && strings.Contains(cmdLower, "-f") {
		return true
	}

	// git push --force or -f (short form)
	if strings.Contains(cmdLower, "git push") && (strings.Contains(cmdLower, "--force") || strings.Contains(cmd, " -f ")) {
		return true
	}

	// Raw disk operations
	if strings.Contains(cmdLower, "> /dev/sd") || strings.Contains(cmdLower, "of=/dev/sd") {
		return true
	}

	// Pipe to bash with $() or backtick substitution
	if strings.Contains(cmdLower, "eval ") || strings.Contains(cmdLower, "exec ") {
		return true
	}

	// fork bombs
	if strings.Contains(cmdLower, ":(){:|:&};:") || strings.Contains(cmdLower, "bash -i >& /dev/tcp") {
		return true
	}

	return false
}

// hasRecursiveRm checks if the command is a recursive forced rm:
// matches -rf, -fr, -r -f, -R -f, --recursive --force, etc.
func hasRecursiveRm(cmdLower string) bool {
	if !strings.Contains(cmdLower, "rm ") {
		return false
	}
	// Combined flags: -rf, -fr, -fd, -df, etc.
	if strings.Contains(cmdLower, "-rf") || strings.Contains(cmdLower, "-fr") ||
		strings.Contains(cmdLower, "-Rf") || strings.Contains(cmdLower, "-fR") {
		return true
	}
	// Separated flags: -r -f, -R -f, --recursive -f, etc.
	hasR := strings.Contains(cmdLower, "-r") || strings.Contains(cmdLower, "-R") || strings.Contains(cmdLower, "--recursive")
	hasF := strings.Contains(cmdLower, " -f") || strings.Contains(cmdLower, "--force")
	return hasR && hasF
}

// isCautionShellCommand checks if a shell command warrants caution
func (v *Validator) isCautionShellCommand(cmd, cmdLower string) bool {
	// rm without -rf/-r (single file deletion)
	hasRm := strings.Contains(cmdLower, "rm ")
	if hasRm && !hasRecursiveRm(cmdLower) {
		return true
	}

	// rm -rf on recoverable targets (node_modules, vendor, dist, etc.)
	if hasRm && hasRecursiveRm(cmdLower) {
		normalized := normalizeWhitespace(cmdLower)
		if v.isRecoverableRmRfTarget(normalized) || v.isRecoverableRmRfTarget(cmdLower) {
			return true
		}
	}

	// git reset, rebase, amend
	if strings.HasPrefix(cmdLower, "git reset") || strings.HasPrefix(cmdLower, "git rebase") || strings.Contains(cmdLower, "git commit --amend") {
		return true
	}

	// git cherry-pick, stash drop/pop
	if strings.HasPrefix(cmdLower, "git cherry-pick") || strings.Contains(cmdLower, "git stash drop") || strings.Contains(cmdLower, "git stash pop") {
		return true
	}

	// Package installs
	packageInstallPrefixes := []string{
		"npm install", "yarn install", "yarn add", "pnpm install", "pnpm add",
		"pip install", "pip3 install", "pipenv install",
		"go get ", "go install ",
		"cargo install", "cargo add",
		"gem install", "bundle install",
		"composer require", "composer install",
		"apt-get install", "apt install",
		"brew install",
		"docker build", "docker-compose build", "docker compose build",
	}
	for _, prefix := range packageInstallPrefixes {
		if strings.HasPrefix(cmdLower, prefix) {
			return true
		}
	}

	// make clean
	if strings.HasPrefix(cmdLower, "make clean") {
		return true
	}

	// chmod (non-777)
	if strings.HasPrefix(cmdLower, "chmod ") && !strings.Contains(cmdLower, "777") {
		return true
	}

	// sed -i (in-place editing)
	if strings.Contains(cmdLower, "sed -i") || strings.Contains(cmdLower, "sed --in-place") {
		return true
	}

	// systemctl stop
	if strings.HasPrefix(cmdLower, "systemctl stop") {
		return true
	}

	// Build artifact deletion (not already in SAFE list)
	if strings.Contains(cmdLower, "rm ") && !strings.Contains(cmdLower, "-rf") {
		buildArtifactDirs := []string{"dist", "build", "out", "target", ".next", "__pycache__", "bin"}
		for _, dir := range buildArtifactDirs {
			if strings.Contains(cmd, dir) {
				return true
			}
		}
	}

	return false
}

// isRecoverableRmRfTarget checks if rm -rf targets a recoverable directory.
func (v *Validator) isRecoverableRmRfTarget(cmd string) bool {
	// Normalize whitespace to handle variations like "rm  -rf  node_modules"
	cmdLower := normalizeWhitespace(strings.ToLower(cmd))

	// Directories that are safe to rm -rf (dependencies, caches, build artifacts)
	recoverableDirs := []string{
		"node_modules", "vendor", "bundle", "pods", ".venv", "venv",
		"dist", "build", "out", "target", "bin",
		".next", "__pycache__", ".cache", ".gradle",
	}

	for _, dir := range recoverableDirs {
		patterns := []string{
			"rm -rf " + dir,
			"rm -rf ./" + dir,
			"rm -rf .\\" + dir,
			"rm -rf ~/" + dir,
		}
		for _, pattern := range patterns {
			if strings.Contains(cmdLower, pattern) {
				return true
			}
		}
	}

	// /tmp/* paths are always recoverable
	if strings.Contains(cmdLower, "/tmp/") {
		return true
	}

	// Lock files are recoverable
	lockFiles := []string{
		"package-lock.json", "yarn.lock", "pnpm-lock.yaml",
		"Podfile.lock", "Gemfile.lock", "Cargo.lock", "go.sum",
		"composer.lock", "pipfile.lock",
	}
	for _, lf := range lockFiles {
		if strings.Contains(cmd, lf) {
			return true
		}
	}

	return false
}

// classifyWriteOperation classifies file write/edit operations
func (v *Validator) classifyWriteOperation(toolName string, args map[string]interface{}) RiskLevel {
	filePath := ""
	for _, key := range []string{"path", "file_path"} {
		if val, ok := args[key].(string); ok && val != "" {
			filePath = val
			break
		}
	}

	if filePath == "" {
		return RiskCaution
	}

	cleanPath := filepath.Clean(filePath)

	// /tmp paths are always safe
	if strings.HasPrefix(cleanPath, "/tmp") || strings.HasPrefix(cleanPath, filepath.Clean(os.TempDir())) {
		return RiskSafe
	}

	// System directories are dangerous
	systemPaths := []string{"/usr", "/etc", "/bin", "/sbin", "/var", "/opt"}
	for _, sysDir := range systemPaths {
		if strings.HasPrefix(cleanPath, sysDir) {
			return RiskDangerous
		}
	}

	// Home directory root or system config files
	if cleanPath == "/etc/shadow" || cleanPath == "/etc/passwd" || cleanPath == "/etc/sudoers" {
		return RiskDangerous
	}

	// Default: write operations in workspace are SAFE (the filesystem security layer handles workspace boundaries)
	return RiskSafe
}

// classifyGitOperation classifies git write operations
func (v *Validator) classifyGitOperation(args map[string]interface{}) RiskLevel {
	operation, _ := args["operation"].(string)
	operation = strings.ToLower(strings.TrimSpace(operation))

	switch operation {
	case "commit", "add", "status", "log", "diff", "show", "branch", "remote", "stash", "tag", "revert":
		return RiskSafe
	case "reset", "rebase", "cherry_pick", "am", "apply":
		return RiskCaution
	case "branch_delete", "clean":
		return RiskDangerous
	case "push":
		// Check for force push
		if argsStr, ok := args["args"].(string); ok {
			if strings.Contains(strings.ToLower(argsStr), "--force") || strings.Contains(strings.ToLower(argsStr), "-f") {
				return RiskDangerous
			}
		}
		return RiskSafe
	case "rm", "mv":
		return RiskCaution
	default:
		return RiskCaution
	}
}

// explainRisk provides a human-readable explanation for the risk classification
func (v *Validator) explainRisk(toolName string, args map[string]interface{}, risk RiskLevel) string {
	switch risk {
	case RiskSafe:
		return fmt.Sprintf("%s is a safe, read-only or informational operation", toolName)
	case RiskCaution:
		return fmt.Sprintf("%s is a potentially risky operation — may modify state but is recoverable", toolName)
	case RiskDangerous:
		return fmt.Sprintf("%s is a dangerous operation — could cause permanent data loss or security issues", toolName)
	default:
		return "Unknown risk classification"
	}
}

// isSafeShellCommand checks if a shell command is unconditionally safe.
// Returns false for commands that use output redirection (> or >>), which
// could write to arbitrary locations.
func isSafeShellCommand(cmdLower string) bool {
	// Reject commands with output redirection — they could write anywhere
	if containsRedirection(cmdLower) {
		return false
	}

	// Informational git
	safeGitPrefixes := []string{
		"git status", "git log", "git diff", "git show", "git branch",
		"git remote", "git config", "git stash list", "git tag",
		"git shortlog", "git blame", "git reflog",
	}
	for _, prefix := range safeGitPrefixes {
		if strings.HasPrefix(cmdLower, prefix+" ") || cmdLower == prefix {
			return true
		}
	}

	// List/info commands
	safeListCommands := map[string]bool{
		"ls": true, "ll": true, "la": true,
		"find": true, "which": true, "whereis": true, "type": true,
		"cat": true, "head": true, "tail": true, "less": true, "more": true, "wc": true,
		"tree": true, "file": true, "stat": true,
		"du": true, "df": true,
		"ps": true, "top": true, "htop": true,
		"uname": true, "env": true, "printenv": true, "export": true,
		"echo": true, "pwd": true, "hostname": true, "date": true, "cal": true,
		"whoami": true, "id": true,
		"lsb_release": true, "lscpu": true, "free": true, "uptime": true,
		"basename": true, "dirname": true, "realpath": true,
	}
	for cmd := range safeListCommands {
		if cmdLower == cmd || strings.HasPrefix(cmdLower, cmd+" ") {
			return true
		}
	}

	// Build and test commands
	safeBuildPrefixes := []string{
		"go build", "go test", "go run", "go fmt", "go vet",
		"go mod ", "go list", "go version", "go env",
		"make test", "make build", "make check", "make lint",
		"npm run build", "npm run test", "npm run lint", "npm run check",
		"npm test", "npm run ", "npm ls", "npm outdated",
		"cargo build", "cargo test", "cargo check", "cargo doc", "cargo clippy",
		"cargo fmt", "cargo metadata",
		"yarn build", "yarn test", "yarn lint", "yarn check",
		"pnpm build", "pnpm test", "pnpm lint",
		"pip list", "pip3 list", "pip show", "pip3 show",
		"python -m pytest", "python3 -m pytest",
		"pytest",
		"mvn test", "mvn compile", "mvn package",
		"gradle test", "gradle build", "gradle check",
		"bundle exec",
		"swift build", "swift test",
		"rustc ",
	}
	for _, prefix := range safeBuildPrefixes {
		if strings.HasPrefix(cmdLower, prefix) {
			return true
		}
	}

	// grep/rg/egrep (read-only)
	if strings.HasPrefix(cmdLower, "grep ") || strings.HasPrefix(cmdLower, "egrep ") ||
		strings.HasPrefix(cmdLower, "fgrep ") || strings.HasPrefix(cmdLower, "rg ") {
		return true
	}

	// sed without -i (read-only)
	if strings.HasPrefix(cmdLower, "sed ") && !strings.Contains(cmdLower, "-i") {
		return true
	}

	// Simple no-arg commands
	if cmdLower == "echo" || cmdLower == "true" || cmdLower == "false" || cmdLower == "pwd" || cmdLower == "ls" {
		return true
	}

	return false
}

// normalizeWhitespace collapses multiple whitespace characters into single spaces
var multipleSpaces = regexp.MustCompile(`\s+`)

func normalizeWhitespace(s string) string {
	return strings.TrimSpace(multipleSpaces.ReplaceAllString(s, " "))
}

// containsRedirection returns true if the command contains output redirection
// operators (>, >>) that could write to arbitrary paths
func containsRedirection(cmd string) bool {
	for i := 0; i < len(cmd); i++ {
		r := cmd[i]
		// Skip inside quotes
		if r == '\'' {
			for i++; i < len(cmd) && cmd[i] != '\''; i++ {
			}
			continue
		}
		if r == '"' {
			for i++; i < len(cmd) && cmd[i] != '"'; i++ {
			}
			continue
		}
		// Check for >> (append redirect)
		if r == '>' && i+1 < len(cmd) && cmd[i+1] == '>' {
			return true
		}
		// Check for > (write redirect) but not >= or > used in HTML tags
		if r == '>' && (i+1 >= len(cmd) || cmd[i+1] != '=' && cmd[i+1] != ' ') ||
			(r == '>' && i+1 < len(cmd) && cmd[i+1] == ' ') {
			// Make sure it's not part of a >> that we already caught
			if i == 0 || cmd[i-1] != '>' {
				return true
			}
		}
	}
	return false
}

// containsSystemDirWrite returns true if the command redirects output to a system directory.
// This checks that the system dir appears as the TARGET of a write operation (after > or >>),
// not just mentioned anywhere in the command string.
func containsSystemDirWrite(cmd string) bool {
	systemDirs := []string{"/usr", "/etc", "/bin", "/sbin", "/var", "/opt"}
	lower := strings.ToLower(cmd)

	for _, dir := range systemDirs {
		// Check patterns: "> /dir", ">> /dir", ">/dir", '>>"dir'
		redirects := []string{
			"> " + dir,
			">>" + " " + dir,
			">" + dir,
			">>" + dir,
		}
		for _, pattern := range redirects {
			if strings.Contains(lower, pattern) {
				return true
			}
		}
	}
	return false
}

// isObviouslySafe checks if an operation is clearly safe without deeper analysis
// This pre-filters read-only and informational operations
func isObviouslySafe(toolName string, args map[string]interface{}) bool {
	// Check if the operation is on /tmp/* paths
	if isInTmpPath(toolName, args) {
		return true
	}

	// List of obviously safe tools (read-only and informational)
	safeTools := map[string]bool{
		"read_file":      true,
		"glob":           true,
		"search_files":   true,
		"fetch_url":      true,
		"list_directory": true,
		"TodoRead":       true,
		"TodoWrite":      true,
	}

	// Check if tool is in the safe list
	if safeTools[toolName] {
		return true
	}

	return false
}

// SetDebug enables or disables debug mode
func (v *Validator) SetDebug(debug bool) {
	v.debug = debug
}

// applyThreshold applies the configured threshold to the validation result
func (v *Validator) applyThreshold(result *ValidationResult) *ValidationResult {
	threshold := v.config.Threshold
	if threshold < 0 {
		threshold = 1 // Default to cautious
	} else if threshold > 2 {
		threshold = 2
	}

	// Threshold meanings:
	// - Threshold 0: Only SAFE (risk 0) operations are auto-confirmed
	// - Threshold 1: SAFE is auto-confirmed, CAUTION/DANGEROUS need confirmation
	// - Threshold 2: SAFE and CAUTION are auto-confirmed, only DANGEROUS needs confirmation

	// For all thresholds: operations with risk >= threshold require confirmation
	if int(result.RiskLevel) >= threshold {
		// Exception: threshold 0 and risk 0 should not require confirmation
		if !(threshold == 0 && result.RiskLevel == RiskSafe) {
			result.ShouldBlock = false
			result.ShouldConfirm = true
			return result
		}
	}

	// Risk level below threshold: allow without confirmation
	result.ShouldBlock = false
	result.ShouldConfirm = false
	return result
}

// isInTmpPath checks if an operation is on /tmp/* paths
func isInTmpPath(toolName string, args map[string]interface{}) bool {
	// Check file_path/path argument for file operations
	for _, key := range []string{"file_path", "path"} {
		if filePath, ok := args[key].(string); ok {
			cleanPath := filepath.Clean(filePath)
			if strings.HasPrefix(cleanPath, "/tmp/") || cleanPath == "/tmp" {
				return true
			}
			if strings.Contains(strings.ToLower(cleanPath), "\\temp\\") ||
				strings.Contains(strings.ToLower(cleanPath), "\\tmp\\") {
				return true
			}
		}
	}

	// For shell commands, check if the command operates on /tmp
	if toolName == "shell_command" {
		if command, ok := args["command"].(string); ok {
			commandLower := strings.ToLower(command)
			if strings.Contains(commandLower, "/tmp/") ||
				strings.Contains(commandLower, " /tmp ") ||
				strings.Contains(commandLower, "> /tmp") ||
				strings.Contains(commandLower, "< /tmp") ||
				strings.HasPrefix(strings.TrimSpace(commandLower), "rm /tmp") ||
				strings.HasPrefix(strings.TrimSpace(commandLower), "rm -rf /tmp") {
				return true
			}
		}
	}

	return false
}

// IsCriticalSystemOperation checks if this is a critical system operation that should be hard blocked.
// These are the few operations that would permanently damage the operating system.
// This function is public so it can be called independently for a pre-filter check.
func IsCriticalSystemOperation(toolName string, args map[string]interface{}) bool {
	// For shell commands, check for critical system operations
	if toolName == "shell_command" {
		if command, ok := args["command"].(string); ok {
			commandLower := strings.ToLower(strings.TrimSpace(command))

			// Filesystem destruction commands
			if strings.HasPrefix(commandLower, "mkfs") ||
				commandLower == "rm -rf /" ||
				commandLower == "rm -rf ." ||
				strings.HasPrefix(commandLower, ":(){:|:&};:") ||
				strings.HasPrefix(commandLower, "killall -9") ||
				strings.HasPrefix(commandLower, "chmod 000 /") {
				return true
			}

			// Only block fdisk/parted on the primary disk
			if (strings.HasPrefix(commandLower, "fdisk ") || strings.HasPrefix(commandLower, "parted ")) &&
				strings.Contains(commandLower, "/dev/sda") {
				return true
			}

			// Block dd operations on primary disk
			if strings.Contains(commandLower, "dd ") {
				if (strings.Contains(commandLower, "dd if=/dev/zero") || strings.Contains(commandLower, "dd if=/dev/random")) &&
					strings.Contains(commandLower, "/dev/sda") {
					return true
				}
			}
		}
	}

	// For file operations, check for critical authentication/config files
	if toolName == "write_file" || toolName == "edit_file" {
		if filePath, ok := args["file_path"].(string); ok {
			cleanPath := filepath.Clean(filePath)
			if cleanPath == "/etc/shadow" ||
				cleanPath == "/etc/passwd" ||
				cleanPath == "/etc/sudoers" ||
				cleanPath == "/etc/sudoers.d/" {
				return true
			}
		}
	}

	return false
}
