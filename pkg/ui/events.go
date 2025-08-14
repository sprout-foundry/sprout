package ui

import (
	"fmt"
	"time"
)

// Event is a marker for all UI events.
type Event interface{}

// LogEvent represents a log line to display in the UI.
type LogEvent struct {
	Level string
	Text  string
	Time  time.Time
}

// ProgressRow represents a single agent row in the progress table
type ProgressRow struct {
	Name   string
	Status string
	Step   string
	Tokens int
	Cost   float64
}

// ProgressSnapshotEvent represents a full table snapshot
type ProgressSnapshotEvent struct {
	Completed int
	Total     int
	Rows      []ProgressRow
	Time      time.Time
}

var eventChan = make(chan Event, 2048)

// Events exposes a receive-only channel of events.
func Events() <-chan Event { return eventChan }

// Publish sends an event to the UI if possible (drops on full buffer).
func Publish(event Event) {
	select {
	case eventChan <- event:
	default:
		// drop if buffer is full to avoid blocking
	}
}

// Log publishes a plain info log line.
func Log(text string) { Publish(LogEvent{Level: "info", Text: text, Time: time.Now()}) }

// Logf publishes a formatted log line.
func Logf(format string, args ...any) { Log(fmt.Sprintf(format, args...)) }

// PublishProgress broadcasts a progress snapshot to the UI
func PublishProgress(completed, total int, rows []ProgressRow) {
	Publish(ProgressSnapshotEvent{
		Completed: completed,
		Total:     total,
		Rows:      rows,
		Time:      time.Now(),
	})
}
