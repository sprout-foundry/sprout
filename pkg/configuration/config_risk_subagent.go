package configuration

import "strings"

// RiskLevel represents the risk classification of an operation for the EA approval cascade.
type RiskLevel string

const (
	RiskLevelLow      RiskLevel = "low"      // Auto-approve (git status, read operations)
	RiskLevelMedium   RiskLevel = "medium"   // Reason and decide (git commit, git push)
	RiskLevelHigh     RiskLevel = "high"     // Prompt the user when interactive; reject when not
	RiskLevelCritical RiskLevel = "critical" // Never approvable: rm -rf root, fork bombs
)

// AutoApproveRules controls the EA's sliding risk cascade for operation approvals.
type AutoApproveRules struct {
	LowRiskOps    []string `json:"low_risk,omitempty"`        // Operations auto-approved by EA
	MediumRiskOps []string `json:"medium_risk,omitempty"`     // Operations the EA reasons about
	HighRiskNever []string `json:"high_risk_never,omitempty"` // Pattern names always gated (rm_recursive, force_flag, ...)
	// DefaultRisk is the level returned for operations that don't
	// match any of the above. Default (empty) is "medium" — the
	// classic EA behavior. Cautious profiles set this to "high"
	// so unrecognized commands route to a prompt. Permissive /
	// unrestricted set it to "low" so common operations auto-approve.
	DefaultRisk RiskLevel `json:"default_risk,omitempty"`
}

// DefaultAutoApproveRules returns the default risk cascade rules for the EA persona.
func DefaultAutoApproveRules() AutoApproveRules {
	return AutoApproveRulesForProfile(RiskProfileDefault)
}

// RiskProfile names a preset risk-cascade configuration. The active
// profile resolves to an AutoApproveRules via AutoApproveRulesForProfile.
// Persona-specified rules always take precedence over the profile.
type RiskProfile string

const (
	// RiskProfileReadonly — strictest. ONLY read operations (git
	// status / log / diff, read_file) are permitted. Every write,
	// edit, shell command, or destructive op is BLOCKED outright
	// (no prompt path) by promoting to the Critical tier. Use for
	// audits, code review, or sandboxed inspection where the agent
	// should never mutate anything.
	RiskProfileReadonly RiskProfile = "readonly"

	// RiskProfileCautious — most operations prompt the user. Suitable
	// for sensitive workspaces or unfamiliar agents. Low-risk reads
	// auto-approve; everything else gets routed to a prompt.
	RiskProfileCautious RiskProfile = "cautious"

	// RiskProfileDefault — sane defaults matching the historical EA
	// cascade. Reads auto-approve, common edits/commits auto-approve,
	// destructive operations (force flags, rm -rf, lossy git) prompt.
	RiskProfileDefault RiskProfile = "default"

	// RiskProfilePermissive — high trust. Almost everything passes
	// without prompting; only truly destructive patterns route to a
	// prompt. Use when the agent is well-trusted and the workspace
	// is recoverable (clean checkout, throwaway dir).
	RiskProfilePermissive RiskProfile = "permissive"

	// RiskProfileUnrestricted — no risk cascade gating at all. Only
	// the Critical tier (rm -rf root, fork bombs) blocks. Use with
	// extreme care; intended for sandboxed / disposable environments.
	RiskProfileUnrestricted RiskProfile = "unrestricted"
)

// IsValidRiskProfile reports whether s names a known profile.
// User-defined profiles (added via Config.RiskProfiles) are NOT
// considered "valid" by this predicate — it only covers the baked-in
// names. Callers that need to accept user-defined profiles should
// check Config.RiskProfiles directly via ResolveRiskProfileRules.
func IsValidRiskProfile(s string) bool {
	switch RiskProfile(s) {
	case RiskProfileReadonly, RiskProfileCautious, RiskProfileDefault, RiskProfilePermissive, RiskProfileUnrestricted:
		return true
	}
	return false
}

