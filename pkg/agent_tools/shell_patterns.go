package tools

import (
	"strings"
)

// shell_patterns.go — the core shell-command safety classification: what
// isSafeShellCommand admits, and the dangerous / caution pattern sets it
// rejects. Split out of the original monolith; pure move.

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
		"git cherry-pick", "git revert",
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
	// Common workspace operations that are safe.
	// NOTE: This block relies on isDangerousPattern (called first in classifySingleCommand)
	// for source-path validation of multi-path commands like cp/mv. If a command has any
	// system path argument, isDangerousPattern catches it before reaching this block.
	for _, prefix := range safeWorkspacePrefixes {
		if strings.HasPrefix(cmd, prefix) {
			argsAfterCmd := cmd[len(prefix):]
			// Check ALL arguments (not just destination) to catch:
			//   cp /etc/shadow /tmp/stolen   (unsafe source)
			//   cp config.txt /etc/config    (unsafe destination)
			if hasSystemPathTarget(argsAfterCmd) {
				return false // targets system path — NOT safe
			}
			return true
		}
	}

	// Simple no-arg commands
	if cmd == "echo" || cmd == "true" || cmd == "false" || cmd == "pwd" || cmd == "ls" {
		return true
	}

	return false
}

// isDangerousPattern checks for genuinely dangerous patterns that can cause
// irreversible system damage or data loss. Only operations targeting system
// directories or raw devices belong here.
//
// Operations that are risky but recoverable (rm -rf of project dirs, eval,
// chmod 777, git push --force, curl|bash) are handled by isCautionPattern
// instead — they prompt the user but don't hard-block.
func isDangerousPattern(cmd string) bool {
	cmdLower := strings.ToLower(cmd)

	// Strip a leading "sudo " prefix for dangerous-pattern evaluation
	// so that sudo-prefixed destructive commands are still detected.
	// e.g., "sudo rm -rf /etc" is evaluated as "rm -rf /etc".
	evalCmd := cmdLower
	if strings.HasPrefix(evalCmd, "sudo ") {
		evalCmd = evalCmd[5:]
	}

	// Dangerous system operations that damage disks or crash the system.
	// (mkfs and dd to block devices are also caught by IsCriticalOperation,
	// but fdisk/parted/init/shutdown are NOT — so we keep them here.)
	dangerousSys := []string{"mkfs", "dd if=/dev/zero", "dd if=/dev/urandom", "fdisk", "parted", "gparted", "init 0", "init 6", "reboot", "shutdown -h"}
	for _, op := range dangerousSys {
		if strings.Contains(evalCmd, op) {
			return true
		}
	}

	// Check for workspace commands targeting system directories.
	// This catches cp/mv/chmod/etc. that modify files in /etc/, /usr/, etc.
	prefixes := []string{"chmod ", "chown ", "chgrp ", "cp ", "mv ", "mkdir -p", "touch ", "tee ", "ln ", "install ", "strip "}
	for _, prefix := range prefixes {
		if strings.HasPrefix(evalCmd, prefix) {
			argsAfterCmd := evalCmd[len(prefix):]
			if hasSystemPathTarget(argsAfterCmd) {
				return true
			}
		}
	}

	return false
}

// isCautionPattern checks for caution-level patterns — operations that are
// risky but recoverable. These prompt the user for approval but do not
// hard-block. They include:
//   - rm -rf / rm -fr of non-whitelisted directories (whitelisted dirs are SAFE)
//   - rm (single file deletion)
//   - docker rm (container deletion)
//   - eval (dynamic code execution)
//   - chmod 777 / chmod 666 (insecure permissions)
//   - curl/wget piped to shell (remote code execution)
//   - Dangerous git operations (push --force, branch -D, clean -ff/-fd)
//
// Note: rm -rf of whitelisted safe directories (node_modules/, dist/, etc.)
// is checked earlier in classifySingleCommand via isSafeRmRfPrefix and
// returns SAFE before this function is reached.
func isCautionPattern(cmd string) bool {
	cmdLower := strings.ToLower(cmd)

	// sudo (non-install) — privilege escalation. Privileged package
	// installs are caught earlier by isPrivilegedPackageInstall in
	// classifySingleCommand; this catches all other sudo usage
	// (sudo systemctl, sudo rm, sudo cat /etc/shadow, etc.).
	if isSudoCommand(cmdLower) {
		return true
	}

	// Process termination — kills running processes by PID or name.
	// killall -9 is caught as DANGEROUS by isCriticalSystemOperation
	// earlier; this catches non-9 variants and kill/pkill.
	for _, prefix := range []string{"kill ", "pkill ", "killall "} {
		if strings.HasPrefix(cmdLower, prefix) || cmdLower == strings.TrimSpace(prefix) {
			return true
		}
	}

	// Service/container state changes — stop, restart, kill, or remove
	// running resources. These are stateful operations that can disrupt
	// running services or destroy container resources.
	stateChangePrefixes := []string{
		"systemctl stop ", "systemctl restart ", "systemctl disable ",
		"service ", // SysV init: "service nginx stop"
		"docker stop ", "docker kill ", "docker rm ",
		"docker rmi ", "docker volume rm ", "docker network rm ",
	}
	for _, prefix := range stateChangePrefixes {
		if strings.HasPrefix(cmdLower, prefix) {
			return true
		}
	}

	// File content destruction — truncates or overwrites file contents
	// without removing the file itself.
	for _, prefix := range []string{"truncate ", "shred "} {
		if strings.HasPrefix(cmdLower, prefix) || cmdLower == strings.TrimSpace(prefix) {
			return true
		}
	}

	// Output redirection to non-system paths — shell file write.
	// System directory redirections (> /etc/, > /dev/sda, etc.) are
	// caught as DANGEROUS earlier in classifySingleCommand. Benign
	// redirections (/tmp/, /dev/null, /dev/stdout) are excluded by
	// isBenignRedirection. This catches `echo x > file.txt` style
	// writes that bypass the write_file/edit_file tools.
	if containsRedirection(cmdLower) && !isBenignRedirection(cmdLower) {
		return true
	}

	// eval — executes a dynamically-constructed string
	if strings.HasPrefix(cmdLower, "eval ") || cmdLower == "eval" {
		return true
	}

	// Insecure world-writable permissions
	if strings.Contains(cmdLower, "chmod 777") || strings.Contains(cmdLower, "chmod 666") {
		return true
	}

	// curl/wget piped to a shell or interpreter — remote code execution
	if isPipeToShell(cmdLower) {
		return true
	}

	// Dangerous git operations — history-rewriting or force operations
	dangerousGit := []string{
		"git push --force", "git push -f",
		"git branch -d", "git branch -D",
		"git clean -ff", "git clean -fd", "git clean -ffd",
		"git rebase", // history-rewriting — never safe to run unattended
	}
	for _, op := range dangerousGit {
		if strings.HasPrefix(cmdLower, op) {
			return true
		}
	}

	// rm -rf / rm -fr of non-whitelisted directories.
	// Whitelisted dirs (node_modules/, dist/, etc.) return SAFE earlier
	// in classifySingleCommand via isSafeRmRfPrefix.
	if strings.HasPrefix(cmdLower, "rm -rf ") || strings.HasPrefix(cmdLower, "rm -fr ") {
		return true
	}

	// Single file deletion and container removal
	cautionPatterns := []string{
		"rm ",       // single file deletion (rm without -rf/-fr)
		"docker rm", // container deletion
	}
	for _, pattern := range cautionPatterns {
		if strings.HasPrefix(cmdLower, pattern) {
			return true
		}
	}
	return false
}
