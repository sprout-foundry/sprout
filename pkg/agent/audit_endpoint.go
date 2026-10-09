//go:build !js

package agent

import (
	"bytes"
	"net/http"
	"sync"
	"time"
)

// auditForwarder batches audit events and POSTs them to a host audit endpoint
// from a single background goroutine. It is best-effort: enqueue never blocks
// the caller (a full queue drops the oldest event), and a failed batch is
// retried a bounded number of times with backoff before being dropped, so a
// slow or unreachable endpoint can never stall a turn.
type auditForwarder struct {
	endpoint string
	batch    int
	interval time.Duration
	client   *http.Client

	mu       sync.Mutex
	queue    [][]byte
	wake     chan struct{}
	done     chan struct{}
	finished chan struct{}
	closed   bool
}

const (
	defaultAuditBatchSize     = 32
	defaultAuditFlushInterval = 5 * time.Second
	auditQueueCap             = 1024
	auditMaxRetries           = 3
	auditRetryBaseDelay       = 500 * time.Millisecond
	auditRequestTimeout       = 10 * time.Second
)

func newAuditForwarder(endpoint string, batchSize, flushIntervalSeconds int) *auditForwarder {
	if batchSize <= 0 {
		batchSize = defaultAuditBatchSize
	}
	interval := defaultAuditFlushInterval
	if flushIntervalSeconds > 0 {
		interval = time.Duration(flushIntervalSeconds) * time.Second
	}
	f := &auditForwarder{
		endpoint: endpoint,
		batch:    batchSize,
		interval: interval,
		client:   &http.Client{Timeout: auditRequestTimeout},
		wake:     make(chan struct{}, 1),
		done:     make(chan struct{}),
		finished: make(chan struct{}),
	}
	go f.run()
	return f
}

// Enqueue adds one event to the queue without blocking. When the queue is full
// the oldest event is dropped to make room — the audit trail stays bounded and
// a stalled endpoint never grows memory without limit.
func (f *auditForwarder) Enqueue(data []byte) {
	if f == nil {
		return
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return
	}
	if len(f.queue) >= auditQueueCap {
		f.queue = f.queue[1:]
	}
	f.queue = append(f.queue, data)
	full := len(f.queue) >= f.batch
	f.mu.Unlock()
	if full {
		f.signal()
	}
}

func (f *auditForwarder) signal() {
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

func (f *auditForwarder) run() {
	defer close(f.finished)
	ticker := time.NewTicker(f.interval)
	defer ticker.Stop()
	for {
		select {
		case <-f.done:
			f.flushRemaining()
			return
		case <-ticker.C:
			f.flush()
		case <-f.wake:
			f.flush()
		}
	}
}

// flush sends every complete batch currently queued.
func (f *auditForwarder) flush() {
	for {
		batch := f.takeBatch()
		if batch == nil {
			return
		}
		f.send(batch)
	}
}

func (f *auditForwarder) takeBatch() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queue) == 0 {
		return nil
	}
	n := f.batch
	if n > len(f.queue) {
		n = len(f.queue)
	}
	batch := f.queue[:n]
	f.queue = f.queue[n:]
	return batch
}

// flushRemaining drains the queue on shutdown.
func (f *auditForwarder) flushRemaining() {
	for {
		batch := f.takeBatch()
		if batch == nil {
			return
		}
		f.send(batch)
	}
}

// send POSTs one batch as a JSON array, retrying a bounded number of times.
// The backoff wait is cancellable by Close so shutdown never blocks on a
// stalled endpoint.
func (f *auditForwarder) send(batch [][]byte) {
	payload := buildAuditBatchPayload(batch)
	for attempt := 0; attempt <= auditMaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-f.done:
				return
			case <-time.After(auditRetryBaseDelay << (attempt - 1)):
			}
		}
		if f.post(payload) {
			return
		}
	}
}

func (f *auditForwarder) post(payload []byte) bool {
	req, err := http.NewRequest(http.MethodPost, f.endpoint, bytes.NewReader(payload))
	if err != nil {
		return true // a malformed endpoint never succeeds; drop rather than spin
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// Close stops the background goroutine, flushing what remains. It waits for
// the goroutine to exit, so a caller that returns from Close knows the final
// flush attempt has finished.
func (f *auditForwarder) Close() {
	if f == nil {
		return
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return
	}
	f.closed = true
	f.mu.Unlock()
	close(f.done)
	<-f.finished
}

// buildAuditBatchPayload wraps the queued event objects into a single JSON
// array, preserving the events verbatim.
func buildAuditBatchPayload(batch [][]byte) []byte {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, ev := range batch {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(ev)
	}
	buf.WriteByte(']')
	return buf.Bytes()
}
