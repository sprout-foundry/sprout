package webui

import (
	"context"
	"log/slog"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

func (ws *ReactWebServer) startWakeupPoller(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ws.checkAndResume()
		}
	}
}

func (ws *ReactWebServer) checkAndResume() {
	a := ws.agent
	if a == nil {
		return
	}
	cfg := a.GetConfig()
	if cfg == nil || !cfg.Wakeup.Enabled {
		return
	}
	if !a.HasPendingNotifications() {
		return
	}
	if a.IsQueryInProgress() {
		return
	}
	if a.IsWakeupDisabled() {
		return
	}
	if !a.IncrementWakeupResume(cfg.Wakeup) {
		ws.log().Warn("wakeup token budget exhausted; skipping automatic resume")
		return
	}
	notifications := a.DrainNotifications()
	if len(notifications) == 0 {
		return
	}
	msg := agent.FormatWakeupBatch(notifications)
	ws.log().Info("automatically resuming wakeup notifications", slog.Int("notification_count", len(notifications)))
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ws.log().Error("automatic resume goroutine panicked", slog.Any("panic", r))
			}
		}()
		tokensBefore := a.GetTotalTokens()
		result, err := a.ProcessQueryWithContinuity(msg)
		if err != nil {
			ws.log().Error("automatic resume failed", slog.Any("err", err))
			return
		}
		tokensAfter := a.GetTotalTokens()
		delta := tokensAfter - tokensBefore
		if delta > 0 {
			a.RecordWakeupTokens(delta, cfg.Wakeup)
			ws.log().Info("automatic resume consumed tokens", slog.Int("tokens_consumed", delta), slog.Int("session_tokens", tokensAfter))
		}
		_ = result
	}()
}
