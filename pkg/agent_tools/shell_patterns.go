package tools

import (
	"path"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

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

// xargsShortFlagsWithSeparateValue is the set of xargs short flags whose
// value is supplied as the next token (e.g. `xargs -n 4 cmd`). Other
// short flags either take an embedded value (`-I{}`, `-n4`, `-eEOF`)
// or no value (`-0`, `-r`, `-t`, `-v`, `-x`).
//
// Keys are lowercase; the caller always passes cmdLower.
//
// `-i` (lowercase) is included despite its GNU xargs(1) semantics of
// "optional value" (shorthand for `-I {}`). We treat it as value-taking
// because the more common usage `xargs -i REPL cmd ...` benefits from
// consuming REPL — that lets the stripper correctly identify `cmd` as
// the inner command. The cost: `xargs -i grep p` parses as inner `p`
// (because we consume `grep` as REPL) instead of inner `grep p`. The
// resulting CAUTION is conservative (false negative in the safe
// direction), which we accept.
var xargsShortFlagsWithSeparateValue = map[string]bool{
	"-a": true, // --arg-file=FILE
	"-d": true, // --delimiter=DELIM
	"-e": true, // --eof[=EOF-STR] (lowercased from -E)
	"-i": true, // --replace[=REPLSTR] (shorthand; GNU spec says optional)
	"-I": true, // --replace=REPLSTR (uppercase I is required-argument)
	"-l": true, // --max-lines[=MAX-LINES]
	"-L": true, // --max-lines=MAX-LINES
	"-n": true, // --max-args=MAX-ARGS
	"-s": true, // --max-chars=MAX-CHARS
}

// xargsLongFlagsWithSeparateValue is the set of xargs long flags whose
// value is supplied as the next token (e.g. `xargs --max-args 4 cmd`).
// Long flags may also use the embedded `--flag=value` form, which is
// detected by a literal `=` in the token.
var xargsLongFlagsWithSeparateValue = map[string]bool{
	"--arg-file":         true,
	"--delimiter":        true,
	"--eof":              true,
	"--max-args":         true,
	"--max-chars":        true,
	"--max-lines":        true,
	"--max-procs":        true,
	"--open-tty":         true,
	"--process-slot-var": true,
	"--replace":          true,
}

// classifyXargsInvocation returns the risk level of `xargs <inner-cmd>` and
// a bool indicating whether the input matched an xargs invocation at all.
// When ok is false, the caller should fall through to other classifiers.
//
// xargs is dangerous only insofar as the command it invokes is dangerous.
// We strip xargs flags and recursively classify the inner command. For
// shell-interpreter invocations (xargs sh -c "...") we can't statically
// inspect the body, so we return CAUTION regardless of the inner script.
//
// Critical-system check: if the inner command would be a critical operation
// when run directly (e.g. `rm -rf /`), we elevate the xargs invocation to
// DANGEROUS. Without this, `xargs rm -rf /` would slip through as CAUTION
// because the inner classification (rm -rf /) itself doesn't reach the
// critical-operation detector — that detector runs at the top level of
// classifyShellCommand and uses invokesCommand semantics that don't know
// about xargs. Adding the inner check here preserves the DANGEROUS
// classification for cases the top-level detector handles coincidentally
// (via the sudo-prefix path) and extends it to bare `xargs rm -rf /`.
//
// Recursion is bounded to depth 1: a second `xargs` keyword in the inner
// command is classified by the recursive call (which will fall through to
// default-CAUTION there, which is the safe behavior).
func classifyXargsInvocation(cmdLower string) (SecurityRisk, bool) {
	if cmdLower == "xargs" {
		// Bare `xargs` reads stdin until EOF and runs whatever lines it
		// finds — ambiguous, stay cautious.
		return SecurityCaution, true
	}
	const prefix = "xargs "
	if !strings.HasPrefix(cmdLower, prefix) {
		return SecuritySafe, false
	}

	inner := stripXargsFlags(cmdLower[len(prefix):])
	inner = strings.TrimSpace(inner)
	if inner == "" {
		// `xargs` with only flags and no inner command — degenerate,
		// treat like bare xargs.
		return SecurityCaution, true
	}

	// Shell-interpreter carve-out. xargs sh -c "...", xargs bash -c "...",
	// etc. The script body is opaque to a static classifier.
	head := strings.Fields(inner)[0]
	switch head {
	case "sh", "bash", "zsh", "dash", "fish", "ksh", "csh", "tcsh":
		return SecurityCaution, true
	}

	// Critical-system elevation. If the inner command is a known critical
	// operation (rm -rf /, mkfs, dd to a block device, fork bomb, etc.),
	// the xargs invocation is also critical — there is no scenario where
	// running `xargs rm -rf /` should be less dangerous than running
	// `rm -rf /` directly. This is the canonical check used by
	// classifyShellCommand's isCriticalSystemOperation path; we replicate
	// it here because the recursion strips xargs before that gate sees
	// the command.
	if configuration.IsCriticalOperation(inner) {
		return SecurityDangerous, true
	}

	return classifySingleCommand(inner), true
}

// stripXargsFlags removes xargs flag tokens from the front of cmdLower and
// returns the remaining inner command as a single space-joined string.
//
// Recognized forms:
//   - bare short flags:       -0 -r -t -v -x
//   - short flag + value:     -n 4, -I REPL, -d DELIM, -s SIZE, ...
//   - short flag with embedded value: -n4, -I{}, -eEOF
//   - bare long flags:        --null --no-run-if-empty --verbose --help ...
//   - long flag + value:      --max-args 4 --replace REPLSTR ...
//   - long flag with =value:  --max-args=4 --replace=REPLSTR
//
// The token `--` ends flag parsing and is preserved as part of the inner
// command (xargs treats `--` as a positional-args separator).
//
// This is intentionally narrow — we don't try to be a complete xargs parser.
// The goal is to handle the common forms seen in agent-generated pipelines:
//   xargs du -sh
//   xargs -n 1 wc -l
//   xargs -I{} grep pattern {}
//   xargs -0 du -shc --files0-from=-
//   xargs --null du -sh
//   xargs --max-args=4 du -sh
//
// Unknown flags consume the token only (conservative: anything we don't
// recognize as value-taking is treated as bare, so we never accidentally
// classify the flag's value as the inner command).
func stripXargsFlags(s string) string {
	tokens := strings.Fields(s)
	out := make([]string, 0, len(tokens))
	consumeNext := false
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if consumeNext {
			consumeNext = false
			continue
		}
		// `--` ends flag parsing; preserve in inner command.
		if tok == "--" {
			out = append(out, tokens[i:]...)
			return strings.Join(out, " ")
		}
		// Single `-` (stdin) or non-flag token: start of inner command.
		if !strings.HasPrefix(tok, "-") {
			out = append(out, tokens[i:]...)
			return strings.Join(out, " ")
		}
		// Long flag.
		if strings.HasPrefix(tok, "--") {
			name := tok
			if eq := strings.IndexByte(tok, '='); eq >= 0 {
				name = tok[:eq]
			}
			if xargsLongFlagsWithSeparateValue[name] && !strings.Contains(tok, "=") {
				// value is the next token
				consumeNext = true
			}
			// else: --flag=value (embedded) or bare long flag — token consumed
			continue
		}
		// Short flag. Form A: bare single letter (`-0`, `-r`, `-t`, `-v`, `-x`).
		// Form B: clustered with embedded value (`-n4`, `-I{}`, `-eEOF`).
		// Form C: clustered bare (`-0rt`, `-rt`).
		//
		// We can't always disambiguate B vs C from the cluster alone, but
		// GNU getopt-style clusters always embed the value with the FIRST
		// short option in the cluster, and the rest of the cluster is that
		// option's value. For our purposes: if the second char is a digit
		// or non-letter, treat the whole rest of the cluster as the value
		// of the first flag (Form B). Otherwise treat as a bare flag
		// cluster (Form C).
		if len(tok) == 2 {
			// Single-letter short flag.
			if xargsShortFlagsWithSeparateValue[tok] {
				consumeNext = true
			}
			// else: bare flag, token consumed
			continue
		}
		// Cluster. Inspect first char.
		firstChar := "-" + string(tok[1])
		if xargsShortFlagsWithSeparateValue[firstChar] {
			// Form B: value is embedded in the cluster (e.g., `-n4`).
			// Token consumed.
			continue
		}
		// Form C: bare flag cluster (`-0rt`, `-rv`). Token consumed.
		continue
	}
	return strings.Join(out, " ")
}