// ResolveRiskProfileRules returns the AutoApproveRules that should
// apply for the given profile name, honoring user overrides in
// Config.RiskProfiles before falling back to the baked-in defaults.
//
// Resolution order:
//  1. cfg.RiskProfiles[name] — user override (replaces builtins
//     entirely; the user is the source of truth for any profile name
//     they list, including the five named built-ins).
//  2. AutoApproveRulesForProfile(name) — baked-in defaults for the
//     known profile names. Unknown names fall through to the Default
//     profile here.
//
// cfg may be nil (no config loaded); in that case the baked-in rules
// are always returned.
func ResolveRiskProfileRules(cfg *Config, profile RiskProfile) AutoApproveRules {
	if cfg != nil && len(cfg.RiskProfiles) > 0 {
		if custom, ok := cfg.RiskProfiles[string(profile)]; ok {
			return custom
		}
	}
	return AutoApproveRulesForProfile(profile)
}

// AutoApproveRulesForProfile returns the rules baked into each named
// profile. Unknown profiles fall back to RiskProfileDefault.
func AutoApproveRulesForProfile(profile RiskProfile) AutoApproveRules {
	switch profile {
	case RiskProfileReadonly:
		// Strictest profile: only reads permitted. Everything else
		// (writes, edits, shell, git, etc.) promotes to Critical so
		// even an interactive prompt cannot approve it. The Critical
		// tier is the only one with no prompt path — that's what
		// makes "readonly" actually readonly instead of "prompt for
		// every write".
		return AutoApproveRules{
			LowRiskOps: []string{
				"git_status", "git_log", "git_diff", "read_file",
			},
			MediumRiskOps: []string{},
			HighRiskNever: []string{},
			DefaultRisk:   RiskLevelCritical,
		}

	case RiskProfileCautious:
		// Only reads auto-approve. Everything else (including normal
		// edits and commits) hits the High → prompt path via
		// DefaultRisk = High.
		return AutoApproveRules{
			LowRiskOps: []string{
				"git_status", "git_log", "git_diff", "read_file",
			},
			MediumRiskOps: []string{},
			HighRiskNever: []string{
				"force_flag", "rm_recursive", "git_reset_hard",
				"git_clean", "docker_prune", "git_push_force",
				"git_checkout", "git_switch", "git_restore", "git_branch_delete",
			},
			DefaultRisk: RiskLevelHigh,
		}

	case RiskProfilePermissive:
		// Everything common is auto-approved; only force/recursive
		// destructive patterns route to a prompt. DefaultRisk = Low
		// covers anything not explicitly listed.
		return AutoApproveRules{
			LowRiskOps: []string{
				"git_add", "git_status", "git_log", "git_diff", "read_file",
				"git_commit", "git_push", "git_pull", "git_fetch",
				"write_file", "edit_file", "shell_command",
				"rm_command", "docker", "subagent_spawn", "cross_directory",
				"git_checkout", "git_switch",
			},
			MediumRiskOps: []string{},
			HighRiskNever: []string{
				"force_flag", "rm_recursive", "git_reset_hard",
				"git_push_force", "git_clean", "git_restore", "git_branch_delete",
			},
			DefaultRisk: RiskLevelLow,
		}

	case RiskProfileUnrestricted:
		// No gating beyond the Critical tier (handled separately by
		// IsCriticalOperation, not via these lists). Even
		// force_flag / rm_recursive route to Low — the deliberate
		// "I know what I'm doing" mode for sandboxed runs.
		return AutoApproveRules{
			LowRiskOps:    []string{},
			MediumRiskOps: []string{},
			HighRiskNever: []string{},
			DefaultRisk:   RiskLevelLow,
		}

	case RiskProfileDefault:
		fallthrough
	default:
		// Backward-compatible default. DefaultRisk = Medium matches
		// the historical behavior so existing personas with EA-style
		// rules continue to behave exactly as before.
		return AutoApproveRules{
			LowRiskOps: []string{
				"git_add", "git_status", "git_log", "git_diff",
				"read_file", "build_test",
			},
			MediumRiskOps: []string{
				"git_commit", "git_push", "git_pull", "git_fetch",
				"write_file", "edit_file", "shell_command",
				"rm_command", "docker",
				"subagent_spawn", "cross_directory",
			},
			HighRiskNever: []string{
				"force_flag", "rm_recursive", "git_reset_hard",
				"git_clean", "docker_prune", "git_push_force",
				"git_checkout", "git_switch", "git_restore", "git_branch_delete",
			},
			DefaultRisk: RiskLevelMedium,
		}
	}
}

