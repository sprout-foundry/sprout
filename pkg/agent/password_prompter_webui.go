//go:build !js

package agent

import (
	"context"
	"fmt"
	"log"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// passwordPromptTimeout is the maximum time a password request blocks
// waiting for a WebUI response. Shorter than edit approval's 30 min
// because passwords are time-sensitive (sudo credential caches, etc.).
var passwordPromptTimeout = 5 * time.Minute

// WebUIPasswordPrompter implements PasswordPrompter for WebUI sessions.
// It publishes a password_request event and blocks until the browser
// POSTs the response (or the timeout fires).
type WebUIPasswordPrompter struct {
	agent *Agent
}

// Compile-time assertion that WebUIPasswordPrompter satisfies PasswordPrompter.
var _ tools.PasswordPrompter = (*WebUIPasswordPrompter)(nil)

// NewWebUIPasswordPrompter creates a WebUI-backed password prompter.
func NewWebUIPasswordPrompter(agent *Agent) *WebUIPasswordPrompter {
	return &WebUIPasswordPrompter{agent: agent}
}

// Prompt asks the WebUI to collect a password from the user.
//
// Returns ErrNoInteractiveSurface when there's no event bus or no active
// WebUI clients. On timeout, returns a descriptive error. On context
// cancellation, returns ctx.Err().
func (wp *WebUIPasswordPrompter) Prompt(ctx context.Context, reason string) (string, error) {
	if wp.agent == nil || wp.agent.GetEventBus() == nil || !wp.agent.HasActiveWebUIClients() {
		return "", tools.ErrNoInteractiveSurface
	}

	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	requestID := generatePasswordRequestID()
	ch := passwordPrompterBroker.register(requestID)
	defer passwordPrompterBroker.cleanup(requestID)

	// TODO(SP-089-4): When the shell tool passes a real PasswordRequest
	// with separate command and prompt fields, use those instead of
	// passing `reason` as both.
	payload := events.PasswordRequestEvent(requestID, reason, reason)
	wp.agent.publishEvent(events.EventTypePasswordRequest, payload)
	// Notify input-required subscribers (CLI bell, browser notification).
	wp.agent.publishEvent(events.EventTypeInputRequired, events.InputRequiredEvent("password_request", requestID))

	log.Printf("[password_prompt] request %s — waiting up to %v for WebUI response",
		requestID, passwordPromptTimeout)

	timer := time.NewTimer(passwordPromptTimeout)
	defer timer.Stop()

	select {
	case password, ok := <-ch:
		if !ok {
			return "", agenterrors.NewAgent("password_prompter", "password channel closed without response", nil)
		}
		return password, nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		log.Printf("[password_prompt] request %s timed out after %v", requestID, passwordPromptTimeout)
		return "", agenterrors.NewTimeout(fmt.Sprintf("password prompt (%v)", passwordPromptTimeout), passwordPromptTimeout)
	}
}

// RespondToPasswordRequest delivers a user password to a pending password
// request. Called by the WebUI handler (POST /api/password/{id}/respond)
// when the user submits their password.
//
// Returns true if the request was found and the password was delivered.
func (a *Agent) RespondToPasswordRequest(requestID string, password string) bool {
	return passwordPrompterBroker.respond(requestID, password)
}