// isSudoCommand reports whether cmd is a sudo-prefixed command.
func isSudoCommand(cmd string) bool {
	cmdLower := strings.ToLower(strings.TrimSpace(cmd))
	return strings.HasPrefix(cmdLower, "sudo ")
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
	"rm -rf vendor/": true, "rm -rf vendor ": true,
	"rm -rf dist/": true, "rm -rf dist ": true,
	"rm -rf build/": true, "rm -rf build ": true,
	"rm -rf target/": true, "rm -rf target ": true,
	"rm -rf bin/": true, "rm -rf bin ": true,
	// Python caches
	"rm -rf __pycache__/": true, "rm -rf __pycache__ ": true,
	// Dotfile caches and tool dirs
	"rm -rf .cache/": true, "rm -rf .cache ": true,
	"rm -rf .gradle/": true, "rm -rf .gradle ": true,
	"rm -rf .next/": true, "rm -rf .next ": true,
	"rm -rf .npm/": true, "rm -rf .npm ": true,
	"rm -rf .yarn/": true, "rm -rf .yarn ": true,
	"rm -rf .pnpm/": true, "rm -rf .pnpm ": true,
	"rm -rf .m2/": true, "rm -rf .m2 ": true,
	"rm -rf .ivy/": true, "rm -rf .ivy ": true,
	"rm -rf .sbt/": true, "rm -rf .sbt ": true,
	"rm -rf .parcel-cache/": true, "rm -rf .parcel-cache ": true,
	"rm -rf .turbo/": true, "rm -rf .turbo ": true,
	"rm -rf .nuxt/": true, "rm -rf .nuxt ": true,
	"rm -rf .output/": true, "rm -rf .output ": true,
	"rm -rf .astro/": true, "rm -rf .astro ": true,
	"rm -rf .svelte-kit/": true, "rm -rf .svelte-kit ": true,
	"rm -rf .sass-cache/": true, "rm -rf .sass-cache ": true,
	"rm -rf .stylelintcache/": true, "rm -rf .stylelintcache ": true,
	"rm -rf .eslintcache/": true, "rm -rf .eslintcache ": true,
	"rm -rf .swc/": true, "rm -rf .swc ": true,
	"rm -rf .vercel/": true, "rm -rf .vercel ": true,
	"rm -rf .netlify/": true, "rm -rf .netlify ": true,
	"rm -rf .firebase/": true, "rm -rf .firebase ": true,
	"rm -rf .serverless/": true, "rm -rf .serverless ": true,
	// Infrastructure/DevOps dots
	"rm -rf .terraform/": true, "rm -rf .terraform ": true,
	"rm -rf .aws/": true, "rm -rf .aws ": true,
	"rm -rf .kube/": true, "rm -rf .kube ": true,
	"rm -rf .docker/": true, "rm -rf .docker ": true,
	"rm -rf .docker-compose/": true, "rm -rf .docker-compose ": true,
	// IDE/editor config dirs
	"rm -rf .idea/": true, "rm -rf .idea ": true,
	"rm -rf .vscode/": true, "rm -rf .vscode ": true,
	"rm -rf .project/": true, "rm -rf .project ": true,
	"rm -rf .settings/": true, "rm -rf .settings ": true,
	"rm -rf .metadata/": true, "rm -rf .metadata ": true,
	// Virtual environments
	"rm -rf venv/": true, "rm -rf venv ": true,
	"rm -rf .venv/": true, "rm -rf .venv ": true,
}

