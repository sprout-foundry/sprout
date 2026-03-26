package tools

import "strings"

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

// SecurityResult contains the classification result for a tool call
type SecurityResult struct {
	Risk         SecurityRisk
	Reasoning    string
	ShouldBlock  bool
	ShouldPrompt bool
	IsHardBlock  bool
}

// Readonly tools map - package level to avoid recreation
var readonlyTools = map[string]bool{
	"read_file": true, "search_files": true, "web_search": true,
	"fetch_url": true, "browse_url": true,
	"analyze_ui_screenshot": true, "analyze_image_content": true,
	"view_history": true, "TodoRead": true, "TodoWrite": true,
	"list_skills": true, "run_subagent": true, "run_parallel_subagents": true,
	"glob": true, "list_directory": true, "get_file_info": true,
	"list_processes": true, "self_review": true,
}

// ClassifyToolCall classifies a tool call for security purposes
func ClassifyToolCall(toolName string, args map[string]interface{}) SecurityResult {
	if readonlyTools[toolName] {
		return SecurityResult{Risk: SecuritySafe, Reasoning: "Read-only operation"}
	}

	switch toolName {
	case "shell_command":
		return classifyShellCommand(args)
	case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		return classifyWriteOperation(args)
	case "git":
		return classifyGitOperation(args)
	default:
		return SecurityResult{Risk: SecurityCaution, Reasoning: "Unknown tool type - manual review recommended", ShouldPrompt: true}
	}
}

// classifyShellCommand classifies shell commands by risk level
func classifyShellCommand(args map[string]interface{}) SecurityResult {
	cmdRaw, ok := args["command"].(string)
	if !ok || cmdRaw == "" {
		return SecurityResult{Risk: SecurityCaution, Reasoning: "Empty or invalid command", ShouldPrompt: true}
	}

	cmd := strings.TrimSpace(cmdRaw)

	if isCriticalSystemOperation("shell_command", args) {
		return SecurityResult{Risk: SecurityDangerous, Reasoning: "Critical system operation detected", ShouldBlock: true, ShouldPrompt: true, IsHardBlock: true}
	}

	risks := classifyChainedCommand(cmd)
	maxRisk := maxRisk(risks)

	// Only DANGEROUS commands trigger blocking/prompts.
	// CAUTION commands (build tools, package managers, etc.) are auto-allowed.
	return SecurityResult{
		Risk:         maxRisk,
		Reasoning:    getShellCommandReasoning(cmd, maxRisk),
		ShouldBlock:  maxRisk == SecurityDangerous,
		ShouldPrompt: maxRisk == SecurityDangerous,
	}
}

