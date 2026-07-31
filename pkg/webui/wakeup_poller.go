package webui

import (
	"context"
	"time"
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
	if a.TryAutoResume() {
		ws.log().Info("automatically resuming wakeup notifications")
	}
}