// safeRmRfComponents is a set of known safe directory names that can appear
// anywhere in a path. A path like "internal/api/webui/dist/sprout-webui" is safe
// because it contains "dist" as a path component, even though "dist" is nested.
// This set is checked by isSafeRmRfComponent for nested path matching.
var safeRmRfComponents = map[string]bool{
	// Common build output directories
	"dist": true, "build": true, "out": true, "target": true, "bin": true,
	// Package manager caches
	"node_modules": true, "vendor": true,
	// Dotfile caches
	"__pycache__": true, ".cache": true, ".gradle": true, ".next": true,
	".npm": true, ".yarn": true, ".pnpm": true, ".m2": true, ".ivy": true, ".sbt": true,
	".parcel-cache": true, ".turbo": true, ".nuxt": true, ".output": true,
	".astro": true, ".svelte-kit": true, ".sass-cache": true, ".stylelintcache": true,
	".eslintcache": true, ".swc": true, ".vercel": true, ".netlify": true,
	".firebase": true, ".serverless": true,
	// Infrastructure/DevOps
	".terraform": true, ".aws": true, ".kube": true, ".docker": true, ".docker-compose": true,
	// IDE/config
	".idea": true, ".vscode": true, ".project": true, ".settings": true, ".metadata": true,
	// Virtual environments
	"venv": true, ".venv": true,
}

