package agent

// Task prompts: a background task has no terminal of its own, but it does
// have somewhere a human can answer — the WebUI the primary is attached to.
//
// The distinction that matters is not "interactive" but "can a human answer
// right now without corrupting the primary's turn". The WebUI is always safe:
// approval and ask_user requests are request-ID scoped and resolve against a
// shared manager, so a task's dialog lands in the same pending map the user's
// answer hits. The primary's TTY is never safe for a task — a prompt would
// interleave with the primary's own spinner and streaming output.
//
// A task therefore runs with workflowRun set (no TTY prompts, and no
// auto-approve from the permissive non-interactive default) plus taskRun set,
// which keeps the WebUI surface (see approvals/broker.go). When no browser is
// attached, the request fails fast with a message that names the surface —
// which is what the old blanket SetWorkflowRun(true) did, minus the "can
// never ask" part.

// taskNoSurfaceMessage explains, in the tool's error text, why a task could
// not ask and what the user can do about it.
const taskNoSurfaceMessage = "this task has no interactive surface: no browser tab is attached to answer " +
	"its approval or question. Open the WebUI for this workspace, or re-run the task " +
	"with a risk profile that allows the operation."
