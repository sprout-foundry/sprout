package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// newBackgroundTestAgent returns an agent able to run background tasks: a
// wakeup host is registered, the context profile is full (low-context mode
// disables background runs), and auto-resume goes to a counting wake
// function instead of starting a real turn.
func newBackgroundTestAgent(t *testing.T, reply string, delay time.Duration) (*Agent, *atomic.Int32) {
	t.Helper()
	parent, _ := newReviewTestRunner(t)
	parent.contextProfile = configuration.ContextProfile{Mode: configuration.ContextModeFull}
	SetBackgroundHost(true)
	t.Cleanup(func() { SetBackgroundHost(false) })

	var wakes atomic.Int32
	parent.SetWakeupWakeFn(func() { wakes.Add(1) })
	parent.GetSubagentRunner().testClientFactory = func(api.ClientType, string) (api.ClientInterface, error) {
		b := NewScriptedResponseBuilder().Content(reply)
		if delay > 0 {
			b = b.Delay(delay)
		}
		return NewScriptedClient(b.Build()), nil
	}
	return parent, &wakes
}

func startedTaskID(t *testing.T, out string) string {
	t.Helper()
	var started struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &started); err != nil || started.TaskID == "" || started.Status != "running" {
		t.Fatalf("not a background start: %v\n%s", err, out)
	}
	return started.TaskID
}

func waitBackgroundDone(t *testing.T, a *Agent, id string) *backgroundTask {
	t.Helper()
	task := a.GetSubagentRunner().backgroundTask(id)
	if task == nil {
		t.Fatalf("task %s not registered", id)
	}
	select {
	case <-task.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("task %s did not finish", id)
	}
	return task
}

const reviewerReply = "Checked it.\n```json\n{\"verdict\":\"APPROVE\",\"findings\":[]}\n```\nVERDICT: APPROVE"

func TestResolveSubagentBackground(t *testing.T) {
	a, _ := newBackgroundTestAgent(t, reviewerReply, 0)

	if bg, _, err := resolveSubagentBackground(a, "reviewer", map[string]any{}); err != nil || !bg {
		t.Errorf("reviewer should default to background: %v %v", bg, err)
	}
	if bg, _, _ := resolveSubagentBackground(a, "reviewer", map[string]any{"background": false}); bg {
		t.Error("explicit background:false ignored")
	}
	if bg, iso, _ := resolveSubagentBackground(a, "coder", map[string]any{}); bg || iso {
		t.Error("coder must not default to background")
	}
	if bg, iso, err := resolveSubagentBackground(a, "coder", map[string]any{"background": true}); err != nil || !bg || !iso {
		t.Error("background coder must run isolated")
	}

	SetBackgroundHost(false)
	if bg, _, _ := resolveSubagentBackground(a, "reviewer", map[string]any{}); bg {
		t.Error("background chosen with no wakeup host to deliver the result")
	}
	SetBackgroundHost(true)

	a.contextProfile = configuration.ContextProfile{Mode: configuration.ContextModeLowContext}
	if bg, _, _ := resolveSubagentBackground(a, "reviewer", map[string]any{}); bg {
		t.Error("background chosen in low-context mode, which lacks check_subagent")
	}
}

func TestBackgroundSubagent_NotifiesAndResumes(t *testing.T) {
	a, wakes := newBackgroundTestAgent(t, "researched: the answer is 42 — long enough to count as complete output.", 0)

	out, err := handleRunSubagent(context.Background(), a, map[string]any{"persona": "researcher", "prompt": "find the answer"})
	if err != nil {
		t.Fatalf("handleRunSubagent: %v", err)
	}
	task := waitBackgroundDone(t, a, startedTaskID(t, out))
	if task.status != "completed" || !strings.Contains(task.result, "the answer is 42") {
		t.Fatalf("task = %s %q", task.status, task.result)
	}

	// Completions settle briefly before waking; poll like the CLI/WebUI poller.
	deadline := time.Now().Add(10 * time.Second)
	for wakes.Load() == 0 && time.Now().Before(deadline) {
		a.TryAutoResume()
		time.Sleep(50 * time.Millisecond)
	}
	if wakes.Load() != 1 {
		t.Fatalf("idle primary not resumed (wakes=%d)", wakes.Load())
	}
	batch := strings.Join(a.DrainWakeupForREPL(), "\n")
	if !strings.Contains(batch, "Background subagent task finished") || !strings.Contains(batch, task.ID) || !strings.Contains(batch, "the answer is 42") {
		t.Errorf("resume batch missing the result:\n%s", batch)
	}
}

