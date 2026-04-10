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
	RiskType     string // Risk category for user-facing messages
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

// ClassifyToolCall classifies a tool call for security purposes based on the
// tool name and its arguments. It returns a SecurityResult indicating the risk
// level, reasoning, and whether the operation should be blocked or prompt the user.
//
// Classification is purely string-based (no filesystem access). See the
// package-level documentation for known limitations of this approach.
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
		return SecurityResult{
			Risk: SecurityDangerous, Reasoning: "Critical system operation detected",
			ShouldBlock: true, ShouldPrompt: true, IsHardBlock: true,
			RiskType: getShellCommandRiskType(cmd, SecurityDangerous, true),
		}
	}

	risks := classifyChainedCommand(cmd)
	maxRisk := maxRisk(risks)
	isPrivilegedInstall := containsPrivilegedPackageInstall(cmd)

	// Only DANGEROUS commands trigger blocking/prompts.
	// Exception: privileged package installation is CAUTION but still prompts.
	shouldPrompt := maxRisk == SecurityDangerous || isPrivilegedInstall
	return SecurityResult{
		Risk:         maxRisk,
		Reasoning:    getShellCommandReasoning(cmd, maxRisk),
		ShouldBlock:  maxRisk == SecurityDangerous,
		ShouldPrompt: shouldPrompt,
		RiskType:     getShellCommandRiskType(cmd, maxRisk, isCriticalSystemOperation("shell_command", args)),
	}
}

// classifyChainedCommand splits and classifies chained commands
func classifyChainedCommand(cmd string) []SecurityRisk {
	if risk, ok := classifyReadOnlyForLoop(cmd); ok {
		return []SecurityRisk{risk}
	}

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

	if isPrivilegedPackageInstall(cmdLower) {
		return SecurityCaution
	}

	if isDangerousPattern(cmdLower) {
		return SecurityDangerous
	}

	// Check caution patterns BEFORE safe patterns, so that specific
	// caution-level commands (like "docker rm") override broad safe matches.  // lint:allow_duplicate_imports
	if isCautionPattern(cmdLower) {
		return SecurityCaution
	}

	if isSafeShellCommand(cmdLower) {
		return SecuritySafe
	}

	return SecurityCaution
}