// isSafeRmRfPrefix checks if a lowercased command matches one of the safe
// rm -rf prefixes in O(1). It checks both "rm -rf " and "rm -fr " variants.
//
// Matching is done in two passes:
//  1. Exact prefix match: checks if the command target matches a known safe directory
//     at the top level (e.g., "rm -rf dist/", "rm -rf node_modules/sub/path")
//  2. Component match: checks if ANY path component in the target is a known safe
//     directory name (e.g., "rm -rf internal/api/webui/dist/sprout-webui" is safe
//     because "dist" is a path component)
//
// Path traversal components ("..") and absolute paths are NOT allowed in component
// matching to prevent bypassing the safe directory check.
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

	// Extract the target path (everything after "rm -rf ")
	target := normalized[len("rm -rf "):]

	// Hard reject any path containing traversal ("..") regardless of
	// whether it passes a prefix or component match below. Without this,
	// "rm -rf dist/../etc" would pass the prefix check (the loop finds
	// "rm -rf dist/" and the map has that as a safe prefix) and silently
	// classify as SAFE even though ".." escapes the safe directory.
	if strings.Contains(target, "..") {
		return false
	}

	// Try direct map lookup — covers exact matches like "rm -rf node_modules/"
	if safeRmRfPrefixes[normalized] {
		return true
	}

	// For commands like "rm -rf node_modules/sub/path", check each possible
	// prefix by scanning for "/" or " " in the target. Since map lookups are O(1),
	// this is still bounded by path depth (typically <10 characters to scan).
	for i := 0; i < len(target); i++ {
		c := target[i]
		if c == '/' || c == ' ' {
			prefix := "rm -rf " + target[:i+1] // include the separator for exact map match
			if safeRmRfPrefixes[prefix] {
				// Reject if the remainder of the path (after the safe
				// prefix) contains ".." — a path-traversal escape that
				// would let the user delete a directory outside the
				// whitelisted safe dir (e.g., "rm -rf dist/../etc" must
				// not be whitelisted by matching "rm -rf dist/").
				remainder := target[i+1:]
				if strings.Contains(remainder, "..") {
					return false
				}
				return true
			}
			break // only check the first path component
		}
	}

	// Phase 1: Component-based matching for nested paths.
	// Split the target path into components and check if any match a safe directory.
	// Skip path traversal ("..") and absolute paths to stay conservative.
	if isSafeRmRfComponent(target) {
		return true
	}

	return false
}

// isSafeRmRfComponent checks if any path component in the given path matches
// a known safe directory name. Returns false for empty paths, path traversal,
// or absolute paths to be conservative.
//
// A path is considered safe only when:
//   - It contains no path-traversal components ("..") anywhere
//   - It is not absolute (no leading "/")
//   - It is not composed entirely of "." components
//   - Any single path component matches a known safe directory name
//     (e.g., "dist", "node_modules")
//   - The matching safe component is NOT the last component — there must
//     be additional content after it (the same convention as the existing
//     prefix whitelist, which requires a trailing "/" or " ").
//
// Examples:
//   - "internal/api/webui/dist/sprout-webui" → true (contains "dist" with more after it)
//   - "dist/sprout-webui" → true (contains "dist" with more after it)
//   - "node_modules/package" → true (contains "node_modules" with more after it)
//   - "internal/api/" → false (no safe component)
//   - "../sibling-project" → false (path traversal)
//   - "dist/../etc" → false (path traversal escapes safe dir)
//   - "internal/api/webui/dist/../etc" → false (traversal escapes)
//   - "dist/." → false (trailing "." with no real content after safe dir)
//   - "/tmp/something" → false (absolute path)
//   - "dist" → false (safe component but nothing follows it)
func isSafeRmRfComponent(target string) bool {
	if target == "" {
		return false
	}

	// Reject absolute paths (conservative: only workspace-relative paths are safe)
	if strings.HasPrefix(target, "/") {
		return false
	}

	components := strings.Split(target, "/")

	// Reject if ANY component is a traversal ("..") — this catches both
	// leading traversal ("../foo") and embedded traversal ("dist/../etc").
	// Must scan ALL components (not just non-last), because a ".."
	// appearing AFTER a safe component still escapes that safe directory.
	for _, comp := range components {
		if comp == ".." {
			return false
		}
	}

	// Check each component except the last one. A safe component must have
	// additional path segments following it to be whitelisted.
	// This ensures "rm -rf node_modules" (no trailing /) is NOT whitelisted
	// while "rm -rf node_modules/package" IS whitelisted.
	for i := 0; i < len(components)-1; i++ {
		comp := components[i]

		// Skip empty components (e.g., from leading ./ or multiple slashes)
		if comp == "" || comp == "." {
			continue
		}

		// Check if this component matches a known safe directory name
		if safeRmRfComponents[comp] {
			return true
		}
	}

	return false
}