// IsCriticalOperation reports whether a command matches a pattern that
// is NEVER allowed regardless of profile, persona, or interactive
// approval. Reserved for operations that can permanently destroy the
// system or leave it in an unrecoverable state.
//
// This is the single source of truth for "critical" across every
// security gate: the static classifier (agent_tools.isCriticalSystemOperation
// delegates here) and the persona risk cascade (EvaluateOperationRisk)
// both consult it, so the two gates can't disagree on what's critical.
//
// Covers:
//   - rm -rf targeting root or the current dir (`/`, `/*`, `~`, `$HOME`, `.`, `*`)
//   - Classic fork-bomb pattern `:(){:|:&};:`
//   - Filesystem creation / raw disk overwrite (mkfs, dd to a block device)
//   - Mass process kills (killall -9 / -KILL)
//   - chmod 000 on a system root path
//   - Overwriting critical auth/system files (/etc/shadow, /etc/passwd, …)
//
// Tokenized matching matches invokesCommand semantics so a benign
// substring inside a path or argument can't trigger a false positive.
func IsCriticalOperation(command string) bool {
	cmdLower := strings.ToLower(strings.TrimSpace(command))

	// Fork bomb — the literal `:()` shell-function-named-colon is a
	// reliable signal. The token is unusual enough that false
	// positives are vanishingly rare.
	if strings.Contains(cmdLower, ":()") && strings.Contains(cmdLower, ":|:") {
		return true
	}

	fields := strings.Fields(cmdLower)

	// rm -rf <root-equivalent or cwd wildcard>. We need rm invoked as a
	// command, with a recursive flag, AND a destructive target.
	if invokesCommand(fields, "rm") {
		hasRecursive := false
		for _, f := range fields {
			if f == "-r" || f == "-R" || f == "--recursive" {
				hasRecursive = true
				break
			}
			if len(f) > 2 && f[0] == '-' && f[1] != '-' && strings.ContainsAny(f, "rR") {
				hasRecursive = true
				break
			}
		}
		if hasRecursive {
			for _, f := range fields {
				switch f {
				case "/", "/*", "~", "~/", "$home", "${home}", "${home}/", "$home/", ".", "*":
					return true
				}
			}
		}
	}

	// mkfs / mkfs.* — formatting a filesystem destroys everything on the target.
	if len(fields) > 0 && (fields[0] == "mkfs" || strings.HasPrefix(fields[0], "mkfs.")) {
		return true
	}

	// dd reading from / writing to a primary block device.
	if invokesCommand(fields, "dd") {
		for _, disk := range []string{"/dev/sda", "/dev/sdb", "/dev/nvme", "/dev/vda"} {
			if strings.Contains(cmdLower, "of="+disk) || strings.Contains(cmdLower, "if="+disk) {
				return true
			}
		}
	}

	// killall -9 / -KILL — mass process termination.
	if strings.HasPrefix(cmdLower, "killall -9") || strings.HasPrefix(cmdLower, "killall -kill") {
		return true
	}

	// chmod 000 on a system root path (locks everyone out).
	if strings.Contains(cmdLower, "chmod 000 /") {
		return true
	}

	// Overwriting critical auth/system files.
	for _, file := range []string{"/etc/shadow", "/etc/passwd", "/etc/sudoers", "/root/.ssh/authorized_keys"} {
		if strings.Contains(cmdLower, "> "+file) || strings.Contains(cmdLower, ">> "+file) || strings.Contains(cmdLower, "echo "+file) {
			return true
		}
	}

	return false
}

