// Event bus implementation
package events

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// isCriticalEvent reports whether eventType must never be silently dropped.
func isCriticalEvent(eventType string) bool {
	switch eventType {
	case EventTypeSecurityApprovalRequest,
		EventTypeSecurityPromptRequest,
		EventTypeAskUserRequest,
		EventTypeEditApprovalRequest,
		EventTypePasswordRequest,
		EventTypeInputRequired:
		return true
	}
	return false
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	eb := &EventBus{
		subscribers:   make(map[string]chan UIEvent),
		workers:       make(map[string]*subscriberWorker),
		deliveryQueue: make(chan eventDelivery, sharedDeliveryQueueSize),
	}
	go eb.runDispatcher()
	return eb
}

// Subscribe adds a new subscriber to the event bus and returns its receive
// channel. Each Subscribe spawns a dedicated worker goroutine that reads
// from a private inbox fed by the dispatcher.
func (eb *EventBus) Subscribe(name string) <-chan UIEvent {
	eb.mutex.Lock()
	defer eb.mutex.Unlock()

	// Generous buffer so a transient consumer stall (a backgrounded/laggy
	// browser tab, a burst of token-level stream chunks) doesn't immediately
	// overflow and start silently dropping non-critical events. The
	// dispatcher coalesces adjacent stream chunks before they reach the
	// worker, so this headroom is rarely approached in practice.
	ch := make(chan UIEvent, subscriberBufferSize)
	eb.subscribers[name] = ch
	w := &subscriberWorker{
		inbox: make(chan eventDelivery, subscriberInboxSize),
		done:  make(chan struct{}),
	}
	eb.workers[name] = w
	go eb.runWorker(name, ch, w)
	return ch
}

// Unsubscribe removes a subscriber. The worker is signalled first so it
// can drain its inbox and close the receive channel itself — that avoids
// the close-during-send race that the historical direct-close path could
// hit when a publish was in flight.
//
// Double-Unsubscribe is a no-op. Closing w.done twice would panic, and
// two concurrent Unsubscribe callers race the channel close (one wins,
// one panics). We guard with sync.Once and re-check the map.
func (eb *EventBus) Unsubscribe(name string) {
	eb.mutex.Lock()
	w, wok := eb.workers[name]
	if wok {
		delete(eb.workers, name)
	}
	_, sok := eb.subscribers[name]
	if sok {
		delete(eb.subscribers, name)
	}
	eb.mutex.Unlock()

	if wok {
		w.closeOnce.Do(func() { close(w.done) })
		// The worker closes the receive channel itself when it exits.
		// Don't close it here — concurrent sends from the dispatcher
		// (with a stale snapshot) would panic.
	}
}