// pathIsWorkspaceSafe checks whether a file path argument is safe for workspace operations.
// A path is considered safe if:
//   - It is a relative path (no leading /) — assumed to be within the workspace
//   - It is under /tmp/ (temporary files)
//   - It is /dev/null, /dev/stdout, or /dev/stderr
//   - It is a hyphen ("-") which is stdin/stdout in many commands
//   - It is under a user home directory: /Users/ (macOS) or /home/ (Linux),
//     EXCEPT sensitive credential/config subdirectories (.ssh, .gnupg, .aws, .kube,
//     .docker, .config/gh, .netrc) which are blocked
//
// Root's home (/root on Linux) is NOT safe — it is treated as a sensitive system dir.
//
// Path traversal is handled by path.Clean which resolves all ".." segments lexically.
// If path.Clean produces a result starting with "/tmp/", all parent directory references
// have been resolved — the path cannot escape /tmp. No additional ".." check is needed.
// This is a string-only heuristic — no filesystem access.
func pathIsWorkspaceSafe(pathStr string) bool {
	if pathStr == "" || pathStr == "-" {
		return true
	}

	// Clean the path to resolve . and .. segments.
	// path.Clean fully resolves all ".." for absolute paths: if the result starts
	// with "/tmp/" the path is guaranteed to be within /tmp.
	cleaned := path.Clean(pathStr)

	// Absolute paths must be under safe prefixes
	if strings.HasPrefix(cleaned, "/") {
		if cleaned == "/tmp" || strings.HasPrefix(cleaned, "/tmp/") {
			return true
		}
		if cleaned == "/dev/null" || cleaned == "/dev/stdout" || cleaned == "/dev/stderr" {
			return true
		}
		// User home directories (macOS /Users, Linux /home) are safe for
		// workspace operations — developers regularly copy/move files between
		// sibling repos and project directories under their home. Root's home
		// (/root) stays blocked as a sensitive system directory.
		// Note: callers (isDangerousPattern) may already lowercase the path,
		// so the prefix check is case-insensitive.
		cleanedLower := strings.ToLower(cleaned)
		if strings.HasPrefix(cleanedLower, "/users/") || strings.HasPrefix(cleanedLower, "/home/") {
			// Block sensitive credential/config directories within home
			for _, sensitive := range []string{"/.ssh/", "/.gnupg/", "/.aws/", "/.kube/", "/.docker/"} {
				if strings.Contains(cleanedLower, sensitive) {
					return false
				}
			}
			// Block sensitive credential files
			if strings.HasSuffix(cleanedLower, "/.netrc") || strings.Contains(cleanedLower, "/.config/gh/") {
				return false
			}
			return true
		}
		// All other absolute paths are unsafe
		return false
	}

	// Relative paths are safe (assumed within workspace)
	return true
}

// extractTargetPath extracts the primary target path from a filesystem-mutating command.
// For commands like "chmod 755 /etc/shadow", it extracts "/etc/shadow".
// For "mv src/ dest/", it extracts the destination "dest/".
// Returns the last non-flag argument. Returns empty if no non-flag arg exists.
func extractTargetPath(args string) string {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return ""
	}

	// Return the last argument that is not a flag.
	// For commands like "mv src dest", the destination is the last argument.
	// For commands like "chmod 755 file", the target is the last argument.
	for i := len(parts) - 1; i >= 0; i-- {
		if !strings.HasPrefix(parts[i], "-") {
			return parts[i]
		}
	}
	return ""
}

// hasSystemPathTarget checks if any path argument in the command targets a system directory.
// This handles commands like "mv /etc/passwd /tmp" where the source is system file,
// and "touch /etc/evil" where the target is system file.
// Also extracts paths from --flag=VALUE style arguments (e.g., --reference=/etc/shadow).
func hasSystemPathTarget(args string) bool {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return false
	}

	// Check each argument that looks like a path (not a standalone flag)
	for _, part := range parts {
		if strings.HasPrefix(part, "-") {
			// Handle --flag=VALUE style arguments where VALUE may be a path
			if eqIdx := strings.Index(part, "="); eqIdx >= 0 {
				val := part[eqIdx+1:]
				if val != "" && !pathIsWorkspaceSafe(val) {
					return true
				}
			}
			continue // Skip standalone flags
		}
		if !pathIsWorkspaceSafe(part) {
			return true
		}
	}

	return false
}