// SubagentType defines a specialized subagent persona with its own configuration
type SubagentType struct {
	ID                 string            `json:"id"`                             // Unique identifier (e.g., "coder", "tester", "debugger")
	Name               string            `json:"name"`                           // Human-readable name (e.g., "Coder", "Tester")
	Description        string            `json:"description"`                    // What this subagent specializes in
	Provider           string            `json:"provider"`                       // Provider for this subagent type (optional, falls back to SubagentProvider)
	Model              string            `json:"model"`                          // Model for this subagent type (optional, falls back to SubagentModel)
	SystemPrompt       string            `json:"system_prompt"`                  // Relative path to system prompt file (e.g., "subagent_prompts/coder.md")
	SystemPromptText   string            `json:"system_prompt_text,omitempty"`   // Optional inline system prompt text (replaces base prompt entirely)
	SystemPromptAppend string            `json:"system_prompt_append,omitempty"` // Optional inline text appended to the base or loaded system prompt (for composition)
	AllowedTools       []string          `json:"allowed_tools,omitempty"`        // Optional explicit tool allowlist for focused persona behavior
	Aliases            []string          `json:"aliases,omitempty"`              // Optional aliases (e.g., "web-scraper")
	Enabled            bool              `json:"enabled"`                        // Catalog-only: every shipped persona sets this true. Runtime "is this persona usable?" is determined by Config.DisabledPersonas (user) + LocalOnly (env). Kept for catalog hygiene + defense-in-depth in case a future variant ships with a deliberately-disabled entry.
	LocalOnly          bool              `json:"local_only,omitempty"`           // Only available in local mode (not cloud)
	Delegatable        bool              `json:"delegatable,omitempty"`          // Whether this persona can be used as a subagent (default: true for worker personas, false for orchestrator personas)
	AutoApproveRules   *AutoApproveRules `json:"auto_approve_rules,omitempty"`   // Risk cascade rules for the runtime auto-approve check
	// Capabilities is an explicit list of agency grants this persona holds
	// (e.g. "git_write"). Replaces sniffing AutoApproveRules to infer what a
	// persona is allowed to do. Use HasCapability to query.
	Capabilities []string `json:"capabilities,omitempty"`
	// CanSpawnNonDelegatable lists otherwise-undelegatable persona IDs that
	// this persona may spawn. Replaces the hardcoded EA-spawn-authority
	// special-case. The coordinator carries ["orchestrator"] to enable the
	// canonical coordinator→orchestrator→specialist chain.
	CanSpawnNonDelegatable []string `json:"can_spawn_non_delegatable,omitempty"`
}

// HasCapability reports whether the persona declares the given capability
// name. Comparison is case-insensitive and whitespace-tolerant.
func (st *SubagentType) HasCapability(name string) bool {
	if st == nil {
		return false
	}
	target := strings.ToLower(strings.TrimSpace(name))
	if target == "" {
		return false
	}
	for _, c := range st.Capabilities {
		if strings.ToLower(strings.TrimSpace(c)) == target {
			return true
		}
	}
	return false
}

// GetAutoApproveRules returns the auto-approve rules for this persona,
// falling back to defaults if none are configured.
// Callers MUST NOT modify the returned struct's slice fields,
// as they may share backing arrays with the original config.
func (st *SubagentType) GetAutoApproveRules() AutoApproveRules {
	if st.AutoApproveRules != nil {
		return *st.AutoApproveRules
	}
	return DefaultAutoApproveRules()
}

// EvaluateOperationRisk determines the risk level of a shell operation
// based on the persona's auto-approve rules.
// Returns RiskLevelCritical for absolute-block patterns, otherwise
// RiskLevelLow, RiskLevelMedium, or RiskLevelHigh per the rules.
func (st *SubagentType) EvaluateOperationRisk(command string) RiskLevel {
	// Critical patterns are ALWAYS blocked, regardless of persona
	// rules or profile. Checked first so no rule lookup can shadow
	// them.
	if IsCriticalOperation(command) {
		return RiskLevelCritical
	}

	rules := st.GetAutoApproveRules()

	cmdLower := strings.ToLower(command)

	// HighRiskNever patterns are gated. "force_flag" is one such
	// pattern that lives in the list for all gated profiles; the
	// Unrestricted profile has it removed so -f / --force passes
	// through. (Prior to SP-058 there was a hardcoded
	// containsForceFlag short-circuit before the loop; that's been
	// folded into the data-driven check so profiles can fully
	// control gating.)
	for _, pattern := range rules.HighRiskNever {
		if matchesRiskPattern(cmdLower, pattern) {
			return RiskLevelHigh
		}
	}

	// Determine the operation category for classification
	opCategory := categorizeCommand(cmdLower)

	// Check if the operation is explicitly in the low-risk list
	for _, pattern := range rules.LowRiskOps {
		if opCategory == pattern {
			return RiskLevelLow
		}
	}

	// Check if the operation is in the medium-risk list
	for _, pattern := range rules.MediumRiskOps {
		if opCategory == pattern {
			return RiskLevelMedium
		}
	}

	// Fall back to the profile's declared DefaultRisk for unknown
	// operations. Empty / unspecified default behaves as Medium for
	// backward compatibility with personas configured before SP-058.
	// DefaultRisk = Critical is legitimate for the readonly profile
	// (blocks all non-read ops outright).
	switch rules.DefaultRisk {
	case RiskLevelLow:
		return RiskLevelLow
	case RiskLevelHigh:
		return RiskLevelHigh
	case RiskLevelCritical:
		return RiskLevelCritical
	default:
		return RiskLevelMedium
	}
}

