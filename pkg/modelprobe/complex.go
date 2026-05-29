package modelprobe

import (
	"context"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// The complex scenario is a root-cause analysis: "users who delete their account
// still receive notification emails." The model must explore, read a couple of
// files, find the REAL cause, and emit a discrete TODO list — we stop once it
// does (we don't have it implement).
//
// Root cause: account deletion (internal/users/service.go) removes the user row
// but never removes the user's notification subscription, so the daily mailer
// still treats them as an active subscriber. The fix is to call the existing-
// but-unused notify.Subscriptions.RemoveForUser from the delete flow.
//
// The project is seeded with distractors and a trap to throw off weak models
// that pattern-match the symptom ("emails") instead of analyzing the cause:
//   - internal/notify/mailer.go, templates.go — about sending email, not the bug
//   - docs/EMAIL.md — red-herring documentation
//   - internal/legacy/old_delete.go — DEPRECATED, unused; tempting but wrong
func complexFiles() map[string]string {
	return map[string]string{
		"README.md":           "# acme-users\n\nUsers service. Code under `internal/`: `users` (accounts), `notify` (emails), `legacy` (old code).\n",
		"go.mod":              "module github.com/acme/users\n\ngo 1.22\n",
		"config/service.json": "{\n  \"name\": \"acme-users\",\n  \"version\": \"3.1.0\"\n}\n",
		"docs/EMAIL.md": "# Email\n\nThe notify package sends a daily digest to every active subscriber. " +
			"Templates live in notify/templates.go and delivery in notify/mailer.go.\n",
		"internal/users/store.go": "package users\n\n" +
			"type User struct {\n\tID   string\n\tName string\n}\n\n" +
			"type Store struct{ db *DB }\n\n" +
			"// DeleteUser removes the user row.\n" +
			"func (s *Store) DeleteUser(id string) error { /* DELETE FROM users WHERE id=? */ return nil }\n" +
			"func (s *Store) GetUser(id string) (User, error) { return User{}, nil }\n",
		"internal/users/service.go": "package users\n\n" +
			"import \"github.com/acme/users/internal/notify\"\n\n" +
			"type Service struct {\n\tstore *Store\n\tsubs  *notify.Subscriptions\n}\n\n" +
			"// DeleteAccount permanently removes a user account.\n" +
			"func (s *Service) DeleteAccount(id string) error {\n" +
			"\t// Removes the user row. NOTE: nothing else here cleans up related data.\n" +
			"\treturn s.store.DeleteUser(id)\n}\n",
		"internal/notify/subscriptions.go": "package notify\n\n" +
			"type Subscriptions struct{ db *DB }\n\n" +
			"// Add subscribes a user to notifications.\n" +
			"func (s *Subscriptions) Add(userID string) error { return nil }\n\n" +
			"// Active returns the user IDs the mailer should email.\n" +
			"func (s *Subscriptions) Active() ([]string, error) { return nil, nil }\n\n" +
			"// RemoveForUser deletes a user's subscription so they stop being emailed.\n" +
			"// NOTE: this is not called from anywhere yet.\n" +
			"func (s *Subscriptions) RemoveForUser(userID string) error { return nil }\n",
		"internal/notify/mailer.go": "package notify\n\n" +
			"// Mailer sends notification emails to all active subscribers.\n" +
			"type Mailer struct{ subs *Subscriptions }\n\n" +
			"func (m *Mailer) SendDaily() error {\n" +
			"\tids, _ := m.subs.Active() // emails everyone Subscriptions says is active\n" +
			"\tfor range ids { /* render template + send */ }\n" +
			"\treturn nil\n}\n",
		"internal/notify/templates.go": "package notify\n\n" +
			"const dailyDigestTemplate = \"Hello {{.Name}}, here is your daily digest...\"\n",
		"internal/legacy/old_delete.go": "package legacy\n\n" +
			"// DEPRECATED: account deletion now lives in internal/users/service.go.\n" +
			"// This file is unused and retained only for reference. Do not modify.\n" +
			"func DeleteUserAndData(id string) error { /* old monolithic delete */ return nil }\n",
	}
}

func complexSystem() string {
	return "You are a senior engineer triaging a bug in an unfamiliar Go codebase. Use the tools to " +
		"explore and read code, determine the ROOT CAUSE, then submit a concrete TODO list to fix it. " +
		"Respond only via tool calls."
}

func complexTask() string {
	return "Bug report: users who DELETE their account keep receiving notification emails for days afterward.\n\n" +
		"Investigate the code (the project root is the current directory) to find the ROOT CAUSE — not just " +
		"the symptom. Then call submit_todos with:\n" +
		"  - summary: the root cause in one or two sentences.\n" +
		"  - todos: a numbered list of the discrete code changes needed to fix it properly.\n\n" +
		"Do NOT write the code — just produce the plan. Respond only via tool calls."
}

func complexTools() []api.Tool {
	return []api.Tool{
		fn("list_dir", "List the immediate entries of a directory (use \".\" for the project root). Directories end with \"/\".", props("path", "directory path, or \".\" for root"), "path"),
		fn("read_file", "Read a file's contents.", props("path", "path to read"), "path"),
		fn("submit_todos", "Submit the root-cause summary and the ordered TODO list to fix the bug. Call this once you've analyzed the cause.",
			props2("summary", "the root cause in one or two sentences", "todos", "numbered list of discrete changes to make"), "summary", "todos"),
	}
}

// runComplex runs the multi-turn discovery/analysis stage.
func runComplex(ctx context.Context, client api.ClientInterface) tierOutcome {
	sb := newSandbox(complexFiles())
	stop := func(s *sandbox) bool { return s.todos != "" }
	stats := drive(ctx, client, "complex", complexSystem(), complexTask(), complexTools(), sb, complexMaxTurns, stop)
	o := verifyComplex(sb)
	o.stats = stats
	o.todos = sb.todos
	return o
}

// verifyComplex scores whether the model explored, identified the real root
// cause (the missing subscription cleanup, not the email machinery), pointed at
// a correct fix site, produced discrete steps, and avoided the deprecated trap.
//
// Passing requires all of these — a weak model lured by the email-sending
// distractors or the deprecated legacy file will miss the cause or misscope.
func verifyComplex(sb *sandbox) tierOutcome {
	todos := strings.ToLower(sb.todos)
	explored := len(sb.reads) >= 2
	foundCause := strings.Contains(todos, "subscri") // subscription / unsubscribe
	foundFixSite := containsAny(todos, "service.go", "subscriptions.go", "removeforuser", "deleteaccount")
	discrete := todoLineCount(sb.todos) >= 2
	avoidsTrap := !containsAny(todos, "old_delete", "legacy", "deleteuseranddata")

	checks := map[string]bool{
		"explored":       explored,
		"found_cause":    foundCause,
		"found_fix_site": foundFixSite,
		"discrete_todos": discrete,
		"avoids_trap":    avoidsTrap,
	}
	score, failed := scoreChecks(checks)
	// Passing requires real analysis: explored, named the true cause and a real
	// fix site, in discrete steps. avoids_trap is scored (a thorough model may
	// legitimately mention cleaning up the dead legacy file) but doesn't gate —
	// a model fooled INTO the trap fails found_fix_site instead.
	o := tierOutcome{score: score, passed: explored && foundCause && foundFixSite && discrete}
	switch {
	case sb.todos == "":
		o.reason = "no todos submitted (failed: " + strings.Join(failed, ", ") + ")"
	case o.passed:
		o.reason = "found the root cause and scoped a sensible fix"
	default:
		o.reason = "failed: " + strings.Join(failed, ", ")
	}
	return o
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// todoLineCount counts non-empty lines, a format-agnostic proxy for "a discrete
// set of steps".
func todoLineCount(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}
