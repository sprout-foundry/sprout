package tools

import (
	"path"
	"strings"
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

	// Pipe-to-shell patterns are checked in classifyChainedCommand on the
	// full command string (with quoted sections stripped). No need to check
	// again here on individual command parts — any | remaining in a part
	// after chain splitting is inside quotes (e.g., grep regex alternation).

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

	// Check for workspace commands targeting system directories
	prefixes := []string{"chmod ", "chown ", "chgrp ", "cp ", "mv ", "mkdir -p", "touch ", "tee ", "ln ", "install ", "strip "}
	for _, prefix := range prefixes {
		if strings.HasPrefix(cmdLower, prefix) {
			argsAfterCmd := cmd[len(prefix):]
			if hasSystemPathTarget(argsAfterCmd) {
				return true
			}
		}
	}

	return false
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

// safeBuildArtifactDirs is a set of directory names that are always safe to
// rm -rf regardless of nesting depth. These are generated build artifacts,
// caches, or tool output directories — deleting them only requires a rebuild.
//
// This is used by isSafeRmRfPrefix as a fallback when the exact-prefix map
// (safeRmRfPrefixes) doesn't match nested paths. A nested path like
// "internal/api/webui/dist/sprout-webui" contains the segment "dist", which
// is in this set, so the deletion is classified as safe.
//
// Source directories (src, lib, internal, app, pkg, etc.) are intentionally
// NOT in this set — those contain hand-written code, not generated artifacts.
//
// Safety: path traversal (../) and absolute non-tmp paths are rejected BEFORE
// this check runs in isSafeRmRfPrefix.
var safeBuildArtifactDirs = map[string]bool{
	"dist":          true,
	"build":         true,
	"node_modules":  true,
	"target":        true,
	"__pycache__":   true,
	".cache":        true,
	".gradle":       true,
	".next":         true,
	".turbo":        true,
	".output":       true,
	".svelte-kit":   true,
	".parcel-cache": true,
	".nuxt":         true,
	".astro":        true,
	".npm":          true,
	".yarn":         true,
	".pnpm":         true,
	".vercel":       true,
	".netlify":      true,
	".terraform":    true,
	".docker":       true,
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

	// Fallback: check if any path segment in the target matches a known
	// build-artifact directory. This handles nested paths like
	// "rm -rf internal/api/webui/dist/sprout-webui" where "dist" is a
	// generated build artifact directory embedded within a project path.
	//
	// The path must contain at least one "/" separator to qualify. This
	// ensures bare names like "rm -rf build" (no trailing "/" or " ") stay
	// DANGEROUS — consistent with the existing map design which intentionally
	// leaves the no-trailing-separator variant unmatched.
	targets := normalized[len("rm -rf "):]
	for _, target := range strings.Fields(targets) {
		// Skip flags (e.g., --no-preserve-root, though such commands
		// should already be caught by isCriticalSystemOperation)
		if strings.HasPrefix(target, "-") {
			continue
		}
		// Require at least one "/" — bare names without a path separator
		// stay DANGEROUS (e.g., "rm -rf build" vs "rm -rf build/").
		if !strings.Contains(target, "/") {
			continue
		}
		// CRITICAL: reject path traversal — never auto-approve "../".
		if strings.Contains(target, "..") {
			continue
		}
		// CRITICAL: reject absolute paths except /tmp/ (already safe).
		if strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "/tmp/") {
			continue
		}
		// Check if any path segment is a known build artifact directory.
		for _, segment := range strings.Split(target, "/") {
			if safeBuildArtifactDirs[segment] {
				return true
			}
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