// classifyChainedCommand splits and classifies chained commands
func classifyChainedCommand(cmd string) []SecurityRisk {
	// Check for pipe-to-shell patterns (case-insensitive to prevent bypass)
	cmdLower := strings.ToLower(cmd)
	for _, pipe := range []string{"| bash", "| sh", "| /bin/bash", "| /bin/sh", "|bash", "|sh", "|/bin/bash", "|/bin/sh"} {
		if strings.Contains(cmdLower, pipe) {
			return []SecurityRisk{SecurityDangerous}
		}
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
		strings.Contains(cmd, "> /boot/") || strings.Contains(cmd, ">> /boot/") ||
		strings.Contains(cmd, "> /dev/") || strings.Contains(cmd, ">> /dev/") {
		return SecurityDangerous
	}

	if isDangerousPattern(cmdLower) {
		return SecurityDangerous
	}

	if isSafeShellCommand(cmdLower) {
		return SecuritySafe
	}

	if isCautionPattern(cmdLower) {
		return SecurityCaution
	}

	return SecurityCaution
}

// isSafeShellCommand checks if a command is safe (read-only or workspace operations).
// Rejects commands with output redirection (> or >>) unless to /tmp/.
func isSafeShellCommand(cmd string) bool {
	// Reject commands with output redirection that target non-tmp paths
	if containsRedirection(cmd) && !isTmpRedirection(cmd) {
		return false
	}

	// Informational git
	safeGitPrefixes := []string{
		"git status", "git log", "git diff", "git show", "git branch",
		"git remote", "git config", "git stash list", "git tag",
		"git shortlog", "git blame", "git reflog",
	}
	for _, prefix := range safeGitPrefixes {
		if strings.HasPrefix(cmd, prefix+" ") || cmd == prefix {
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
		"locate": true, "time": true,
	}
	for c := range safeListCommands {
		if cmd == c || strings.HasPrefix(cmd, c+" ") {
			return true
		}
	}

	// grep/rg/egrep (read-only)
	if strings.HasPrefix(cmd, "grep ") || strings.HasPrefix(cmd, "egrep ") ||
		strings.HasPrefix(cmd, "fgrep ") || strings.HasPrefix(cmd, "rg ") {
		return true
	}

	// sed without -i (read-only)
	if strings.HasPrefix(cmd, "sed ") && !strings.Contains(cmd, "-i") {
		return true
	}

	// Go commands
	safeGoPrefixes := []string{
		"go build", "go test", "go run", "go fmt", "go vet",
		"go mod ", "go list", "go version", "go env",
		"go install", "go doc",
	}
	for _, prefix := range safeGoPrefixes {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}

	// Build and test commands (Node.js, Rust, Python, Java, Swift, etc.)
	safeBuildPrefixes := []string{
		"make test", "make build", "make check", "make lint",
		"npm run build", "npm run test", "npm run lint", "npm run check",
		"npm test", "npm run ", "npm ls", "npm outdated", "npm view",
		"npm pack", "npm audit",
		"cargo build", "cargo test", "cargo check", "cargo doc", "cargo clippy",
		"cargo fmt", "cargo metadata",
		"yarn build", "yarn test", "yarn lint", "yarn check",
		"pnpm build", "pnpm test", "pnpm lint",
		"pip list", "pip3 list", "pip show", "pip3 show", "pip install", "pip3 install",
		"python -m pytest", "python3 -m pytest",
		"pytest",
		"mvn test", "mvn compile", "mvn package",
		"gradle test", "gradle build", "gradle check",
		"bundle exec",
		"swift build", "swift test",
		"rustc ",
	}
	for _, prefix := range safeBuildPrefixes {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}

	// Network diagnostics
	safeNetworkPrefixes := []string{
		"curl", "wget", "ping ", "nslookup", "dig ", "traceroute",
		"nc -z", "nc -vz",
	}
	for _, prefix := range safeNetworkPrefixes {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}

	// System info/processes
	safeSystemPrefixes := []string{
		"systemctl status", "systemctl list-units", "journalctl",
		"docker ps", "docker images", "docker logs", "docker inspect",
		"kubectl get", "kubectl describe", "kubectl logs", "kubectl exec --",
		"tar tf", "zip -l", "unzip -l", "gzip -l",
	}
	for _, prefix := range safeSystemPrefixes {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}

	// Common workspace operations that are safe
	safeWorkspacePrefixes := []string{
		"mkdir -p", "touch ", "tee ",  // writing to workspace, not system dirs
		"cp ", "mv ",                   // workspace-level moves/copies
		"chmod ", "chown ",             // workspace permissions
	}
	for _, prefix := range safeWorkspacePrefixes {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}

	// Simple no-arg commands
	if cmd == "echo" || cmd == "true" || cmd == "false" || cmd == "pwd" || cmd == "ls" {
		return true
	}

	return false
}

// containsRedirection returns true if the command contains output redirection
// operators (>, >>) that could write to arbitrary paths
func containsRedirection(cmd string) bool {
	for i := 0; i < len(cmd); i++ {
		r := cmd[i]
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
		if r == '>' && i+1 < len(cmd) && cmd[i+1] == '>' {
			return true
		}
		if r == '>' && (i+1 >= len(cmd) || cmd[i+1] != '=') {
			if i == 0 || cmd[i-1] != '>' {
				return true
			}
		}
	}
	return false
}

// isTmpRedirection returns true if output redirection targets /tmp/
func isTmpRedirection(cmd string) bool {
	lower := strings.ToLower(cmd)
	return strings.Contains(lower, "> /tmp") || strings.Contains(lower, ">> /tmp") ||
		strings.Contains(lower, ">/tmp")
}

// isCautionPattern checks for caution-level patterns
func isCautionPattern(cmd string) bool {
	cautionPatterns := []string{
		"rm ", "git reset", "git rebase", "git cherry-pick",
		"git am", "git apply", "git clean", "npm install",
		"pip install", "go mod tidy", "sed -i", "perl -pi",
		"chmod", "chown", "systemctl start", "systemctl stop",
		"systemctl restart", "docker start", "docker stop",
		"docker restart", "docker rm", "rm -rf node_modules",
		"rm -rf vendor", "rm -rf dist", "rm -rf build",
		"rm -rf target", "rm -rf bin", "rm -rf out",
		"rm -rf __pycache__", "rm -rf .cache", "rm -rf .gradle",
		"rm -rf .next", "rm -rf venv", "rm -rf .venv",
		"rm -rf pods", "rm -rf .bundle", "rm -rf package-lock.json",
		"rm -rf go.sum", "rm -rf yarn.lock",
		"mv ", "cp ",
	}

	for _, pattern := range cautionPatterns {
		if strings.HasPrefix(cmd, pattern) {
			return true
		}
	}
	return false
}

// isDangerousPattern checks for dangerous patterns
func isDangerousPattern(cmd string) bool {
	if strings.HasPrefix(cmd, "eval ") || cmd == "eval" {
		return true
	}
	if strings.HasPrefix(cmd, "sudo ") {
		return true
	}
	if strings.Contains(cmd, "chmod 777") || strings.Contains(cmd, "chmod 666") {
		return true
	}

	// Pipe to shell (with space)
	for _, pipe := range []string{"| bash", "| sh", "| /bin/bash", "| /bin/sh", ">| bash", ">| sh"} {
		if strings.Contains(cmd, pipe) {
			return true
		}
	}

	// Pipe to shell (no space)
	for _, pipe := range []string{"|bash", "|sh", "|/bin/bash", "|/bin/sh"} {
		if strings.Contains(cmd, pipe) {
			return true
		}
	}

	// curl/wget piped to shell
	if (strings.Contains(cmd, "curl") || strings.Contains(cmd, "wget")) &&
		(strings.Contains(cmd, "| bash") || strings.Contains(cmd, "| sh")) {
		return true
	}

	// Dangerous git operations
	dangerousGit := []string{"git push --force", "git push -f", "git branch -D", "git branch -d", "git clean -ff", "git clean -fd", "git clean -ffd"}
	for _, op := range dangerousGit {
		if strings.HasPrefix(cmd, op) {
			return true
		}
	}

	// Permanent deletion targets
	deletionPatterns := []string{
		"rm -rf src/", "rm -rf src ", "rm -rf lib/", "rm -rf lib ",
		"rm -rf app/", "rm -rf app ", "rm -rf pkg/", "rm -rf pkg ",
		"rm -rf tests/", "rm -rf tests ", "rm -rf spec/", "rm -rf spec ",
		"rm -rf include/", "rm -rf include ", "rm -rf pages/", "rm -rf pages ",
		"rm -rf components/", "rm -rf components ", "rm -rf .git", "rm -rf .git ",
		"rm -rf ~/", "rm -rf ~/*",
	}
	for _, pattern := range deletionPatterns {
		if strings.HasPrefix(cmd, pattern) {
			return true
		}
	}

	// Dangerous system operations
	dangerousSys := []string{"mkfs", "dd if=/dev/zero", "dd if=/dev/urandom", "fdisk", "parted", "gparted", "init 0", "init 6", "reboot", "shutdown -h"}
	for _, op := range dangerousSys {
		if strings.Contains(cmd, op) {
			return true
		}
	}

	return false
}

// getShellCommandReasoning returns a human-readable reasoning string
func getShellCommandReasoning(cmd string, risk SecurityRisk) string {
	switch risk {
	case SecuritySafe:
		return "Read-only or safe workspace operation"
	case SecurityCaution:
		return "Potentially risky operation - review carefully"
	case SecurityDangerous:
		return "Dangerous operation detected - may cause data loss or system damage"
	default:
		return "Unknown operation type"
	}
}

// classifyWriteOperation classifies file write operations
func classifyWriteOperation(args map[string]interface{}) SecurityResult {
	pathRaw, ok := args["path"].(string)
	if !ok || pathRaw == "" {
		return SecurityResult{Risk: SecurityCaution, Reasoning: "Empty or invalid path", ShouldPrompt: true}
	}

	path := pathRaw

	// Check for critical system files and directories
	for _, critical := range []string{
		"/etc/shadow", "/etc/passwd", "/etc/sudoers", "/etc/ssh/sshd_config",
		"/root/.ssh/authorized_keys", "/etc/hosts", "/etc/resolv.conf",
		"/usr/", "/etc/", "/bin/", "/sbin/", "/var/", "/opt/", "/boot/", "/lib/", "/lib64/",
	} {
		if path == critical || strings.HasPrefix(path, critical) {
			return SecurityResult{Risk: SecurityDangerous, Reasoning: "Writing to critical system file or directory: " + path, ShouldBlock: true, ShouldPrompt: true, IsHardBlock: true}
		}
	}

	if strings.HasPrefix(path, "/tmp/") || path == "/tmp" {
		return SecurityResult{Risk: SecuritySafe, Reasoning: "Writing to temporary directory"}
	}

	return SecurityResult{Risk: SecuritySafe, Reasoning: "Workspace file operation"}
}

// classifyGitOperation classifies git operations
func classifyGitOperation(args map[string]interface{}) SecurityResult {
	opRaw, ok := args["operation"].(string)
	if !ok || opRaw == "" {
		return SecurityResult{Risk: SecurityCaution, Reasoning: "Empty or invalid git operation", ShouldPrompt: true}
	}

	op := strings.ToLower(strings.TrimSpace(opRaw))

	safeOps := []string{"commit", "add", "status", "log", "diff", "show", "branch", "remote", "stash", "tag", "revert", "fetch", "merge", "pull"}
	for _, safe := range safeOps {
		if op == safe {
			return SecurityResult{Risk: SecuritySafe, Reasoning: "Safe git operation: " + op}
		}
	}

	cautionOps := []string{"reset", "rebase", "cherry_pick", "am", "apply", "rm", "mv", "clean"}
	for _, caution := range cautionOps {
		if op == caution {
			return SecurityResult{Risk: SecurityCaution, Reasoning: "Git operation may affect history: " + op, ShouldPrompt: true}
		}
	}

	dangerousOps := []string{"branch_delete", "clean", "push --force", "push -f"}
	for _, danger := range dangerousOps {
		if op == danger || strings.HasPrefix(op, "push") && strings.Contains(opRaw, "--force") {
			return SecurityResult{Risk: SecurityDangerous, Reasoning: "Dangerous git operation that may force-push or delete: " + op, ShouldBlock: true, ShouldPrompt: true}
		}
	}

	return SecurityResult{Risk: SecurityCaution, Reasoning: "Unknown git operation: " + op, ShouldPrompt: true}
}

// isCriticalSystemOperation checks for critical system operations that should always be blocked
func isCriticalSystemOperation(toolName string, args map[string]interface{}) bool {
	if toolName != "shell_command" {
		return false
	}

	cmdRaw, ok := args["command"].(string)
	if !ok || cmdRaw == "" {
		return false
	}

	cmd := strings.TrimSpace(cmdRaw)

	// Check for rm -rf / or rm -rf . or rm -rf ~ or rm -rf * (with flags or no flags)
	// Split command into parts to handle variations like "rm -rf / --no-preserve-root" or "rm -rf  /"
	// Also handles "sudo rm -rf /" by checking any consecutive token pair.
	parts := strings.Fields(cmd)
	for i := 0; i+2 < len(parts); i++ {
		if strings.ToLower(parts[i]) == "rm" && strings.ToLower(parts[i+1]) == "-rf" {
			// Check if the next part is exactly /, ., ~, or *
			target := parts[i+2]
			if target == "/" || target == "." || target == "~" || target == "*" {
				return true
			}
		}
	}

	// mkfs and disk formatting
	if strings.HasPrefix(cmd, "mkfs") || strings.HasPrefix(cmd, "mkfs.") {
		return true
	}

	// Fork bomb
	if strings.Contains(cmd, "() { :|: }") || strings.Contains(cmd, "fork bomb") ||
		cmd == ":(){:|:&};" || strings.Contains(cmd, ":(){:|:&};:") {
		return true
	}

	// killall -9
	if strings.HasPrefix(cmd, "killall -9") || strings.HasPrefix(cmd, "killall -KILL") {
		return true
	}

	// chmod 000 /
	if strings.Contains(cmd, "chmod 000 /") || strings.Contains(cmd, "chmod 000 /usr") || strings.Contains(cmd, "chmod 000 /etc") {
		return true
	}

	// dd to primary disk
	primaryDisks := []string{"/dev/sda", "/dev/sdb", "/dev/nvme", "/dev/vda"}
	for _, disk := range primaryDisks {
		if strings.Contains(cmd, "dd if="+disk) || strings.Contains(cmd, "dd of="+disk) || strings.Contains(cmd, "dd if=/dev/zero of="+disk) {
			return true
		}
	}

	// Writes to critical system files
	for _, file := range []string{"/etc/shadow", "/etc/passwd", "/etc/sudoers", "/root/.ssh/authorized_keys"} {
		if strings.Contains(cmd, "echo "+file) || strings.Contains(cmd, "> "+file) || strings.Contains(cmd, ">> "+file) {
			return true
		}
	}

	return false
}

// maxRisk returns the maximum risk level from a slice
func maxRisk(risks []SecurityRisk) SecurityRisk {
	max := SecuritySafe
	for _, r := range risks {
		if r > max {
			max = r
		}
	}
	return max
}