// containsForceFlag checks if a command string contains -f or --force flags.
// --force-with-lease is explicitly excluded as it is a safer alternative
// that verifies remote state before overwriting.
// For -f standalone, only treats it as force for commands that commonly use -f as a force flag:
// git, rm, mv, cp, and docker. This avoids false positives on commands like grep -f or tail -f.
func containsForceFlag(cmdLower string) bool {
	// Check for --force as an exact token, but NOT --force-with-lease
	for _, segment := range strings.Fields(cmdLower) {
		if segment == "--force" {
			return true
		}
	}

	// Get the first word (command name) to check if -f should be treated as force
	fields := strings.Fields(cmdLower)
	if len(fields) == 0 {
		return false
	}
	firstCmd := fields[0]

	// Check for -f as a standalone flag (not part of a word)
	// Only treat -f as force for commands that commonly use it as a force flag
	for idx, segment := range fields {
		if segment == "-f" {
			// Only -f for force-capable commands
			switch firstCmd {
			case "git":
				// For git, -f must appear AFTER the subcommand (not at position 1).
				// Position 1 is between git and the subcommand — that's a malformed
				// global flag position, not a force flag.
				// Exception: if -f is the last token (e.g. "git -f" with no subcommand),
				// treat it as force — bare git -f is unusual but should be flagged.
				if idx > 1 {
					return true
				}
				if idx == 1 && idx == len(fields)-1 {
					return true // "git -f" with nothing after — bare force flag
				}
				// idx == 1 and there are more tokens → malformed global flag, skip
			case "rm", "mv", "cp", "docker":
				return true
			}
		}
		// Handle combined short flags like -af, -rf (these are dangerous)
		// Only treat combined flags with 'f' as force for force-capable commands
		if len(segment) > 2 && segment[0] == '-' && segment[1] != '-' && strings.Contains(segment, "f") {
			switch firstCmd {
			case "git":
				// Same rule: for git, combined flags with 'f' at position 1 are
				// skipped if there's a subcommand after them.
				if idx == 1 && idx < len(fields)-1 {
					continue
				}
				// But not things like "diff" or "conf" that happen to contain f
				// Only flag combinations that include f
				isAllFlags := true
				for _, ch := range segment[1:] {
					if ch >= '0' && ch <= '9' {
						isAllFlags = false
						break
					}
				}
				if isAllFlags {
					return true
				}
			case "rm", "mv", "cp", "docker":
				// But not things like "diff" or "conf" that happen to contain f
				// Only flag combinations that include f
				isAllFlags := true
				for _, ch := range segment[1:] {
					if ch >= '0' && ch <= '9' {
						isAllFlags = false
						break
					}
				}
				if isAllFlags {
					return true
				}
			}
		}
	}
	return false
}

