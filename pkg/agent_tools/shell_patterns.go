package tools

import "strings"

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
