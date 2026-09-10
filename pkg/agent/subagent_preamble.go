package agent

import "strings"

// subagentPreamble is appended to every subagent's system prompt at spawn
// time (createSubagent → appendSubagentPreamble). It carries the constraints
// that apply to EVERY subagent regardless of persona, so persona prompt
// files don't have to hand-copy them (and can't silently drift out of sync).
//
// Keep this tight: it rides on every spawn, in every persona, at every
// depth. Anything persona-specific belongs in the persona's own file.
const subagentPreamble = `## Subagent Operating Rules (framework)

- **Git**: read-only git (` + "`git status/diff/log/show`" + `) is fine via shell_command. Do NOT commit or push — the primary agent handles git writes. Never ` + "`git add .`/`-A`" + `, never force flags, never ` + "`git checkout`/`reset`/`restore`" + ` via shell_command.
- **No subagents**: complete the task yourself; you cannot spawn further subagents.
- **No user interaction**: stdin is disabled. If the task is ambiguous, state your assumption and proceed; if you hit a security or permission error, do NOT retry or work around it — report the error to the primary agent.
- **Scope**: complete the delegated task only. Report what you changed and how you verified it.
`

// appendSubagentPreamble appends the shared subagent operating rules to a
// persona system prompt. Idempotent: if the marker is already present
// (e.g. a user-configured prompt that inlined it), the prompt is returned
// unchanged.
func appendSubagentPreamble(prompt string) string {
	if strings.Contains(prompt, "## Subagent Operating Rules (framework)") {
		return prompt
	}
	return strings.TrimRight(prompt, "\n") + "\n" + subagentPreamble
}