// categorizeCommand maps a command string to a risk-category identifier.
func categorizeCommand(cmdLower string) string {
	if strings.HasPrefix(cmdLower, "git ") {
		return categorizeGitCommand(cmdLower)
	}
	if strings.HasPrefix(cmdLower, "rm ") {
		return "rm_command"
	}
	if strings.HasPrefix(cmdLower, "docker ") {
		return "docker"
	}
	// Read-only file operations. These commands cannot mutate state:
	// they only read files / metadata / environment / hardware info.
	// Commands commonly invoked WITHOUT arguments (pwd, date, whoami,
	// ...) are matched via isReadOnlyCmd which accepts both the bare
	// command and "<cmd> <args>" forms. The remaining ones are matched
	// by prefix only (they always take an argument in practice).
	if isReadOnlyCmd(cmdLower,
		// Bare-or-arged: frequently invoked with no arguments.
		"pwd", "date", "whoami", "id", "groups", "tty", "arch",
		"nproc", "uptime", "free", "true", "false", "env", "printenv",
		// Always-arged in practice, but matched uniformly for simplicity.
		"cat", "head", "ls", "find", "which", "file",
		"grep", "rg", "wc", "tree", "du", "df", "stat", "uname",
		"basename", "dirname", "realpath", "test",
		"type", "command", "hash", "locate", "lscpu", "lsblk", "lsmod",
		"lspci", "lsusb", "getconf",
	) {
		return "read_file"
	}
	// Write operations
	if strings.HasPrefix(cmdLower, "write_file") || strings.HasPrefix(cmdLower, "edit_file") {
		return "write_file"
	}
	// Build / test / lint tool invocations. These execute a project's
	// build system, test runner, or linter — read-mostly operations that
	// are the single most common source of shell_command prompts during
	// development. Only the safe subcommands are recognized; state-mutating
	// forms (install, apply, delete, publish, push, prune, system prune,
	// exec, run --rm that mutates) fall through to shell_command → Medium.
	if isBuildTestCmd(cmdLower) {
		return "build_test"
	}
	return "shell_command"
}

// isReadOnlyCmd reports whether cmdLower is an invocation of one of the
// given read-only command names — either the bare command (e.g. "pwd")
// or the command followed by arguments ("pwd\n", "pwd ", "pwd\n…"). The
// trailing-space prefix form matches any argument-bearing invocation
// while the exact-equality form covers the common no-argument case.
func isReadOnlyCmd(cmdLower string, names ...string) bool {
	for _, n := range names {
		if cmdLower == n || strings.HasPrefix(cmdLower, n+" ") {
			return true
		}
	}
	return false
}

// buildTestSafeSubcommands lists the subcommands of npm/yarn/pnpm that are
// treated as build/test/lint (auto-approved) rather than state-mutating.
// Anything not listed here (install, publish, uninstall, add, remove,
// create, etc.) falls through to shell_command → Medium.
var buildTestSafeSubcommands = map[string]bool{
	"test": true, "tests": true, "e2e": true,
	"build": true, "build-all": true,
	"lint": true, "format": true, "fmt": true,
	"check": true, "vet": true, "typecheck": true, "type-check": true,
	"storybook": true, "coverage": true,
	"run": true, "exec": true, // `npm run X` / `npm exec X` — user-defined scripts
}

