package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// NotificationKind classifies the source of a background completion notification.
type NotificationKind string

const (
	NotifAutomate       NotificationKind = "automate"
	NotifShellBg        NotificationKind = "shell_bg"
	NotifShellBgTimeout NotificationKind = "shell_bg_timeout"
)

// Notification is a durable completion message queued when a background task
// finishes. It survives turn boundaries — unlike channel-based injection
// (InjectInputContext), which loses messages when the forwarder goroutine dies
// at turn end.
type Notification struct {
	Content   string           // formatted message for the agent
	SessionID string           // bg session or automate session ID
	Kind      NotificationKind // source of the notification
	Timestamp time.Time        // when the notification was queued
}

func (n Notification) FormatForAgent() string {
	var b strings.Builder
	b.WriteString("[wakeup] ")
	switch n.Kind {
	case NotifAutomate:
		b.WriteString("Automate workflow completed\n\n")
	case NotifShellBg:
		b.WriteString("Background command completed\n\n")
	case NotifShellBgTimeout:
		b.WriteString("Background command timed out\n\n")
	}
	b.WriteString(n.Content)
	return b.String()
}

func FormatWakeupBatch(notifications []Notification) string {
	if len(notifications) == 0 {
		return ""
	}
	if len(notifications) == 1 {
		return notifications[0].FormatForAgent()
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[wakeup] %d background tasks completed\n\n", len(notifications)))
	for i, n := range notifications {
		b.WriteString(fmt.Sprintf("--- task %d (%s) ---\n", i+1, n.SessionID))
		b.WriteString(n.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// --- Notification queue methods on Agent ---

func (a *Agent) QueueNotification(n Notification) {
	if a == nil {
		return
	}
	a.notifMu.Lock()
	defer a.notifMu.Unlock()
	n.Timestamp = time.Now()
	a.pendingNotifications = append(a.pendingNotifications, n)
}

func (a *Agent) DrainNotifications() []Notification {
	if a == nil {
		return nil
	}
	a.notifMu.Lock()
	defer a.notifMu.Unlock()
	if len(a.pendingNotifications) == 0 {
		return nil
	}
	out := a.pendingNotifications
	a.pendingNotifications = nil
	return out
}

func (a *Agent) HasPendingNotifications() bool {
	if a == nil {
		return false
	}
	a.notifMu.Lock()
	defer a.notifMu.Unlock()
	return len(a.pendingNotifications) > 0
}

// --- Wakeup budget methods ---

func (a *Agent) IsWakeupDisabled() bool {
	if a == nil {
		return true
	}
	a.wakeupMu.Lock()
	defer a.wakeupMu.Unlock()
	return a.wakeupDisabled
}

func (a *Agent) DisableWakeup() {
	if a == nil {
		return
	}
	a.wakeupMu.Lock()
	defer a.wakeupMu.Unlock()
	a.wakeupDisabled = true
}

func (a *Agent) EnableWakeupIfDisabled() {
	if a == nil {
		return
	}
	a.wakeupMu.Lock()
	defer a.wakeupMu.Unlock()
	a.wakeupDisabled = false
}

func (a *Agent) IncrementWakeupResume(cfg configuration.WakeupConfig) bool {
	if a == nil {
		return false
	}
	a.wakeupMu.Lock()
	defer a.wakeupMu.Unlock()
	if a.wakeupDisabled {
		return false
	}
	if cfg.MaxResumesPerSession > 0 && a.wakeupResumeCount >= cfg.MaxResumesPerSession {
		a.wakeupDisabled = true
		return false
	}
	a.wakeupResumeCount++
	return true
}

func (a *Agent) RecordWakeupTokens(tokens int, cfg configuration.WakeupConfig) {
	if a == nil || tokens <= 0 {
		return
	}
	a.wakeupMu.Lock()
	defer a.wakeupMu.Unlock()
	a.wakeupTokensConsumed += tokens
	if cfg.MaxTokensPerSession > 0 && a.wakeupTokensConsumed >= cfg.MaxTokensPerSession {
		a.wakeupDisabled = true
	}
}

func (a *Agent) NotifyCompletion(sessionID, kind, content string) {
	a.QueueNotification(Notification{
		Content:   content,
		SessionID: sessionID,
		Kind:      NotificationKind(kind),
	})
}

// TryAutoResume checks whether there are pending background-task
// notifications that warrant an automatic agent resume. If so, it
// drains them and calls ProcessQueryWithContinuity to re-invoke the
// agent so it can act on the completed background tasks.
//
// This is the shared entry point used by both the WebUI wakeup poller
// (pkg/webui/wakeup_poller.go) and the CLI interactive loop
// (cmd/agent_mode_interactive.go). It encapsulates the budget checks
// (max resumes, max tokens), notification draining, and the actual
// resume call.
//
// Returns true if a resume was performed, false if conditions were not
// met (no notifications, wakeup disabled, budget exhausted, or a query
// is already in progress).
func (a *Agent) TryAutoResume() bool {
	if a == nil {
		return false
	}
	cfg := a.GetConfig()
	if cfg == nil || !cfg.Wakeup.Enabled {
		return false
	}
	if !a.HasPendingNotifications() {
		return false
	}
	if a.IsQueryInProgress() {
		return false
	}
	if a.IsWakeupDisabled() {
		return false
	}
	if !a.IncrementWakeupResume(cfg.Wakeup) {
		return false
	}
	notifications := a.DrainNotifications()
	if len(notifications) == 0 {
		return false
	}
	msg := FormatWakeupBatch(notifications)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				a.Logger().Debug("[wakeup] auto-resume panicked: %v\n", r)
			}
		}()
		tokensBefore := a.GetTotalTokens()
		_, err := a.ProcessQueryWithContinuityAs(QuerySourceAutoResume, msg)
		if err != nil {
			a.Logger().Debug("[wakeup] auto-resume failed: %v\n", err)
			return
		}
		tokensAfter := a.GetTotalTokens()
		delta := tokensAfter - tokensBefore
		if delta > 0 {
			a.RecordWakeupTokens(delta, cfg.Wakeup)
		}
	}()
	return true
}
