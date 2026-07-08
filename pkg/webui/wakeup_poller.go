package webui

import (
	"context"
	"log"
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
		log.Printf("[wakeup] Budget exhausted — skipping auto-resume")
		return
	}
	notifications := a.DrainNotifications()
	if len(notifications) == 0 {
		return
	}
	msg := agent.FormatWakeupBatch(notifications)
	log.Printf("[wakeup] Auto-resuming with %d notification(s)", len(notifications))
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[wakeup] panic in auto-resume goroutine: %v", r)
			}
		}()
		tokensBefore := a.GetTotalTokens()
		result, err := a.ProcessQueryWithContinuity(msg)
		if err != nil {
			log.Printf("[wakeup] Auto-resume failed: %v", err)
			return
		}
		tokensAfter := a.GetTotalTokens()
		delta := tokensAfter - tokensBefore
		if delta > 0 {
			a.RecordWakeupTokens(delta, cfg.Wakeup)
			log.Printf("[wakeup] Auto-resume consumed %d tokens (session total: %d)", delta, tokensAfter)
		}
		_ = result
	}()
}