// isBuildTestCmd reports whether cmdLower is a recognized build / test / lint
// tool invocation. It matches:
//
//   - Bare tools that are inherently read/exec-only on local state:
//     go, make, cargo, mvn, gradle, dotnet.
//   - Script runners (node, python3/python, ruby, perl) — running a script
//     is the development primitive; the classifier cannot inspect what the
//     script does, but treating it as Medium on every invocation produces
//     a prompt storm with no real safety benefit (Critical ops inside the
//     script like `rm -rf /` are caught by the static classifier when the
//     shell handler expands them, not here).
//   - npm/yarn/pnpm with a safe subcommand (test, build, lint, run, …).
//     State-mutating subcommands (install, publish, add, remove) are NOT
//     matched and fall through to shell_command → Medium.
//   - Safe kubectl (get, describe, logs, top, explain) and terraform
//     (plan, validate, fmt) — read-only inspection subcommands.
//
// The common false-positive vectors are excluded deliberately:
//   - `go install` → "go" matches but install mutates GOPATH/pkg cache;
//     however go's safe surface (build, test, vet, run) is large enough
//     that we accept go as a unit and rely on the shell handler's
//     pipeline-expansion check for genuinely destructive `go run` scripts.
//   - `docker run/exec` mutates containers but not the host; treated as
//     build_test (docker compose up/down is the common dev primitive).
func isBuildTestCmd(cmdLower string) bool {
	fields := strings.Fields(cmdLower)
	if len(fields) == 0 {
		return false
	}
	base := fields[0]
	switch base {
	// Bare tools: every invocation is a build/test/lint primitive.
	case "make", "cargo", "mvn", "gradle", "dotnet", "msbuild":
		return true
	case "go":
		// `go install`/`go get` mutate module cache — route to shell_command.
		// Everything else (build, test, vet, run, generate, mod tidy, etc.)
		// is a development primitive.
		if len(fields) >= 2 && (fields[1] == "install" || fields[1] == "get") {
			return false
		}
		return true
	// Script runners: executing a script file is the dev primitive.
	case "node", "python", "python3", "ruby", "perl", "deno", "bun":
		return true
	// npm/yarn/pnpm: only safe subcommands.
	case "npm", "yarn", "pnpm", "npx":
		// Bare `npm` or `yarn` with no subcommand is not a recognized op.
		if len(fields) < 2 {
			return false
		}
		return buildTestSafeSubcommands[fields[1]]
	// kubectl: only read-only inspection subcommands.
	case "kubectl", "oc", "k":
		if len(fields) < 2 {
			return false
		}
		switch fields[1] {
		case "get", "describe", "logs", "log", "top", "explain",
			"version", "config", "cluster-info", "api-resources", "api-versions":
			return true
		}
		return false
	// terraform: plan/validate/fmt/version are read-only.
	case "terraform", "tofu", "tflint":
		if len(fields) < 2 {
			return false
		}
		switch fields[1] {
		case "plan", "validate", "fmt", "version", "show", "graph", "output":
			return true
		}
		return false
	// docker: compose up/down/ps/logs (dev workflow) and ps/images/logs (read).
	// docker run/exec/prune/system fall through to shell_command → Medium.
	case "docker", "podman":
		if len(fields) < 2 {
			return false
		}
		switch fields[1] {
		case "compose", "ps", "images", "logs", "log", "version", "info",
			"stats", "top", "port", "inspect":
			return true
		}
		return false
	}
	return false
}

// categorizeGitCommand maps git subcommands to risk-category identifiers.
func categorizeGitCommand(cmdLower string) string {
	subcmd := firstFieldAfter(cmdLower, "git")
	switch subcmd {
	case "status":
		return "git_status"
	case "log":
		return "git_log"
	case "diff":
		return "git_diff"
	case "add":
		return "git_add"
	case "commit":
		return "git_commit"
	case "push":
		return "git_push"
	case "pull":
		return "git_pull"
	case "fetch":
		return "git_fetch"
	case "reset":
		return "git_reset_hard"
	case "clean":
		return "git_clean"
	case "branch":
		if strings.Contains(cmdLower, "-d") || strings.Contains(cmdLower, "--delete") {
			return "git_branch_delete" // Branch deletion is high risk
		}
		return "git_status" // Branch listing is low risk
	case "checkout":
		return "git_checkout" // Can discard changes
	case "switch":
		return "git_switch" // Can discard changes
	case "restore":
		return "git_restore" // Can discard changes
	case "stash":
		return "git_status" // Stash is relatively safe
	case "tag":
		return "git_add" // Tags are relatively safe
	case "merge", "rebase":
		return "git_commit" // Medium risk like commit
	case "cherry-pick", "cherry_pick", "am", "apply":
		return "git_commit" // Medium — applies patches, version-controlled
	case "rm", "mv":
		return "git_commit" // Medium — removes/moves tracked files, version-controlled
	default:
		return "shell_command" // Default to medium
	}
}