// isSafeShellCommand checks if a command is safe (read-only or workspace operations).
// Rejects commands with output redirection (> or >>) unless to /tmp/.
func isSafeShellCommand(cmd string) bool {
	// Reject commands with output redirection that target non-tmp paths
	if containsRedirection(cmd) && !isBenignRedirection(cmd) {
		return false
	}

	// Git commands (broadened: dangerous patterns like --force still caught by isDangerousPattern)
	safeGitPrefixes := []string{
		"git status", "git log", "git diff", "git show", "git branch",
		"git remote", "git config", "git stash", "git tag",
		"git shortlog", "git blame", "git reflog",
		"git switch", "git checkout", "git restore", "git add",
		"git commit", "git push", "git pull", "git fetch", "git merge",
		"git rebase", "git cherry-pick", "git revert",
		"git am", "git apply", "git reset",
		"git stash pop", "git stash drop", "git stash apply",
		"git stash branch", "git stash clear", "git stash show",
		"git worktree", "git bisect", "git submodule", "git filter-branch",
		"git notes", "git describe", "git rev-parse", "git rev-list",
		"git ls-files", "git ls-tree", "git ls-remote",
		"git for-each-ref", "git name-rev",
		"git format-patch", "git send-email", "git request-pull",
		"git archive", "git bundle",
		"git clean", "git rm", "git mv",
		"git init", "git clone",
		"git sparse-checkout", "git replace", "git rerere",
	}
	for _, prefix := range safeGitPrefixes {
		if strings.HasPrefix(cmd, prefix+" ") || cmd == prefix {
			return true
		}
	}

	// List/info commands and development tools
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
		// Text processing
		"cd": true, "diff": true, "awk": true, "sort": true, "uniq": true,
		"tr": true, "cut": true, "column": true,
		// Encoding/hashing utilities
		"xxd": true, "base64": true,
		"sha256sum": true, "sha1sum": true, "md5sum": true,
		// Language runtimes and compilers
		"python": true, "python3": true,
		"ruby": true, "php": true, "perl": true,
		"java": true, "javac": true,
		"dotnet": true,
		"gcc":    true, "g++": true, "cc": true, "c++": true, "clang": true, "clang++": true, "gfortran": true,
		// Node.js/npm tools
		"npm": true, "npx": true, "tsc": true, "node": true, "pnpm": true,
		// Shells
		"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true,
		// Infrastructure/DevOps
		"terraform": true, "ansible-playbook": true, "ansible": true,
		"helm": true, "kustomize": true,
		"az": true, "aws": true, "gcloud": true, "doctl": true,
		// Container tools
		"docker": true, "docker-compose": true, "podman": true, "nerdctl": true,
		"kind": true, "minikube": true,
		// Kubernetes
		"kubectl": true, "k9s": true,
		// Database tools
		"psql": true, "mysql": true, "sqlite3": true, "mongosh": true,
		"redis-cli": true, "mongodump": true, "mongorestore": true,
		// Linux package managers
		"brew": true, "apt": true, "dpkg": true, "snap": true,
		"yum": true, "dnf": true, "apk": true,
		// Archives
		"tar": true, "zip": true, "unzip": true, "gzip": true,
		"gunzip": true, "bzip2": true, "xz": true, "7z": true, "zstd": true,
		// Network
		"ssh": true, "scp": true, "rsync": true, "sftp": true,
		"gitleaks": true, "trivy": true,
		// Build tools and linters
		"make": true, "cmake": true, "ninja": true, "meson": true,
		"webpack": true, "vite": true, "rollup": true, "esbuild": true,
		"prettier": true, "eslint": true, "biome": true, "ruff": true,
		"black": true, "isort": true, "mypy": true, "pylint": true,
		"flake8": true, "pyright": true,
		"gofumpt": true, "golangci-lint": true,
		"shellcheck": true, "hadolint": true,
		// Version control CLIs
		"gh": true, "glab": true,
		// Misc dev tools
		"jq": true, "yq": true, "tomlq": true,
		"open": true, "xdg-open": true,
		"sleep": true, "wait": true,
		"strip": true, "objdump": true, "nm": true, "strings": true,
		"ldd": true, "pkg-config": true,
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

	// sed (safe for all usage in workspace context)
	if strings.HasPrefix(cmd, "sed ") {
		return true
	}

	// Go commands
	safeGoPrefixes := []string{
		"go build", "go test", "go run", "go fmt", "go vet",
		"go mod ", "go list", "go version", "go env",
		"go install", "go doc", "go tool ", "go generate",
		"go get ", "go work ", "go clean", "go cover",
		"go cgo", "go bug",
	}
	for _, prefix := range safeGoPrefixes {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}

	// Build and test commands (Node.js, Rust, Python, Java, Swift, etc.)
	safeBuildPrefixes := []string{
		"make test", "make build", "make check", "make lint",
		"make clean", "make all", "make install", "make run", "make deploy",
		"make fmt", "make tidy", "make generate", "make docs", "make vet",
		"make update", "make migrate", "make seed", "make serve", "make dev",
		"npm run build", "npm run test", "npm run lint", "npm run check",
		"npm test", "npm run ", "npm ls", "npm outdated", "npm view",
		"npm pack", "npm audit",
		"npm start", "npm stop", "npm restart",
		"npm init", "npm version", "npm publish",
		"npm root", "npm bin", "npm cache ", "npm config ",
		"npm dedupe", "npm fund", "npm rebuild", "npm shrinkwrap",
		"npm explore ", "npm link", "npm search",
		"npm update", "npm whoami", "npm ci",
		"cargo build", "cargo test", "cargo check", "cargo doc", "cargo clippy",
		"cargo fmt", "cargo metadata",
		"cargo run", "cargo install", "cargo add", "cargo remove",
		"cargo update", "cargo search", "cargo tree", "cargo publish",
		"cargo bench", "cargo clean",
		"yarn build", "yarn test", "yarn lint", "yarn check", "yarn ",
		"pnpm build", "pnpm test", "pnpm lint", "pnpm ",
		"npx tsc", "npx ",
		"deno ", "bun ",
		"pip list", "pip3 list", "pip show", "pip3 show", "pip install", "pip3 install",
		"pip uninstall", "pip3 uninstall",
		"pip freeze", "pip3 freeze", "pip check", "pip3 check",
		"pip cache ", "pip3 cache ",
		"pipenv install", "pipenv lock", "pipenv run",
		"poetry install", "poetry add", "poetry run", "poetry build",
		"poetry publish", "poetry update", "poetry lock",
		"uv ", "uvx ",
		"hatch ",
		"virtualenv",
		"python -m pytest", "python3 -m pytest",
		"python -m ", "python3 -m ",
		"python ", "python3 ",
		"pytest",
		"tox ", "nox ",
		"mvn test", "mvn compile", "mvn package",
		"mvn install", "mvn clean", "mvn deploy", "mvn verify",
		"gradle test", "gradle build", "gradle check",
		"gradle clean", "gradle bootRun", "gradle jar", "gradle war",
		"bundle exec", "bundle install", "bundle update", "bundle check",
		"bundle package", "bundle show", "bundle list",
		"gem install", "gem build", "gem push",
		"rake ", "rails ", "rspec ",
		"swift build", "swift test", "swift run", "swift package", "swift format",
		"rustc ",
		"dotnet build", "dotnet test", "dotnet run",
		"dotnet publish", "dotnet clean", "dotnet restore",
		"dotnet add ", "dotnet remove ", "dotnet tool ", "dotnet format",
		"dotnet watch run", "dotnet ef ",
		"terraform ",
		"docker build", "docker run", "docker push", "docker pull",
		"docker-compose up", "docker-compose down", "docker-compose build",
		"docker-compose logs", "docker-compose ps", "docker-compose exec",
		"docker system ", "docker network ", "docker volume ",
		"gh ", "glab ",
		"turbo run ", "turbo build ", "turbo test ", "nx ",
	}
	for _, prefix := range safeBuildPrefixes {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}

	// Network diagnostics
	safeNetworkPrefixes := []string{
		"curl", "wget",
		"ping ", "ping6",
		"nslookup", "dig ", "host ", "traceroute", "tracepath",
		"nc -z", "nc -vz",
		"ssh ", "scp ", "rsync ", "sftp ",
		"gitleaks ", "trivy ",
	}
	for _, prefix := range safeNetworkPrefixes {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}

	// System info/processes
	safeSystemPrefixes := []string{
		"systemctl status", "systemctl list-units", "systemctl is-active",
		"systemctl is-enabled", "systemctl show",
		"systemctl start", "systemctl stop", "systemctl restart",
		"journalctl",
		"docker ps", "docker images", "docker logs", "docker inspect",
		"docker network ls", "docker volume ls", "docker system df",
		"docker start", "docker stop", "docker restart",
		"kubectl ", // broadened: matches all subcommands
		"tar tf", "zip -l", "unzip -l", "gzip -l",
	}
	for _, prefix := range safeSystemPrefixes {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}

	// Common workspace operations that are safe
	safeWorkspacePrefixes := []string{
		"mkdir -p", "touch ", "tee ", // writing to workspace, not system dirs
		"cp ", "mv ", "ln ", // workspace-level moves/copies/symlinks
		"chmod ", "chown ", "chgrp ", // workspace permissions
		"strip ", "install ",
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

func extractCommandSubstitutions(cmd string) []string {
	var subs []string
	for i := 0; i < len(cmd); i++ {
		if cmd[i] != '$' || i+1 >= len(cmd) || cmd[i+1] != '(' {
			continue
		}
		start := i + 2
		depth := 1
		j := start
		for ; j < len(cmd); j++ {
			switch cmd[j] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					sub := strings.TrimSpace(cmd[start:j])
					if sub != "" {
						subs = append(subs, sub)
					}
					i = j
					break
				}
			}
			if depth == 0 {
				break
			}
		}
	}
	return subs
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
		// File descriptor duplication (2>&1, 1>&2, etc.) is not file output redirection
		if r == '>' && i+1 < len(cmd) && cmd[i+1] == '&' {
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

// isBenignRedirection returns true if output redirection targets known harmless sinks.
func isBenignRedirection(cmd string) bool {
	lower := strings.ToLower(cmd)
	return strings.Contains(lower, "> /tmp") || strings.Contains(lower, ">> /tmp") ||
		strings.Contains(lower, ">/tmp") ||
		strings.Contains(lower, "> /dev/null") || strings.Contains(lower, ">> /dev/null") ||
		strings.Contains(lower, ">/dev/null") ||
		strings.Contains(lower, "> /dev/stdout") || strings.Contains(lower, ">> /dev/stdout") ||
		strings.Contains(lower, "> /dev/stderr") || strings.Contains(lower, ">> /dev/stderr")
}

// isCautionPattern checks for caution-level patterns.
// This is deliberately minimal — almost everything is SAFE now.
// Only true deletion operations remain as CAUTION (never DANGEROUS).
func isCautionPattern(cmd string) bool {
	cautionPatterns := []string{
		"rm ",       // single file deletion (rm without -rf flag; rm -rf commands use safeRmRfPrefixes whitelist)
		"docker rm", // container deletion
	}

	for _, pattern := range cautionPatterns {
		if strings.HasPrefix(cmd, pattern) {
			return true
		}
	}
	return false
}

// safeRmRfPrefixes is a set of safe "rm -rf " (and "rm -fr ") command prefixes
// for common development cleanup tasks (e.g., node_modules, build artifacts).
// Only commands matching these exact prefixes bypass DANGEROUS classification.
//
// Uses map[string]bool for O(1) lookup instead of linear slice scan.
// Each entry must explicitly include both "rm -rf dir/" and "rm -rf dir "
// variants because "rm -rf dir" (no trailing char) is intentionally left
// unmatched and classified as DANGEROUS for safety.
//
// See package-level documentation for limitations of this prefix-based approach
// (no symlink following, no path normalization, no env variable expansion, etc.).
var safeRmRfPrefixes = map[string]bool{
	// Build artifacts and caches
	"rm -rf node_modules/": true, "rm -rf node_modules ": true,
	"rm -rf vendor/": true,       "rm -rf vendor ": true,
	"rm -rf dist/": true,         "rm -rf dist ": true,
	"rm -rf build/": true,        "rm -rf build ": true,
	"rm -rf target/": true,       "rm -rf target ": true,
	"rm -rf bin/": true,          "rm -rf bin ": true,
	// Python caches
	"rm -rf __pycache__/": true,  "rm -rf __pycache__ ": true,
	// Dotfile caches and tool dirs
	"rm -rf .cache/": true,       "rm -rf .cache ": true,
	"rm -rf .gradle/": true,       "rm -rf .gradle ": true,
	"rm -rf .next/": true,         "rm -rf .next ": true,
	"rm -rf .npm/": true,          "rm -rf .npm ": true,
	"rm -rf .yarn/": true,         "rm -rf .yarn ": true,
	"rm -rf .pnpm/": true,         "rm -rf .pnpm ": true,
	"rm -rf .m2/": true,           "rm -rf .m2 ": true,
	"rm -rf .ivy/": true,          "rm -rf .ivy ": true,
	"rm -rf .sbt/": true,          "rm -rf .sbt ": true,
	"rm -rf .parcel-cache/": true, "rm -rf .parcel-cache ": true,
	"rm -rf .turbo/": true,        "rm -rf .turbo ": true,
	"rm -rf .nuxt/": true,         "rm -rf .nuxt ": true,
	"rm -rf .output/": true,       "rm -rf .output ": true,
	"rm -rf .astro/": true,        "rm -rf .astro ": true,
	"rm -rf .svelte-kit/": true,   "rm -rf .svelte-kit ": true,
	"rm -rf .sass-cache/": true,   "rm -rf .sass-cache ": true,
	"rm -rf .stylelintcache/": true, "rm -rf .stylelintcache ": true,
	"rm -rf .eslintcache/": true,  "rm -rf .eslintcache ": true,
	"rm -rf .swc/": true,          "rm -rf .swc ": true,
	"rm -rf .vercel/": true,       "rm -rf .vercel ": true,
	"rm -rf .netlify/": true,      "rm -rf .netlify ": true,
	"rm -rf .firebase/": true,     "rm -rf .firebase ": true,
	"rm -rf .serverless/": true,   "rm -rf .serverless ": true,
	// Infrastructure/DevOps dots
	"rm -rf .terraform/": true,    "rm -rf .terraform ": true,
	"rm -rf .aws/": true,          "rm -rf .aws ": true,
	"rm -rf .kube/": true,         "rm -rf .kube ": true,
	"rm -rf .docker/": true,       "rm -rf .docker ": true,
	"rm -rf .docker-compose/": true, "rm -rf .docker-compose ": true,
	// IDE/editor config dirs
	"rm -rf .idea/": true,         "rm -rf .idea ": true,
	"rm -rf .vscode/": true,       "rm -rf .vscode ": true,
	"rm -rf .project/": true,      "rm -rf .project ": true,
	"rm -rf .settings/": true,     "rm -rf .settings ": true,
	"rm -rf .metadata/": true,     "rm -rf .metadata ": true,
	// Virtual environments
	"rm -rf venv/": true,          "rm -rf venv ": true,
	"rm -rf .venv/": true,         "rm -rf .venv ": true,
}

// isSafeRmRfPrefix checks if a lowercased command matches one of the safe
// rm -rf prefixes in O(1). It checks both "rm -rf " and "rm -fr " variants.
func isSafeRmRfPrefix(cmdLower string) bool {
	// Only check if it's an rm -rf command at all
	if !strings.HasPrefix(cmdLower, "rm -rf ") && !strings.HasPrefix(cmdLower, "rm -fr ") {
		return false
	}

	// Normalize to "rm -rf " for map lookup
	normalized := cmdLower
	if strings.HasPrefix(cmdLower, "rm -fr ") {
		normalized = "rm -rf " + cmdLower[len("rm -fr "):]
	}

	// Try direct map lookup — covers exact matches like "rm -rf node_modules/"
	if safeRmRfPrefixes[normalized] {
		return true
	}

	// For commands like "rm -rf node_modules/sub/path", check each possible
	// prefix by scanning for "/" or " " in the target. Since map lookups are O(1),
	// this is still bounded by path depth (typically <10 characters to scan).
	for i := len("rm -rf "); i < len(normalized); i++ {
		c := normalized[i]
		if c == '/' || c == ' ' {
			prefix := normalized[:i+1] // include the separator for exact map match
			if safeRmRfPrefixes[prefix] {
				return true
			}
			break // only check the first path component
		}
	}
	return false
}

// isDangerousPattern checks for dangerous patterns
func isDangerousPattern(cmd string) bool {
	cmdLower := strings.ToLower(cmd)
	if strings.HasPrefix(cmd, "eval ") || cmd == "eval" {
		return true
	}
	if strings.HasPrefix(cmd, "sudo ") && !isPrivilegedPackageInstall(cmd) {
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

	// Check for rm -rf or rm -fr (case-insensitive) - default to dangerous
	// Check if an rm -rf target is safe (O(1) map lookup)
	if isSafeRmRfPrefix(cmdLower) {
		return false
	}
	// All other rm -rf commands not in the safe allowlist are dangerous
	if strings.HasPrefix(cmdLower, "rm -rf ") || strings.HasPrefix(cmdLower, "rm -fr ") {
		return true
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

func isPrivilegedPackageInstall(cmd string) bool {
	normalized := strings.TrimSpace(strings.ToLower(cmd))
	installPrefixes := []string{
		"sudo apt-get install",
		"sudo apt install",
		"sudo yum install",
		"sudo dnf install",
		"sudo brew install",
		"sudo snap install",
		"sudo flatpak install",
		"sudo apk add",
	}
	for _, prefix := range installPrefixes {
		if normalized == prefix || strings.HasPrefix(normalized, prefix+" ") {
			return true
		}
	}
	return false
}

func containsPrivilegedPackageInstall(cmd string) bool {
	parts := strings.FieldsFunc(cmd, func(r rune) bool {
		return r == ';' || r == '|'
	})
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		subparts := strings.Split(part, "&&")
		for _, sub := range subparts {
			for _, candidate := range strings.Split(sub, "||") {
				if isPrivilegedPackageInstall(candidate) {
					return true
				}
			}
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
		if containsPrivilegedPackageInstall(cmd) {
			return "Privileged package installation requested - review before continuing"
		}
		return "Potentially risky operation - review carefully"
	case SecurityDangerous:
		return "Dangerous operation detected - may cause data loss or system damage"
	default:
		return "Unknown operation type"
	}
}

// getShellCommandRiskType returns a risk category string for user-facing messages
func getShellCommandRiskType(cmd string, risk SecurityRisk, isCritical bool) string {
	if risk != SecurityDangerous {
		return ""
	}

	cmdLower := strings.ToLower(cmd)

	// rm -rf . or ~ or / or * or .git — mass deletion (check this first for specificity)
	for _, pattern := range []string{"rm -rf .", "rm -rf ~", "rm -rf /", "rm -rf *", "rm -rf .git"} {
		if strings.HasPrefix(cmdLower, pattern) {
			return "mass_deletion"
		}
	}

	// rm -rf of source/project directories (more specific than general dir deletion)
	for _, pattern := range []string{
		"rm -rf src/", "rm -rf src ", "rm -rf lib/", "rm -rf lib ",
		"rm -rf app/", "rm -rf app ", "rm -rf pkg/", "rm -rf pkg ",
		"rm -rf tests/", "rm -rf tests ", "rm -rf spec/", "rm -rf spec ",
		"rm -rf include/", "rm -rf include ", "rm -rf pages/", "rm -rf pages ",
		"rm -rf components/", "rm -rf components ",
	} {
		if strings.HasPrefix(cmdLower, pattern) {
			return "source_code_destruction"
		}
	}

	// rm -rf of arbitrary directories (general directory deletion)
	// Check this last, after more specific patterns above
	if (strings.HasPrefix(cmdLower, "rm -rf ") || strings.HasPrefix(cmdLower, "rm -fr ")) && !isSafeRmRfPrefix(cmdLower) {
		return "directory_deletion"
	}

	if strings.HasPrefix(cmdLower, "sudo ") {
		return "privilege_escalation"
	}
	if strings.Contains(cmd, "chmod 777") || strings.Contains(cmd, "chmod 666") {
		return "insecure_permissions"
	}
	if (strings.Contains(cmd, "curl") || strings.Contains(cmd, "wget")) &&
		(strings.Contains(cmd, "| bash") || strings.Contains(cmd, "| sh")) {
		return "remote_code_execution"
	}
	if strings.HasPrefix(cmdLower, "eval ") || cmd == "eval" {
		return "arbitrary_code_execution"
	}
	if strings.HasPrefix(cmdLower, "git push --force") || strings.HasPrefix(cmdLower, "git push -f") {
		return "destructive_git_operation"
	}
	if strings.HasPrefix(cmdLower, "git branch -d") || strings.HasPrefix(cmdLower, "git branch -D") {
		return "destructive_git_operation"
	}
	if strings.Contains(cmd, "() { :|: }") || strings.Contains(cmd, "fork bomb") {
		return "system_instability"
	}
	if strings.HasPrefix(cmdLower, "killall -9") {
		return "system_instability"
	}
	if strings.HasPrefix(cmd, "mkfs") {
		return "disk_destruction"
	}
	if strings.HasPrefix(cmd, "fdisk") || strings.HasPrefix(cmd, "parted") || strings.HasPrefix(cmd, "gparted") {
		return "disk_destruction"
	}
	if strings.Contains(cmd, "dd if=/dev/zero") || strings.Contains(cmd, "dd if=/dev/urandom") ||
		strings.Contains(cmd, "dd of=/dev/sda") || strings.Contains(cmd, "dd of=/dev/sdb") ||
		strings.Contains(cmd, "dd of=/dev/nvme") {
		return "disk_destruction"
	}
	// Output redirection to system directories
	for _, dir := range []string{"/etc/", "/usr/", "/bin/", "/sbin/", "/var/", "/opt/", "/boot/", "/root/"} {
		if strings.Contains(cmd, "> "+dir) || strings.Contains(cmd, ">> "+dir) {
			return "system_integrity"
		}
	}
	if (strings.Contains(cmd, "> /dev/") || strings.Contains(cmd, ">> /dev/")) &&
		!strings.Contains(cmd, "> /dev/null") && !strings.Contains(cmd, ">> /dev/null") &&
		!strings.Contains(cmd, "> /dev/stdout") && !strings.Contains(cmd, ">> /dev/stdout") &&
		!strings.Contains(cmd, "> /dev/stderr") && !strings.Contains(cmd, ">> /dev/stderr") {
		return "system_integrity"
	}

	// Fall back to generic critical system operation for anything caught by isCriticalSystemOperation
	if isCritical {
		return "critical_system_operation"
	}

	return ""
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
			return SecurityResult{Risk: SecurityDangerous, Reasoning: "Writing to critical system file or directory: " + path, ShouldBlock: true, ShouldPrompt: true, IsHardBlock: true, RiskType: "system_integrity"}
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

	safeOps := []string{"commit", "add", "status", "log", "diff", "show", "branch", "remote", "stash", "tag", "revert", "fetch", "merge", "pull", "push"}
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

	// Note: "clean" is intentionally only CAUTION-level here. Dangerous variants
	// like "git clean -ff" and "git clean -fd" are caught by the shell-level
	// security classifier (isDangerousPattern), which processes the full git
	// command string including flags.
	dangerousOps := []string{"branch_delete", "push --force", "push -f"}
	for _, danger := range dangerousOps {
		if op == danger || (strings.HasPrefix(op, "push") && strings.Contains(opRaw, "--force")) {
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
