//go:build !js

package webui

import (
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// chatSubscribers tracks which WebSocket connections are interested in
// each chat. Used by the multi-tab consistency path (SP-034-3): when an
// event publishes for chat X, we want every connection viewing chat X to
// receive it, even when their originating clientIDs differ (e.g. the
// same user opened the chat in two browser tabs, each with its own
// clientID cookie).
//
// Layout: chatID → set of conn pointers. The set is implemented as a
// map[*websocket.Conn]struct{} so add/remove are O(1). The outer mutex
// is a separate lock from ws.mutex to avoid lock-ordering hazards: this
// only protects the subscriber map, not any wider state.
type chatSubscribersRegistry struct {
	mu          sync.RWMutex
	subscribers map[string]map[*websocket.Conn]struct{}
}

func newChatSubscribersRegistry() *chatSubscribersRegistry {
	return &chatSubscribersRegistry{
		subscribers: make(map[string]map[*websocket.Conn]struct{}),
	}
}

// Subscribe registers conn as interested in chatID. Idempotent — repeated
// subscriptions for the same conn are a no-op.
func (r *chatSubscribersRegistry) Subscribe(chatID string, conn *websocket.Conn) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" || conn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	set, ok := r.subscribers[chatID]
	if !ok {
		set = make(map[*websocket.Conn]struct{})
		r.subscribers[chatID] = set
	}
	set[conn] = struct{}{}
}

// Unsubscribe removes conn from chatID's subscriber set. Empty sets are
// pruned so the outer map doesn't accumulate stale chat IDs.
func (r *chatSubscribersRegistry) Unsubscribe(chatID string, conn *websocket.Conn) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" || conn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	set, ok := r.subscribers[chatID]
	if !ok {
		return
	}
	delete(set, conn)
	if len(set) == 0 {
		delete(r.subscribers, chatID)
	}
}

// UnsubscribeAll removes conn from every chat it was subscribed to.
// Called on WebSocket disconnect so a dropped connection doesn't sit in
// the subscriber lists indefinitely. Cheap because the typical conn
// subscribes to one chat (its connected chat_id).
func (r *chatSubscribersRegistry) UnsubscribeAll(conn *websocket.Conn) {
	if conn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for chatID, set := range r.subscribers {
		if _, ok := set[conn]; ok {
			delete(set, conn)
			if len(set) == 0 {
				delete(r.subscribers, chatID)
			}
		}
	}
}

// Subscribers returns a snapshot of the connections subscribed to
// chatID. The snapshot is a copy — safe to range over while events fan
// out, since direct iteration over the underlying map would race with
// concurrent Subscribe/Unsubscribe.
func (r *chatSubscribersRegistry) Subscribers(chatID string) []*websocket.Conn {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	set, ok := r.subscribers[chatID]
	if !ok || len(set) == 0 {
		return nil
	}
	out := make([]*websocket.Conn, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	return out
}

// HasSubscribers reports whether any connection is subscribed to chatID.
// Cheaper than Subscribers when callers only need a boolean (e.g. to
// decide whether to construct a fan-out goroutine).
func (r *chatSubscribersRegistry) HasSubscribers(chatID string) bool {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	set, ok := r.subscribers[chatID]
	return ok && len(set) > 0
}

// ChatCount reports the number of distinct chats with at least one
// subscriber. Test/diagnostic helper.
func (r *chatSubscribersRegistry) ChatCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.subscribers)
}
