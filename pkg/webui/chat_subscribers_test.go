//go:build !js

package webui

import (
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

// Pointer-identity fakes — chatSubscribersRegistry only stores conn
// pointers; it never touches the underlying websocket state. We can use
// zero-value *websocket.Conn pointers as distinct test handles.
func fakeConn() *websocket.Conn {
	return &websocket.Conn{}
}

func TestChatSubscribersRegistry_SubscribeAndQuery(t *testing.T) {
	r := newChatSubscribersRegistry()

	c1, c2, c3 := fakeConn(), fakeConn(), fakeConn()
	r.Subscribe("chat-A", c1)
	r.Subscribe("chat-A", c2)
	r.Subscribe("chat-B", c3)

	if got := r.ChatCount(); got != 2 {
		t.Errorf("ChatCount = %d, want 2", got)
	}
	if got := len(r.Subscribers("chat-A")); got != 2 {
		t.Errorf("Subscribers(chat-A) len = %d, want 2", got)
	}
	if got := len(r.Subscribers("chat-B")); got != 1 {
		t.Errorf("Subscribers(chat-B) len = %d, want 1", got)
	}
	if !r.HasSubscribers("chat-A") {
		t.Error("HasSubscribers(chat-A) = false, want true")
	}
	if r.HasSubscribers("chat-C") {
		t.Error("HasSubscribers(chat-C) = true, want false (no one subscribed)")
	}
}

func TestChatSubscribersRegistry_SubscribeIsIdempotent(t *testing.T) {
	r := newChatSubscribersRegistry()
	c := fakeConn()
	r.Subscribe("chat-A", c)
	r.Subscribe("chat-A", c)
	r.Subscribe("chat-A", c)
	if got := len(r.Subscribers("chat-A")); got != 1 {
		t.Errorf("repeated subscribe should be a no-op, got %d subscribers", got)
	}
}

func TestChatSubscribersRegistry_UnsubscribePrunesEmptyChats(t *testing.T) {
	r := newChatSubscribersRegistry()
	c1, c2 := fakeConn(), fakeConn()
	r.Subscribe("chat-A", c1)
	r.Subscribe("chat-A", c2)
	r.Unsubscribe("chat-A", c1)
	if got := len(r.Subscribers("chat-A")); got != 1 {
		t.Errorf("after one unsubscribe, want 1 left, got %d", got)
	}
	r.Unsubscribe("chat-A", c2)
	if got := r.ChatCount(); got != 0 {
		t.Errorf("empty set should be pruned, ChatCount = %d, want 0", got)
	}
	if r.HasSubscribers("chat-A") {
		t.Error("HasSubscribers(chat-A) should be false after all subscribers removed")
	}
}

func TestChatSubscribersRegistry_UnsubscribeAll(t *testing.T) {
	r := newChatSubscribersRegistry()
	target := fakeConn()
	other := fakeConn()
	r.Subscribe("chat-A", target)
	r.Subscribe("chat-A", other)
	r.Subscribe("chat-B", target)
	r.Subscribe("chat-C", target)

	r.UnsubscribeAll(target)

	if got := len(r.Subscribers("chat-A")); got != 1 {
		t.Errorf("chat-A should still have `other`, got %d subscribers", got)
	}
	if r.HasSubscribers("chat-B") {
		t.Error("chat-B should be pruned after target unsubscribed")
	}
	if r.HasSubscribers("chat-C") {
		t.Error("chat-C should be pruned after target unsubscribed")
	}
}

func TestChatSubscribersRegistry_RejectsEmptyAndNilInputs(t *testing.T) {
	r := newChatSubscribersRegistry()

	// Empty / whitespace chat IDs are silently ignored.
	r.Subscribe("", fakeConn())
	r.Subscribe("   ", fakeConn())
	if got := r.ChatCount(); got != 0 {
		t.Errorf("empty chatID Subscribe should be no-op, ChatCount = %d", got)
	}
	if got := r.Subscribers(""); got != nil {
		t.Errorf("Subscribers(\"\") = %v, want nil", got)
	}

	// Nil conn is silently ignored.
	r.Subscribe("chat-A", nil)
	if r.HasSubscribers("chat-A") {
		t.Error("nil conn should not create a subscriber entry")
	}
}

func TestChatSubscribersRegistry_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	r := newChatSubscribersRegistry()

	// Use a fixed pool of conn pointers so concurrent goroutines can
	// safely subscribe/unsubscribe overlapping sets.
	conns := make([]*websocket.Conn, 16)
	for i := range conns {
		conns[i] = fakeConn()
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := conns[i%len(conns)]
			r.Subscribe("chat-A", c)
			r.Subscribe("chat-B", c)
			_ = r.Subscribers("chat-A")
			_ = r.HasSubscribers("chat-B")
			r.Unsubscribe("chat-A", c)
		}(i)
	}
	wg.Wait()

	// After all goroutines, chat-B should still have entries — chat-A's
	// subscribes and unsubscribes were balanced per conn.
	if !r.HasSubscribers("chat-B") {
		t.Error("expected chat-B to retain subscribers after concurrent run")
	}
}