// runDispatcher reads coalesced events from deliveryQueue and fans them
// out to each subscriber's inbox. Stream_chunk events are coalesced across
// the queue (not across per-subscriber inboxes), so a burst of 50 token
// chunks becomes 1–3 inbox sends per subscriber instead of 50.
//
// The dispatcher never blocks on a slow subscriber: if a subscriber's
// inbox is full, the dispatcher drops the non-critical event for THAT
// subscriber only and continues. Other subscribers keep receiving.
func (eb *EventBus) runDispatcher() {
	for ev := range eb.deliveryQueue {
		// Coalesce (currently a no-op; see coalesceBatch). Stream-chunk
		// coalescing is performed at the WS write drain in
		// pkg/webui/stream_coalesce.go, where many events have already
		// accumulated — the right place for the optimization.
		batch := eb.coalesceBatch(ev)

		// Snapshot subscribers BY NAME (not by random map iteration
		// order), so we pair each receive channel with its own inbox.
		// Earlier revisions built two parallel slices from `subscribers`
		// and `workers` and paired by index — Go maps iterate in
		// randomized order, so that produced subscriber cross-talk
		// (events meant for A would go to B's receive channel).
		eb.mutex.RLock()
		// Sort names for deterministic snapshot order — also avoids any
		// platform-specific map iteration quirks.
		names := make([]string, 0, len(eb.subscribers))
		for name := range eb.subscribers {
			names = append(names, name)
		}
		sort.Strings(names)
		receives := make([]chan UIEvent, len(names))
		inboxes := make([]chan eventDelivery, len(names))
		hasInbox := make([]bool, len(names))
		for i, name := range names {
			receives[i] = eb.subscribers[name]
			if w, ok := eb.workers[name]; ok {
				inboxes[i] = w.inbox
				hasInbox[i] = true
			}
		}
		eb.mutex.RUnlock()

		for i, name := range names {
			for _, d := range batch {
				if hasInbox[i] {
					inbox := inboxes[i]
					func() {
						defer func() { _ = recover() }()
						select {
						case inbox <- d:
						default:
							if !d.isCritical {
								log.Printf("[EventBus] Dropped %s event: subscriber %s inbox full (cap=%d)", d.event.Type, name, subscriberInboxSize)
								// The event will never reach this subscriber's
								// worker, so account for it here.
								accountForDelivery(d)
								return
							}
							// Critical: drain one and retry once. Bounded
							// latency (no blocking send on the hot path).
							select {
							case <-inbox:
								select {
								case inbox <- d:
								default:
									log.Printf("[EventBus] Dropped critical %s after inbox drain for %s", d.event.Type, name)
									accountForDelivery(d)
								}
							default:
								// Inbox closed (subscriber unsubscribed) or
								// already drained. Account for delivery so
								// Publish can return.
								accountForDelivery(d)
							}
						}
					}()
					continue
				}
				// Bare-channel path: subscriber was added by direct
				// manipulation (test setup or legacy code path), not via
				// Subscribe. forwardToReceive applies the critical
				// drain-replace policy directly to the receive channel.
				func() {
					defer func() { _ = recover() }()
					forwardToReceive(receives[i], d.event)
				}()
				// accountForDelivery here too: there is no worker that
				// will decrement the per-event counter for this subscriber.
				accountForDelivery(d)
			}
		}
	}
}

// accountForDelivery decrements the per-event subscriber counter and
// releases the publishWG when the last subscriber finishes. Safe to call
// from any goroutine. Both fields are nil-guarded for tests that build
// eventDelivery values manually.
func accountForDelivery(d eventDelivery) {
	if d.remainingSubs == nil {
		return
	}
	if atomic.AddInt32(d.remainingSubs, -1) == 0 && d.publishWG != nil {
		d.publishWG.Done()
	}
}

// coalesceBatch is a no-op stub kept so Publish's call site doesn't
// change. The SP-128 macOS freeze was caused by goroutine fan-out, not by
// per-event channel sends, and the worker-pool design eliminates that.
// Coalescing stream chunks is done at the WS writer
// (pkg/webui/stream_coalesce.go) when it drains its receive channel —
// that path coalesces many events that have already accumulated, which
// is the right place for the optimization.
func (eb *EventBus) coalesceBatch(first eventDelivery) []eventDelivery {
	return []eventDelivery{first}
}

// runWorker is the per-subscriber forwarder. It reads events from the
// inbox and pushes them onto the receive channel using the critical
// drain-replace policy. Each subscriber has exactly one of these, started
// at Subscribe time and stopped at Unsubscribe time. After forwarding
// (or deterministically dropping on a full inbox), the worker calls
// accountForDelivery so the originating Publish can return once every
// subscriber has had a chance to process the event.
func (eb *EventBus) runWorker(name string, receive chan UIEvent, w *subscriberWorker) {
	for {
		select {
		case <-w.done:
			// Drain any remaining inbox items, then close receive.
			for {
				select {
				case d := <-w.inbox:
					forwardToReceive(receive, d.event)
					accountForDelivery(d)
				default:
					close(receive)
					return
				}
			}
		case d := <-w.inbox:
			forwardToReceive(receive, d.event)
			accountForDelivery(d)
		}
	}
}

