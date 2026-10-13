package subagents

import "strings"

// subagentPreamble is appended to every subagent's system prompt at spawn
// time (createSubagent → appendSubagentPreamble). It carries the constraints
// that apply to EVERY subagent regardless of persona, so persona prompt
// files don't have to hand-copy them (and can't silently drift out of sync).
//
// Keep this tight: it rides on every spawn, in every persona, at every
// depth. Anything persona-specific belongs in the persona's own file.
const subagentPreamble = `## Subagent Operating Rules (framework)

- **No subagents**: complete the task yourself; you cannot spawn further subagents.
- **No user interaction**: stdin is disabled. If the task is ambiguous, state your assumption and proceed; if you hit a security or permission error, do NOT retry or work around it — report the error to the primary agent.
- **Scope**: complete the delegated task only. Report what you changed and how you verified it.
`

// gitRulesNoWrite is the git bullet for subagents WITHOUT the git_write
// capability: read-only git, no commits, no destructive flags.
const gitRulesNoWrite = `- **Git**: read-only git (` + "`git status/diff/log/show`" + `) is fine via shell_command. Do NOT commit or push — the primary agent handles git writes. Never ` + "`git add .`/`-A`" + `, never force flags, never ` + "`git checkout`/`reset`/`restore`" + ` via shell_command.
`

// gitRulesWrite replaces the read-only bullet for personas WITH the
// git_write capability (the orchestrator family). Their runs are long and
// time-budgeted, so the rule is to commit incrementally: a run cut off at
// the deadline must leave committed work behind, not an uncommitted tree.
const gitRulesWrite = `- **Git**: you have git-write access and a time budget. **Commit incrementally**: as soon as each coherent slice builds and tests pass, commit it (stage individual paths, meaningful message) before moving to the next slice — do NOT hold everything for one final commit. A run cut off at the deadline loses only the slice in progress. Never ` + "`git add .`/`-A`" + `, never force flags, never ` + "`git checkout`/`reset`/`restore`" + ` via shell_command.
`

// AppendSubagentPreamble appends the shared subagent operating rules (the
// read-only git variant) to a persona system prompt. Idempotent: if the
// marker is already present (e.g. a user-configured prompt that inlined
// it), the prompt is returned unchanged.
func AppendSubagentPreamble(prompt string) string {
	return AppendSubagentPreambleForPersona(prompt, false)
}

// AppendSubagentPreambleForPersona is AppendSubagentPreamble with the git
// bullet chosen by capability: hasGitWrite personas get the incremental-
// commit rule, everyone else the read-only rule.
func AppendSubagentPreambleForPersona(prompt string, hasGitWrite bool) string {
	if strings.Contains(prompt, "## Subagent Operating Rules (framework)") {
		return prompt
	}
	gitRules := gitRulesNoWrite
	if hasGitWrite {
		gitRules = gitRulesWrite
	}
	return strings.TrimRight(prompt, "\n") + "\n" + subagentPreamble + gitRules
}