// matchesRiskPattern checks if a command matches a risk pattern identifier.
func matchesRiskPattern(cmdLower string, pattern string) bool {
	// Map pattern names to actual command matching. All patterns here
	// operate on tokenized fields rather than bare substring matches —
	// a path component like ".../platform &&" used to false-match
	// "rm " (the last two chars of "platform" + the space before "&&"),
	// and "-run" used to false-match "-r" — so a benign command like
	// `cd ~/.../platform && go test -run X` got classified as
	// high-risk rm_recursive. See the rm_recursive case below.
	fields := strings.Fields(cmdLower)
	hasToken := func(target string) bool {
		for _, f := range fields {
			if f == target {
				return true
			}
		}
		return false
	}
	switch pattern {
	case "force_flag":
		return containsForceFlag(cmdLower)
	case "rm_recursive":
		// Must actually invoke `rm` as a command — either at the very
		// start, after `sudo`, or after a `;` / `&&` / `||` / `|`
		// operator. A path component that happens to end in "rm"
		// (e.g. "platform") is NOT an invocation.
		if !invokesCommand(fields, "rm") {
			return false
		}
		// And a real recursive-mode flag must appear as its own token
		// or combined short flag (-r, -R, -rf, -fr, --recursive).
		for _, f := range fields {
			if f == "-r" || f == "-R" || f == "--recursive" {
				return true
			}
			// Combined short flag: -rf, -fr, -Rf, -fR (any order, any
			// length, must start with '-' and not be a long flag).
			if len(f) > 2 && f[0] == '-' && f[1] != '-' {
				hasR := strings.ContainsAny(f, "rR")
				hasF := strings.Contains(f, "f")
				if hasR && hasF {
					return true
				}
			}
		}
		return false
	case "git_reset_hard":
		return invokesGitSubcommand(fields, "reset") && hasToken("--hard")
	case "git_clean":
		return invokesGitSubcommand(fields, "clean")
	case "git_push_force":
		if !invokesGitSubcommand(fields, "push") {
			return false
		}
		// --force-with-lease is safer, don't match it
		for _, segment := range fields {
			if segment == "--force" || segment == "-f" {
				return true
			}
		}
		return false
	case "docker_prune":
		if !invokesCommand(fields, "docker") {
			return false
		}
		return hasToken("prune")
	case "git_checkout":
		return invokesGitSubcommand(fields, "checkout")
	case "git_switch":
		return invokesGitSubcommand(fields, "switch")
	case "git_restore":
		return invokesGitSubcommand(fields, "restore")
	case "git_branch_delete":
		if !invokesGitSubcommand(fields, "branch") {
			return false
		}
		return hasToken("-d") || hasToken("-D") || hasToken("--delete")
	default:
		return false
	}
}

// invokesCommand reports whether the tokenized command line actually
// invokes `name` as a command — i.e. as the first token, after `sudo`,
// or as the first token after a shell pipeline / chain operator
// (`;`, `&&`, `||`, `|`). This avoids substring matches inside paths,
// flag values, or arguments (the bug that made
// `cd .../platform && go test ... -run X` match `rm_recursive`).
func invokesCommand(fields []string, name string) bool {
	if len(fields) == 0 {
		return false
	}
	for i, f := range fields {
		if f != name {
			continue
		}
		if i == 0 {
			return true
		}
		// After a chain/pipe operator — first command in the next
		// segment. Walk backwards skipping `sudo`-like prefixes.
		prev := fields[i-1]
		switch prev {
		case ";", "&&", "||", "|":
			return true
		case "sudo":
			return true
		}
	}
	return false
}

// invokesGitSubcommand reports whether the tokenized command line
// invokes `git <subcmd>` — `git` must be invoked as a command (per
// invokesCommand) AND immediately followed by the subcommand token.
// Catches `cd /repo && git checkout main` but not `... grep 'git
// checkout' file` or path/argument substrings that happen to spell
// the same letters.
func invokesGitSubcommand(fields []string, subcmd string) bool {
	for i, f := range fields {
		if f != "git" {
			continue
		}
		if !invokesCommand(fields[i:i+1], "git") && !(i > 0 && isChainOperator(fields[i-1])) && i != 0 {
			continue
		}
		if i+1 < len(fields) && fields[i+1] == subcmd {
			return true
		}
	}
	return false
}

// isChainOperator reports whether a token is a shell pipeline / chain
// operator that ends one command and starts another.
func isChainOperator(tok string) bool {
	switch tok {
	case ";", "&&", "||", "|":
		return true
	}
	return false
}

// firstFieldAfter returns the first whitespace-delimited field after the given prefix.
func firstFieldAfter(s, prefix string) string {
	after := strings.TrimPrefix(s, prefix)
	after = strings.TrimSpace(after)
	fields := strings.Fields(after)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