// forwardToReceive applies the critical-event drain-replace policy when
// pushing onto the receive channel. Recovers from send-on-closed-channel
// in case the receive was closed under us (e.g. consumer bailed out).
func forwardToReceive(ch chan UIEvent, ev UIEvent) {
	defer func() { _ = recover() }()
	if isCriticalEvent(ev.Type) {
		select {
		case ch <- ev:
		default:
			select {
			case <-ch:
				select {
				case ch <- ev:
				case <-time.After(1 * time.Second):
					log.Printf("[EventBus] Dropped critical %s event: subscriber unresponsive for 1s after drain", ev.Type)
				}
			default:
			}
		}
		return
	}
	select {
	case ch <- ev:
	default:
		log.Printf("[EventBus] Dropped %s event for slow subscriber (channel full, cap=%d)", ev.Type, subscriberBufferSize)
	}
}

// Publish broadcasts an event to all subscribers.
//
// Publish itself does NO goroutine creation: it queues one eventDelivery
// onto the shared delivery queue (non-blocking for non-critical, brief
// bounded retry for critical) and returns. All fan-out, coalescing, and
// per-subscriber forwarding happens in the persistent dispatcher +
// per-subscriber workers started at bus creation and Subscribe time.
//
// This eliminates the goroutine creation rate that caused the historical
// macOS streaming-freeze: hundreds of token-level chunks/sec no longer
// spawn hundreds of goroutines/sec.
func (eb *EventBus) Publish(eventType string, data any) {
	eb.mutex.Lock()
	eb.nextID++
	event := UIEvent{
		ID:        generateEventID(eb.nextID),
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}

	// publishWG preserves the historical synchronous-Publish contract:
	// Publish returns only after delivery has been attempted for every
	// subscriber. The WaitGroup is counted once per Publish, and the
	// LAST worker to process the event (across all subscribers) calls
	// wg.Done() so Publish can return.
	var wg sync.WaitGroup
	wg.Add(1)

	// Snapshot subscriber count under the same lock so the worker's
	// atomic decrement matches the actual fan-out count.
	var remaining int32
	if len(eb.subscribers) > 0 {
		remaining = int32(len(eb.subscribers))
	} else {
		// No subscribers: there's nothing to wait on; we'll call Done
		// below.
	}

	delivery := eventDelivery{
		event:         event,
		isCritical:    isCriticalEvent(eventType),
		remainingSubs: &remaining,
		publishWG:     &wg,
	}
	eb.mutex.Unlock()

	// Edge case: no subscribers. Publish still must return.
	if remaining == 0 {
		wg.Done()
		return
	}

	queued := false
	if delivery.isCritical {
		// Critical: try once, then briefly retry. We don't want to
		// silently lose a security prompt, but we also don't want to
		// block the model goroutine for the full 1-second drain.
		select {
		case eb.deliveryQueue <- delivery:
			queued = true
		default:
			select {
			case eb.deliveryQueue <- delivery:
				queued = true
			case <-time.After(2 * time.Millisecond):
				log.Printf("[EventBus] Dropped critical %s event: delivery queue full", eventType)
			}
		}
	} else {
		select {
		case eb.deliveryQueue <- delivery:
			queued = true
		default:
			log.Printf("[EventBus] Dropped %s event: delivery queue full (cap=%d)", eventType, sharedDeliveryQueueSize)
		}
	}

	if !queued {
		// Drop already accounted for in the log line above; release the
		// wait so Publish returns instead of hanging forever.
		wg.Done()
		return
	}
	wg.Wait()
}

// generateEventID creates a unique event ID
func generateEventID(id int64) string {
	return fmt.Sprintf("%s-%d", time.Now().Format("20060102-150405"), id)
}
