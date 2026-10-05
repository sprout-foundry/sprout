//go:build !js

// Hosted-preview subscriber — SP-155 §155a (TODO 155.5).
//
// In hosted workspaces the agent runs in-process inside the webui server,
// and its register_preview_port tool publishes a preview_port_registered
// event to the shared event bus (the same bus passed to ToolEnv). This
// file is the webui server's half of that wiring: it keeps the active
// hosted preview (the platform-registered URL) on the server so
// /api/preview/status can report it to the preview pane (155.6) instead
// of the URL being only printed to the model.
package webui

import (
	"log/slog"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/preview"
)

// hostedPreviewInfo is the recorded state of a platform-registered preview
// (SP-155 §155a, TODO 155.5): the URL the agent's register_preview_port
// call returned, plus its port/label and when it was registered. The
// platform owns that URL (it is not a dev server we started), so the
// preview pane embeds it as-is and the local start/restart/stop actions
// never apply to it.
type hostedPreviewInfo struct {
	URL          string
	Port         int
	Label        string
	RegisteredAt time.Time
}

// startHostedPreviewSubscriber subscribes to the event bus and records
// hosted-preview registrations (preview_port_registered events).
//
// It mirrors the run-buffer subscriber (run_buffer_subscriber.go): a
// named Subscribe plus a panic-recovering goroutine that ranges over the
// channel. A new registration supersedes the previous one — the agent
// calls register_preview_port when (re)starting a server, so the newest
// port is the one the pane should show. Nil eventBus (a surface with no
// shared bus) is a no-op.
func (ws *ReactWebServer) startHostedPreviewSubscriber() {
	if ws.eventBus == nil {
		return
	}

	ch := ws.eventBus.Subscribe("hosted-preview-subscriber")

	go func() {
		defer func() {
			if r := recover(); r != nil {
				ws.log().Error("hosted preview subscriber panicked", slog.Any("panic", r))
			}
		}()
		for ev := range ch {
			if ev.Type != events.EventTypePreviewPortRegistered {
				continue
			}
			data, ok := ev.Data.(map[string]any)
			if !ok {
				continue
			}
			previewURL, _ := data["preview_url"].(string)
			if previewURL == "" {
				continue
			}
			ws.setHostedPreview(previewURL, intFromAny(data["port"]), dataLabel(data))
		}
		ws.log().Warn("hosted preview subscriber channel closed")
	}()
}

// setHostedPreview records (or replaces) the active hosted preview; a new
// registration supersedes the previous one.
func (ws *ReactWebServer) setHostedPreview(url string, port int, label string) {
	ws.hostedPreviewMu.Lock()
	ws.hostedPreview = &hostedPreviewInfo{
		URL:          url,
		Port:         port,
		Label:        label,
		RegisteredAt: time.Now(),
	}
	ws.hostedPreviewMu.Unlock()
}

// clearHostedPreview forgets the active hosted preview. It is in-memory
// state (server Shutdown) and must not survive a process restart.
func (ws *ReactWebServer) clearHostedPreview() {
	ws.hostedPreviewMu.Lock()
	ws.hostedPreview = nil
	ws.hostedPreviewMu.Unlock()
}

// hostedPreviewState returns the active hosted preview (SP-155 §155a,
// TODO 155.5) as the preview.State the pane renders: running, with the
// platform URL and the hosted marker. The second return is false when no
// hosted preview is registered (the caller then falls back to the local
// dev-server manager's state).
func (ws *ReactWebServer) hostedPreviewState() (preview.State, bool) {
	ws.hostedPreviewMu.RLock()
	defer ws.hostedPreviewMu.RUnlock()
	if ws.hostedPreview == nil {
		return preview.State{}, false
	}
	return preview.State{
		Status: preview.StatusRunning,
		URL:    ws.hostedPreview.URL,
		Hosted: true,
	}, true
}

// intFromAny decodes a numeric event-payload value (a JSON-decoded port is
// a float64; the in-process path hands over a Go int).
func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	default:
		return 0
	}
}

// dataLabel reads the optional label from a preview_port_registered
// payload (missing/empty is fine — the subscriber keeps the URL regardless).
func dataLabel(data map[string]any) string {
	label, _ := data["label"].(string)
	return label
}
