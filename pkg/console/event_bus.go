package console

import (
	"fmt"
	"sync"
	"time"
)

// eventBus implements EventBus interface
type eventBus struct {
	mu            sync.RWMutex
	subscriptions map[string][]eventSubscription
	filter        EventFilter
	eventQueue    chan Event
	stopChan      chan struct{}
	running       bool
	idCounter     int
	queueSize     int
}

// eventSubscription holds handler info
type eventSubscription struct {
	id        string
	eventType string
	source    string // optional source filter
	handler   EventHandler
}

// NewEventBus creates a new event bus
func NewEventBus(queueSize int) EventBus {
	if queueSize <= 0 {
		queueSize = 100 // default queue size
	}

	return &eventBus{
		subscriptions: make(map[string][]eventSubscription),
		eventQueue:    make(chan Event, queueSize),
		stopChan:      make(chan struct{}),
		queueSize:     queueSize,
	}
}

// Start starts the event processing loop
func (eb *eventBus) Start() error {
	eb.mu.Lock()
	if eb.running {
		eb.mu.Unlock()
		return fmt.Errorf("event bus already running")
	}
	eb.running = true
	eb.mu.Unlock()

	// Start event processing goroutine
	go eb.processEvents()

	return nil
}

// Stop stops the event processing loop
func (eb *eventBus) Stop() error {
	eb.mu.Lock()
	if !eb.running {
		eb.mu.Unlock()
		return fmt.Errorf("event bus not running")
	}
	eb.running = false
	eb.mu.Unlock()

	// Signal stop
	close(eb.stopChan)

	// Drain remaining events
	close(eb.eventQueue)

	return nil
}

// Publish publishes an event synchronously
func (eb *eventBus) Publish(event Event) error {
	eb.mu.RLock()
	if !eb.running {
		eb.mu.RUnlock()
		return fmt.Errorf("event bus not running")
	}
	filter := eb.filter
	eb.mu.RUnlock()

	// Set timestamp if not set
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().UnixNano()
	}

	// Apply filter
	if filter != nil && !filter(event) {
		return nil // Event filtered out
	}

	// Process synchronously
	return eb.processEvent(event)
}

// PublishAsync publishes an event asynchronously
func (eb *eventBus) PublishAsync(event Event) {
	eb.mu.RLock()
	if !eb.running {
		eb.mu.RUnlock()
		return
	}
	filter := eb.filter
	eb.mu.RUnlock()

	// Set timestamp if not set
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().UnixNano()
	}

	// Apply filter
	if filter != nil && !filter(event) {
		return // Event filtered out
	}

	// Queue event (non-blocking)
	select {
	case eb.eventQueue <- event:
		// Queued successfully
	default:
		// Queue full, drop event
		// In production, might want to log this
	}
}

// Subscribe subscribes to events by type
func (eb *eventBus) Subscribe(eventType string, handler EventHandler) string {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.idCounter++
	id := fmt.Sprintf("sub_%d", eb.idCounter)

	sub := eventSubscription{
		id:        id,
		eventType: eventType,
		handler:   handler,
	}

	eb.subscriptions[eventType] = append(eb.subscriptions[eventType], sub)

	return id
}

// SubscribeToSource subscribes to events from a specific source
func (eb *eventBus) SubscribeToSource(source string, handler EventHandler) string {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.idCounter++
	id := fmt.Sprintf("sub_%d", eb.idCounter)

	sub := eventSubscription{
		id:        id,
		eventType: "*", // Listen to all event types
		source:    source,
		handler:   handler,
	}

	// Store under special key for source subscriptions
	key := "_source_" + source
	eb.subscriptions[key] = append(eb.subscriptions[key], sub)

	return id
}

// Unsubscribe removes a subscription
func (eb *eventBus) Unsubscribe(subscriptionID string) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	// Search all subscription lists
	for key, subs := range eb.subscriptions {
		for i, sub := range subs {
			if sub.id == subscriptionID {
				// Remove subscription
				eb.subscriptions[key] = append(subs[:i], subs[i+1:]...)

				// Clean up empty lists
				if len(eb.subscriptions[key]) == 0 {
					delete(eb.subscriptions, key)
				}
				return
			}
		}
	}
}

// SetFilter sets the event filter
func (eb *eventBus) SetFilter(filter EventFilter) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.filter = filter
}

// processEvents processes events from the queue
func (eb *eventBus) processEvents() {
	for {
		select {
		case event, ok := <-eb.eventQueue:
			if !ok {
				// Channel closed, stop processing
				return
			}
			// Process event (ignore errors in async processing)
			_ = eb.processEvent(event)

		case <-eb.stopChan:
			// Stop requested
			return
		}
	}
}

// processEvent processes a single event
func (eb *eventBus) processEvent(event Event) error {
	eb.mu.RLock()

	// Collect all matching handlers
	var handlers []EventHandler

	// Type-based subscriptions
	if subs, exists := eb.subscriptions[event.Type]; exists {
		for _, sub := range subs {
			// Check if source matches (if specified)
			if sub.source == "" || sub.source == event.Source {
				handlers = append(handlers, sub.handler)
			}
		}
	}

	// Wildcard subscriptions
	if subs, exists := eb.subscriptions["*"]; exists {
		for _, sub := range subs {
			// Check if source matches (if specified)
			if sub.source == "" || sub.source == event.Source {
				handlers = append(handlers, sub.handler)
			}
		}
	}

	// Source-based subscriptions
	sourceKey := "_source_" + event.Source
	if subs, exists := eb.subscriptions[sourceKey]; exists {
		for _, sub := range subs {
			handlers = append(handlers, sub.handler)
		}
	}

	eb.mu.RUnlock()

	// Call handlers outside of lock
	var firstErr error
	for _, handler := range handlers {
		if err := handler(event); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}
