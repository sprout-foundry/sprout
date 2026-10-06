package zsh

import (
	"os"
	"regexp"
	"strings"
)

// Subcommands of tools whose first argument must be one of a fixed set.
var knownSubcommands = map[string][]string{
	"go":    {"build", "clean", "doc", "env", "fix", "fmt", "generate", "get", "install", "list", "mod", "work", "run", "test", "tool", "version", "vet"},
	"cargo": {"add", "bench", "build", "b", "check", "c", "clean", "clippy", "doc", "d", "fetch", "fix", "fmt", "init", "install", "metadata", "new", "publish", "remove", "run", "r", "search", "test", "t", "tree", "update", "version"},
	"npm":   {"ci", "install", "i", "init", "ls", "outdated", "pack", "publish", "rebuild", "run", "run-script", "start", "stop", "test", "t", "uninstall", "update", "version", "view", "audit", "exec", "x", "list", "why", "prune", "dedupe"},
	"yarn":  {"add", "audit", "build", "dev", "dlx", "exec", "info", "init", "install", "lint", "remove", "run", "start", "test", "upgrade", "why", "workspace", "workspaces"},
	"pnpm":  {"add", "audit", "build", "dev", "dlx", "exec", "install", "i", "list", "ls", "outdated", "remove", "rm", "run", "start", "test", "t", "update", "up", "why"},
}

var (
	shellSyntax   = regexp.MustCompile("^-|[|<>;&*?$=~`\"'/\\\\]")
	fileWithExt   = regexp.MustCompile(`\.[A-Za-z0-9]{1,8}$`)
	makefileNames = []string{"GNUmakefile", "makefile", "Makefile"}
)

// LooksLikeCommandLine reports whether input reads as a shell command rather
// than a sentence that happens to start with a command's name ("make sure
// the tests pass", "find the bug", "go on"). Typed without a "!" prefix,
// input only runs as a command when this holds: a bare word, a question, a
// tool without a real subcommand, or arguments with nothing shell-like
// about them all go to the assistant.
func LooksLikeCommandLine(input string) bool {
	input = strings.TrimSpace(input)
	words := strings.Fields(input)
	if len(words) < 2 || strings.HasSuffix(input, "?") {
		return false
	}
	name, args := strings.ToLower(words[0]), words[1:]
	switch name {
	case "git", "docker", "kubectl", "gh":
		return true // IsCommand validates their subcommand against the tool itself
	}
	if subs, ok := knownSubcommands[name]; ok {
		for _, s := range subs {
			if args[0] == s {
				return true
			}
		}
		return false
	}
	if name == "make" && makeTargetExists(args[0]) {
		return true
	}
	for _, a := range args {
		if shellSyntax.MatchString(a) || fileWithExt.MatchString(a) {
			return true
		}
		if _, err := os.Stat(a); err == nil {
			return true
		}
	}
	return false
}

// makeTargetExists reports whether a Makefile in the working directory
// defines target.
func makeTargetExists(target string) bool {
	rule := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `\s*:`)
	for _, name := range makefileNames {
		if data, err := os.ReadFile(name); err == nil && rule.Match(data) {
			return true
		}
	}
	return false
}
