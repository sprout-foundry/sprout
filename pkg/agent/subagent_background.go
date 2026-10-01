package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sprout-foundry/sprout/pkg/agent/subagents"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

const (
	maxBackgroundWait          = 600 * time.Second
	backgroundRecentOutput     = 8
	maxRetainedBackgroundTasks = 50
)

// backgroundTask is a subagent run (or a review_changes run) that the primary
// started without waiting for. Its completion is delivered through the
// notification queue, so it reaches the primary even after its turn ended.
type backgroundTask struct {
	ID      string
	Kind    string // "subagent" | "review"
	Persona string
	Label   string
	Started time.Time

	cancel context.CancelFunc
	done   chan struct{}

	// Set once done is closed.
	finished time.Time
	result   string
	status   string // "completed" | "failed" | "cancelled" | "stopped"

	// stoppedByTool suppresses the completion notification: the primary
	// asked for the stop, so there is nothing to wake it for.
	stoppedByTool bool
}

func (t *backgroundTask) isDone() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

// backgroundHost is set while this process runs a wakeup poller (the
// interactive CLI or the WebUI server). Without one, a finished background
// task could not resume an idle primary, and a one-shot run would exit and
// orphan it — so background requests run blocking instead.
var backgroundHost atomic.Int32

// SetBackgroundHost marks whether a wakeup poller is running in this process.
// Pollers call it with true on start and false on exit.
func SetBackgroundHost(active bool) {
	if active {
		backgroundHost.Add(1)
	} else {
		backgroundHost.Add(-1)
	}
}

// backgroundAvailable reports whether background subagent runs can be
// delivered: a wakeup poller is running and auto-resume is enabled.
func (a *Agent) backgroundAvailable() bool {
	if backgroundHost.Load() <= 0 {
		return false
	}
	if a.contextProfile.Mode == configuration.ContextModeLowContext {
		return false // the check/stop tools aren't in the low-context tool set
	}
	cfg := a.GetConfig()
	return cfg != nil && cfg.Wakeup.Enabled
}

