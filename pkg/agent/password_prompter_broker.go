package agent

import (
	"fmt"
	"sync"
)

// passwordPrompterBroker tracks pending password requests and their response
// channels. It mirrors the editApprovalBroker pattern: the caller registers a
// request, blocks on the returned channel, and the broker delivers the
// response when the user (via CLI or WebUI) provides the password.
//
// Package-level so that any agent instance can resolve any request ID —
// essential in daemon mode where multiple chat agents exist.
var passwordPrompterBroker = &passwordPrompterBrokerType{
	pending: make(map[string]chan string),
}

type passwordPrompterBrokerType struct {
	mu      sync.Mutex
	pending map[string]chan string
}

// register creates a buffered response channel for the given request ID and
// returns it. The caller blocks on the channel until a password is delivered
// or the context is cancelled. cleanup removes the entry after resolution.
func (b *passwordPrompterBrokerType) register(requestID string) chan string {
	ch := make(chan string, 1)
	b.mu.Lock()
	b.pending[requestID] = ch
	b.mu.Unlock()
	return ch
}

// respond delivers the password to the waiting goroutine. Returns false if
// the request was not found or already resolved.
func (b *passwordPrompterBrokerType) respond(requestID string, password string) bool {
	b.mu.Lock()
	ch, ok := b.pending[requestID]
	b.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- password:
		return true
	default:
		return false
	}
}

// cleanup removes a pending entry after it resolves or times out.
func (b *passwordPrompterBrokerType) cleanup(requestID string) {
	b.mu.Lock()
	delete(b.pending, requestID)
	b.mu.Unlock()
}

// generatePasswordRequestID produces a unique ID for a password request.
var (
	passwordReqCounter int64
	passwordReqMu      sync.Mutex
)

func generatePasswordRequestID() string {
	passwordReqMu.Lock()
	defer passwordReqMu.Unlock()
	passwordReqCounter++
	return fmt.Sprintf("pwd_%d", passwordReqCounter)
}