func TestCheckSubagent_WaitsAndCollects(t *testing.T) {
	a, _ := newBackgroundTestAgent(t, reviewerReply, 300*time.Millisecond)

	out, err := handleRunSubagent(context.Background(), a, map[string]any{"persona": "reviewer", "prompt": "review it"})
	if err != nil {
		t.Fatal(err)
	}
	id := startedTaskID(t, out)

	progress, err := handleCheckSubagent(context.Background(), a, map[string]any{"task_id": id})
	if err != nil || !strings.Contains(progress, "running for") {
		t.Errorf("progress while running: %v\n%s", err, progress)
	}
	list, _ := handleCheckSubagent(context.Background(), a, map[string]any{})
	if !strings.Contains(list, id) {
		t.Errorf("task missing from list:\n%s", list)
	}

	result, err := handleCheckSubagent(context.Background(), a, map[string]any{"task_id": id, "wait_seconds": float64(10)})
	if err != nil || !strings.Contains(result, "completed") || !strings.Contains(result, "VERDICT: APPROVE") {
		t.Fatalf("waited result: %v\n%s", err, result)
	}

	a.QueueNotification(Notification{SessionID: id, Kind: NotifSubagent, Content: "late"})
	if _, err := handleCheckSubagent(context.Background(), a, map[string]any{"task_id": id}); err != nil {
		t.Fatal(err)
	}
	for _, n := range a.DrainNotifications() {
		if n.SessionID == id {
			t.Error("collected task's notification still queued")
		}
	}

	if _, err := handleCheckSubagent(context.Background(), a, map[string]any{"task_id": "bg-nope-9"}); err == nil {
		t.Error("unknown task accepted")
	}
}

func TestStopSubagent_StopsWithoutNotification(t *testing.T) {
	a, wakes := newBackgroundTestAgent(t, reviewerReply, 30*time.Second)

	out, err := handleRunSubagent(context.Background(), a, map[string]any{"persona": "reviewer", "prompt": "review it"})
	if err != nil {
		t.Fatal(err)
	}
	id := startedTaskID(t, out)
	time.Sleep(100 * time.Millisecond)

	msg, err := handleStopSubagent(context.Background(), a, map[string]any{"task_id": id})
	if err != nil || !strings.Contains(msg, "Stopped") {
		t.Fatalf("stop: %v %s", err, msg)
	}
	task := waitBackgroundDone(t, a, id)
	if task.status != "stopped" {
		t.Errorf("status = %s, want stopped", task.status)
	}
	if a.HasPendingNotifications() || wakes.Load() != 0 {
		t.Error("stopped task notified the primary")
	}
}

func TestCancelAll_CancelsBackgroundWithoutNotification(t *testing.T) {
	a, wakes := newBackgroundTestAgent(t, reviewerReply, 30*time.Second)

	out, err := handleRunSubagent(context.Background(), a, map[string]any{"persona": "reviewer", "prompt": "review it"})
	if err != nil {
		t.Fatal(err)
	}
	id := startedTaskID(t, out)
	time.Sleep(100 * time.Millisecond)

	a.GetSubagentRunner().CancelAll()
	task := waitBackgroundDone(t, a, id)
	if task.status != "cancelled" {
		t.Errorf("status = %s, want cancelled", task.status)
	}
	if a.HasPendingNotifications() || wakes.Load() != 0 {
		t.Error("a user Stop must not resume the agent with the cancelled task")
	}
}

func TestReviewChanges_BackgroundAndBlocking(t *testing.T) {
	a, _ := newBackgroundTestAgent(t, reviewerReply, 0)
	dir := initReviewRepo(t)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(1) }\n")
	a.workspaceRoot = dir

	out, err := handleReviewChanges(context.Background(), a, map[string]any{})
	if err != nil {
		t.Fatalf("handleReviewChanges: %v", err)
	}
	task := waitBackgroundDone(t, a, startedTaskID(t, out))
	if task.status != "completed" || !strings.Contains(task.result, "Review verdict: APPROVE") {
		t.Errorf("background review = %s\n%s", task.status, task.result)
	}

	blocking, err := handleReviewChanges(context.Background(), a, map[string]any{"background": false})
	if err != nil || !strings.HasPrefix(blocking, "Review verdict: APPROVE") {
		t.Errorf("blocking review: %v\n%s", err, blocking)
	}

	SetBackgroundHost(false)
	defer SetBackgroundHost(true)
	noHost, err := handleReviewChanges(context.Background(), a, map[string]any{})
	if err != nil || !strings.HasPrefix(noHost, "Review verdict:") {
		t.Errorf("without a wakeup host the review must run blocking: %v\n%s", err, noHost)
	}

	if _, err := handleReviewChanges(context.Background(), a, map[string]any{"scope": "range", "range": "--bad"}); err == nil {
		t.Error("invalid range accepted for a background review")
	}
}

func TestBackgroundTaskDetail_LeavesNotificationQueued(t *testing.T) {
	a, _ := newBackgroundTestAgent(t, reviewerReply, 0)
	out, err := handleRunSubagent(context.Background(), a, map[string]any{"persona": "reviewer", "prompt": "review it"})
	if err != nil {
		t.Fatal(err)
	}
	id := startedTaskID(t, out)
	waitBackgroundDone(t, a, id)

	a.QueueNotification(Notification{SessionID: id, Kind: NotifSubagent, Content: "pending"})
	if detail, err := a.BackgroundTaskDetail(id); err != nil || !strings.Contains(detail, "VERDICT: APPROVE") {
		t.Fatalf("detail: %v\n%s", err, detail)
	}
	if !a.HasPendingNotifications() {
		t.Error("viewing a task from /tasks consumed the agent's notification")
	}
	if !strings.Contains(a.BackgroundTasksSummary(), id) {
		t.Error("task missing from summary")
	}
}