// registerBackground records a new background task and returns it.
func (r *SubagentRunner) registerBackground(kind, persona, label string, cancel context.CancelFunc) *backgroundTask {
	r.bgMu.Lock()
	defer r.bgMu.Unlock()
	if r.bg == nil {
		r.bg = map[string]*backgroundTask{}
	}
	r.bgSeq++
	t := &backgroundTask{
		ID:      fmt.Sprintf("bg-%s-%d", kind, r.bgSeq),
		Kind:    kind,
		Persona: persona,
		Label:   label,
		Started: time.Now(),
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	r.bg[t.ID] = t
	r.bgOrder = append(r.bgOrder, t.ID)
	r.pruneBackgroundLocked()
	return t
}

// pruneBackgroundLocked drops the oldest finished tasks beyond the retention cap.
func (r *SubagentRunner) pruneBackgroundLocked() {
	for len(r.bgOrder) > maxRetainedBackgroundTasks {
		pruned := false
		for i, id := range r.bgOrder {
			if t := r.bg[id]; t == nil || t.isDone() {
				delete(r.bg, id)
				r.bgOrder = append(r.bgOrder[:i], r.bgOrder[i+1:]...)
				pruned = true
				break
			}
		}
		if !pruned {
			return
		}
	}
}

// hasRunningBackground reports whether any background task is unfinished.
func (r *SubagentRunner) hasRunningBackground() bool {
	r.bgMu.Lock()
	defer r.bgMu.Unlock()
	for _, t := range r.bg {
		if !t.isDone() {
			return true
		}
	}
	return false
}

func (r *SubagentRunner) backgroundTask(id string) *backgroundTask {
	r.bgMu.Lock()
	defer r.bgMu.Unlock()
	return r.bg[id]
}

func (r *SubagentRunner) backgroundTasks() []*backgroundTask {
	r.bgMu.Lock()
	defer r.bgMu.Unlock()
	out := make([]*backgroundTask, 0, len(r.bgOrder))
	for _, id := range r.bgOrder {
		if t := r.bg[id]; t != nil {
			out = append(out, t)
		}
	}
	return out
}

// finishBackground records the outcome and, unless the primary stopped the
// task itself or the user cancelled the session's work, queues a completion
// notification and tries to resume an idle primary.
func (a *Agent) finishBackground(t *backgroundTask, status, result string) {
	r := a.GetSubagentRunner()
	r.bgMu.Lock()
	if t.stoppedByTool {
		status = "stopped"
	}
	t.status, t.result, t.finished = status, result, time.Now()
	notify := !t.stoppedByTool && status != "cancelled"
	r.bgMu.Unlock()
	close(t.done)

	if !notify {
		return
	}
	a.NotifyCompletionLabeled(t.ID, string(NotifSubagent), formatBackgroundCompletion(t), t.Label)
	a.TryAutoResume()
}

func formatBackgroundCompletion(t *backgroundTask) string {
	who := t.Persona
	if t.Kind == "review" {
		who = "review_changes"
	}
	return fmt.Sprintf("Background task %s (%s: %s) %s after %s.\n\n%s",
		t.ID, who, t.Label, t.status, t.finished.Sub(t.Started).Round(time.Second), t.result)
}

// resolveSubagentBackground decides how run_subagent runs. background: an
// explicit `background` argument wins, otherwise read-only personas default
// to background. isolate: run in an isolated worktree — requested with
// `isolation: "worktree"`, and forced for a background run of a persona that
// modifies files, whose edits would otherwise collide with the primary's.
// Without a wakeup host to deliver results, runs fall back to blocking.
func resolveSubagentBackground(a *Agent, persona string, args map[string]interface{}) (background, isolate bool, err error) {
	readOnly := false
	if cfg := a.GetConfig(); cfg != nil {
		if st := cfg.GetSubagentType(persona); st != nil {
			readOnly = st.ReadOnly
		}
	}
	switch iso, _ := args["isolation"].(string); iso {
	case "", "none":
	case "worktree":
		isolate = true
	default:
		return false, false, agenterrors.NewValidation(fmt.Sprintf("unknown isolation %q (use \"worktree\")", iso), nil)
	}

	background = readOnly
	if v, ok := args["background"].(bool); ok {
		background = v
	}
	if !a.backgroundAvailable() {
		return false, isolate, nil
	}
	if background && !readOnly {
		isolate = true
	}
	return background, isolate, nil
}

// startBackgroundSubagent launches spec without waiting and returns the task
// handle as the tool result.
func startBackgroundSubagent(a *Agent, spec *subagentLaunchSpec, isolate bool) (string, error) {
	runner := a.GetSubagentRunner()
	bgCtx, cancel := context.WithCancel(context.Background())
	t := runner.registerBackground("subagent", spec.persona, ShortCommandLabel(spec.prompt), cancel)

	printSubagentStart(spec.persona+", background", displayOrDefault(spec.provider), displayOrDefault(spec.model))
	go func() {
		defer cancel()
		var result *SubagentResult
		if isolate {
			result = runIsolatedSubagent(bgCtx, a, spec, t.ID, true)
		} else {
			result = runner.runTask(bgCtx, t.ID, spec.enhancedPrompt, SubagentOptions{
				Persona:      spec.persona,
				Model:        spec.model,
				Provider:     spec.provider,
				SystemPrompt: spec.systemPromptText,
				WorkingDir:   spec.workingDir,
				Quiet:        true,
			}, nil, 0)
		}
		text, err := finishSubagentRun(bgCtx, a, spec, result)
		status := "completed"
		switch {
		case result != nil && result.Cancelled:
			status = "cancelled"
		case err != nil:
			status, text = "failed", err.Error()
		case result != nil && result.Error != nil:
			status = "failed"
		}
		a.finishBackground(t, status, text)
	}()

	return backgroundStartedMessage(t), nil
}

// startBackgroundReview runs review_changes without waiting.
func (a *Agent) startBackgroundReview(opts ReviewChangesOptions) (string, error) {
	// Fail fast on bad input instead of reporting it as a background failure.
	if _, err := opts.target(); err != nil {
		return "", err
	}
	if !a.CanSpawnSubagents() {
		return "", agenterrors.NewValidation("review_changes spawns reviewer subagents and is not available at this subagent depth", nil)
	}
	runner := a.GetSubagentRunner()
	bgCtx, cancel := context.WithCancel(context.Background())
	label := "review " + strings.TrimSpace(map[string]string{"": "working tree", "working_tree": "working tree", "staged": "staged changes"}[opts.Scope])
	if opts.Scope == "range" {
		label = "review " + opts.Range
	}
	t := runner.registerBackground("review", "reviewer", label, cancel)
	opts.taskPrefix = t.ID
	opts.quiet = true

	go func() {
		defer cancel()
		review, err := a.ReviewChanges(bgCtx, opts)
		switch {
		case bgCtx.Err() != nil:
			a.finishBackground(t, "cancelled", "")
		case err != nil:
			a.finishBackground(t, "failed", err.Error())
		default:
			a.finishBackground(t, "completed", review.Markdown())
		}
	}()
	return backgroundStartedMessage(t), nil
}

func backgroundStartedMessage(t *backgroundTask) string {
	out, _ := json.MarshalIndent(map[string]any{
		"task_id": t.ID,
		"status":  "running",
		"message": "Started in the background. Continue with other work: you will be notified with the result when it finishes " +
			"(even if your turn has ended). To block for it, call check_subagent with this task_id and wait_seconds; " +
			"check_subagent without wait_seconds shows progress.",
	}, "", "  ")
	return string(out)
}

// handleCheckSubagent implements check_subagent: list background tasks, or
// report one task's progress or result, optionally waiting for it.
func handleCheckSubagent(ctx context.Context, a *Agent, args map[string]any) (string, error) {
	runner := a.GetSubagentRunner()
	id, _ := args["task_id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return listBackgroundTasks(runner), nil
	}
	t := runner.backgroundTask(id)
	if t == nil {
		return "", agenterrors.NewNotFound("background task " + id)
	}

	if wait := waitSecondsArg(args); wait > 0 && !t.isDone() {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-t.done:
		case <-timer.C:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	if t.isDone() {
		// The result is being delivered now; drop the queued notification
		// so it doesn't trigger a redundant resume turn later.
		a.removeNotifications(t.ID)
		return fmt.Sprintf("Task %s %s after %s.\n\n%s", t.ID, t.status, t.finished.Sub(t.Started).Round(time.Second), t.result), nil
	}
	return describeRunningTask(runner, t), nil
}

func waitSecondsArg(args map[string]any) time.Duration {
	var secs float64
	switch v := args["wait_seconds"].(type) {
	case float64:
		secs = v
	case int:
		secs = float64(v)
	}
	if secs <= 0 {
		return 0
	}
	if d := time.Duration(secs * float64(time.Second)); d < maxBackgroundWait {
		return d
	}
	return maxBackgroundWait
}

func listBackgroundTasks(r *SubagentRunner) string {
	tasks := r.backgroundTasks()
	if len(tasks) == 0 {
		return "No background tasks."
	}
	var b strings.Builder
	for _, t := range tasks {
		status := "running"
		elapsed := time.Since(t.Started)
		if t.isDone() {
			status, elapsed = t.status, t.finished.Sub(t.Started)
		}
		fmt.Fprintf(&b, "- %s · %s · %s · %s · %s\n", t.ID, t.Persona, t.Label, status, elapsed.Round(time.Second))
	}
	return b.String()
}

// describeRunningTask reports progress of the subagent runs belonging to t:
// the task itself, or a review's per-slice reviewers (IDs prefixed by t.ID).
func describeRunningTask(r *SubagentRunner, t *backgroundTask) string {
	var runs []*runningSubagent
	r.active.Range(func(key, value any) bool {
		id, _ := key.(string)
		if sub, ok := value.(*runningSubagent); ok && (id == t.ID || strings.HasPrefix(id, t.ID+"-")) {
			runs = append(runs, sub)
		}
		return true
	})
	sort.Slice(runs, func(i, j int) bool { return runs[i].ID < runs[j].ID })

	var b strings.Builder
	fmt.Fprintf(&b, "Task %s (%s: %s) running for %s.\n", t.ID, t.Persona, t.Label, time.Since(t.Started).Round(time.Second))
	if len(runs) == 0 {
		b.WriteString("Preparing (no subagent running yet).\n")
	}
	for _, sub := range runs {
		state := "running"
		if sub.Completed.Load() {
			state = "done"
		}
		iterations, tokens, tools := 0, 0, 0
		if sub.Agent != nil {
			iterations = sub.Agent.state.GetCurrentIteration()
			tokens = sub.Agent.state.GetTotalTokens()
			tools = sub.Agent.state.GetTotalToolCalls()
		}
		fmt.Fprintf(&b, "- %s: %s · step %d · %d tool calls · %s tokens\n", sub.ID, state, iterations, tools, subagents.CompactCount(tokens))
		for _, line := range sub.recentOutput(backgroundRecentOutput) {
			fmt.Fprintf(&b, "    | %s\n", line)
		}
	}
	return b.String()
}

// handleStopSubagent implements stop_subagent.
func handleStopSubagent(_ context.Context, a *Agent, args map[string]any) (string, error) {
	runner := a.GetSubagentRunner()
	id, _ := args["task_id"].(string)
	t := runner.backgroundTask(strings.TrimSpace(id))
	if t == nil {
		return "", agenterrors.NewNotFound("background task " + id)
	}
	if t.isDone() {
		return fmt.Sprintf("Task %s already %s.", t.ID, t.status), nil
	}
	runner.bgMu.Lock()
	t.stoppedByTool = true
	runner.bgMu.Unlock()
	t.cancel()
	select {
	case <-t.done:
	case <-time.After(10 * time.Second):
	}
	return fmt.Sprintf("Stopped task %s.", t.ID), nil
}

// BackgroundTasksSummary lists background subagent tasks for display.
func (a *Agent) BackgroundTasksSummary() string {
	return listBackgroundTasks(a.GetSubagentRunner())
}

// BackgroundTaskDetail reports one task's progress or result for display.
// Unlike check_subagent it leaves the task's pending notification queued:
// the user looking at a result doesn't mean the agent has seen it.
func (a *Agent) BackgroundTaskDetail(id string) (string, error) {
	runner := a.GetSubagentRunner()
	t := runner.backgroundTask(strings.TrimSpace(id))
	if t == nil {
		return "", agenterrors.NewNotFound("background task " + id)
	}
	if t.isDone() {
		return fmt.Sprintf("Task %s %s after %s.\n\n%s", t.ID, t.status, t.finished.Sub(t.Started).Round(time.Second), t.result), nil
	}
	return describeRunningTask(runner, t), nil
}

// StopBackgroundTask stops a running background task.
func (a *Agent) StopBackgroundTask(id string) (string, error) {
	return handleStopSubagent(context.Background(), a, map[string]any{"task_id": id})
}
