//go:build !js && !windows

package appcheck

import (
	"sync"
	"time"
)

// logSink collects a child's output into a string another goroutine reads.
// The server log is only read on a failure path and in the report, so it is
// guarded by a mutex rather than streamed.
type logSink struct {
	mu  sync.Mutex
	buf string
}

func (s *logSink) append(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf += text
}

func (s *logSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf
}

// stopTimeout bounds how long stopping a server waits before killing it
// outright: SIGTERM lets wrangler clean up, SIGKILL guarantees the port is
// freed.
const stopTimeout = 5 * time.Second
