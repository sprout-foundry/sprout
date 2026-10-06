// Package relay carries HTTP exchanges over one WebSocket between the
// platform and a runner behind NAT (platform SP-BUILDER-13, sprout SP-159).
//
// The runner dials the platform; the platform then opens one stream per
// HTTP request and the runner serves it. Streams are correlated by id, so
// many run concurrently without head-of-line blocking, and each direction of
// each stream has a credit window so a large transfer cannot starve an
// interactive call or grow memory without bound.
//
// Frames are JSON text messages:
//
//	{"t":"open","id":"1","ws":"<workspace>","method":"POST","path":"/api/txn/run","hdr":{…}}
//	{"t":"head","id":"1","status":200,"hdr":{…}}
//	{"t":"data","id":"1","b64":"<chunk>"}    (≤ ChunkSize bytes, either direction)
//	{"t":"ack","id":"1","n":65536}           (credit returned for consumed bytes)
//	{"t":"end","id":"1","err":""}            (direction finished; err non-empty resets)
//
// This file is kept identical in the sprout and platform repositories; the
// wire format is pinned in the platform's docs/runners/PROTOCOL.md.
package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// ChunkSize bounds one data frame's payload.
	ChunkSize = 64 << 10
	// Window is the unacknowledged bytes a sender may have in flight per
	// stream direction.
	Window = 4 << 20
	// queueFrames holds a full window of chunks, so a sender that respects
	// credit never blocks the read loop.
	queueFrames = Window/ChunkSize + 4

	pingInterval = 25 * time.Second
	readTimeout  = 75 * time.Second
	writeTimeout = 30 * time.Second
)

// ErrClosed is returned for streams on a mux whose connection has ended.
var ErrClosed = errors.New("relay: connection closed")

type frame struct {
	T      string              `json:"t"`
	ID     string              `json:"id"`
	WS     string              `json:"ws,omitempty"`
	Method string              `json:"method,omitempty"`
	Path   string              `json:"path,omitempty"`
	Hdr    map[string][]string `json:"hdr,omitempty"`
	Status int                 `json:"status,omitempty"`
	B64    []byte              `json:"b64,omitempty"`
	N      int                 `json:"n,omitempty"`
	Err    string              `json:"err,omitempty"`
}

// Handler serves one relayed request for workspace ws.
type Handler func(ws string, w http.ResponseWriter, r *http.Request)

// Mux multiplexes streams over one WebSocket. Either side may open streams;
// in practice the platform opens and the runner serves.
type Mux struct {
	conn    *websocket.Conn
	handler Handler

	writeCh chan frame
	done    chan struct{}
	once    sync.Once
	err     error

	mu      sync.Mutex
	streams map[string]*stream
	nextID  atomic.Uint64
}

// NewMux wraps conn. handler serves streams the peer opens; nil refuses them.
func NewMux(conn *websocket.Conn, handler Handler) *Mux {
	return &Mux{
		conn:    conn,
		handler: handler,
		writeCh: make(chan frame, 64),
		done:    make(chan struct{}),
		streams: make(map[string]*stream),
	}
}

// Done is closed when the connection ends.
func (m *Mux) Done() <-chan struct{} { return m.done }

// Serve runs the mux until the connection ends or ctx is cancelled.
func (m *Mux) Serve(ctx context.Context) error {
	go m.writeLoop()
	go func() {
		select {
		case <-ctx.Done():
			m.close(ctx.Err())
		case <-m.done:
		}
	}()
	m.conn.SetReadLimit(2*ChunkSize + 64<<10)
	_ = m.conn.SetReadDeadline(time.Now().Add(readTimeout))
	m.conn.SetPongHandler(func(string) error {
		return m.conn.SetReadDeadline(time.Now().Add(readTimeout))
	})
	for {
		var f frame
		if err := m.conn.ReadJSON(&f); err != nil {
			m.close(err)
			return m.err
		}
		_ = m.conn.SetReadDeadline(time.Now().Add(readTimeout))
		m.dispatch(ctx, f)
	}
}

// Close ends the connection and every stream on it.
func (m *Mux) Close() { m.close(ErrClosed) }

func (m *Mux) close(err error) {
	m.once.Do(func() {
		m.err = err
		close(m.done)
		_ = m.conn.Close()
		m.mu.Lock()
		all := m.streams
		m.streams = map[string]*stream{}
		m.mu.Unlock()
		for _, s := range all {
			s.fail(ErrClosed)
		}
	})
}

func (m *Mux) writeLoop() {
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	for {
		select {
		case <-m.done:
			return
		case f := <-m.writeCh:
			_ = m.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := m.conn.WriteJSON(f); err != nil {
				m.close(err)
				return
			}
		case <-ping.C:
			if err := m.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeTimeout)); err != nil {
				m.close(err)
				return
			}
		}
	}
}

func (m *Mux) send(f frame) error {
	select {
	case m.writeCh <- f:
		return nil
	case <-m.done:
		return ErrClosed
	}
}

func (m *Mux) lookup(id string) *stream {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streams[id]
}

func (m *Mux) register(s *stream) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streams[s.id] = s
}

func (m *Mux) forget(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.streams, id)
}

func (m *Mux) dispatch(ctx context.Context, f frame) {
	if f.T == "open" {
		m.serveStream(ctx, f)
		return
	}
	s := m.lookup(f.ID)
	if s == nil {
		return
	}
	switch f.T {
	case "head":
		select {
		case s.head <- f:
		default:
		}
	case "data":
		if !s.deliver(f.B64) {
			// The peer overran its credit: a protocol violation.
			_ = m.send(frame{T: "end", ID: s.id, Err: "window exceeded"})
			s.fail(errors.New("relay: peer exceeded flow-control window"))
			m.forget(s.id)
		}
	case "ack":
		s.credit.add(f.N)
	case "end":
		if f.Err != "" {
			s.fail(fmt.Errorf("relay: stream reset by peer: %s", f.Err))
			m.forget(s.id)
			return
		}
		s.finishIn()
	}
}

// stream is one exchange. "in" is what the peer sends us; "out" is what we
// send, limited by the credit the peer grants.
type stream struct {
	id      string
	mux     *Mux
	head    chan frame
	in      chan []byte
	inMu    sync.Mutex
	inDone  bool
	inErr   error
	buf     []byte
	credit  *credit
	cancel  context.CancelFunc
	failed  chan struct{}
	failOne sync.Once
}

func newStream(m *Mux, id string) *stream {
	return &stream{
		id: id, mux: m,
		head:   make(chan frame, 1),
		in:     make(chan []byte, queueFrames),
		credit: newCredit(Window),
		failed: make(chan struct{}),
	}
}

func (s *stream) deliver(p []byte) bool {
	s.inMu.Lock()
	defer s.inMu.Unlock()
	if s.inDone {
		return true
	}
	select {
	case s.in <- p:
		return true
	default:
		return false
	}
}

func (s *stream) finishIn() {
	s.inMu.Lock()
	defer s.inMu.Unlock()
	if !s.inDone {
		s.inDone = true
		close(s.in)
	}
}

func (s *stream) fail(err error) {
	s.inMu.Lock()
	if s.inErr == nil {
		s.inErr = err
	}
	s.inMu.Unlock()
	s.finishIn()
	s.credit.fail()
	s.failOne.Do(func() { close(s.failed) })
	if s.cancel != nil {
		s.cancel()
	}
}

// Read consumes the peer's data, returning credit as it goes.
func (s *stream) Read(p []byte) (int, error) {
	if len(s.buf) == 0 {
		chunk, ok := <-s.in
		if !ok {
			s.inMu.Lock()
			err := s.inErr
			s.inMu.Unlock()
			if err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		s.buf = chunk
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	_ = s.mux.send(frame{T: "ack", ID: s.id, N: n})
	return n, nil
}

// write sends p as data frames, waiting for credit.
func (s *stream) write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		n := len(p)
		if n > ChunkSize {
			n = ChunkSize
		}
		if err := s.credit.take(n); err != nil {
			return written, err
		}
		chunk := append([]byte(nil), p[:n]...)
		if err := s.mux.send(frame{T: "data", ID: s.id, B64: chunk}); err != nil {
			return written, err
		}
		written += n
		p = p[n:]
	}
	return written, nil
}

// credit is a byte budget that blocks takers until the peer acks.
type credit struct {
	mu     sync.Mutex
	cond   *sync.Cond
	avail  int
	failed bool
}

func newCredit(n int) *credit {
	c := &credit{avail: n}
	c.cond = sync.NewCond(&c.mu)
	return c
}

func (c *credit) take(n int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for c.avail < n && !c.failed {
		c.cond.Wait()
	}
	if c.failed {
		return ErrClosed
	}
	c.avail -= n
	return nil
}

func (c *credit) add(n int) {
	c.mu.Lock()
	c.avail += n
	c.mu.Unlock()
	c.cond.Broadcast()
}

func (c *credit) fail() {
	c.mu.Lock()
	c.failed = true
	c.mu.Unlock()
	c.cond.Broadcast()
}

// RoundTripper returns a transport that sends requests to workspace ws
// through the peer. The request URL's path and query are forwarded; scheme
// and host are ignored.
func (m *Mux) RoundTripper(ws string) http.RoundTripper {
	return roundTripper{m: m, ws: ws}
}

type roundTripper struct {
	m  *Mux
	ws string
}

func (rt roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	m := rt.m
	s := newStream(m, "p"+strconv.FormatUint(m.nextID.Add(1), 10))
	m.register(s)
	if err := m.send(frame{T: "open", ID: s.id, WS: rt.ws, Method: req.Method, Path: req.URL.RequestURI(), Hdr: req.Header.Clone()}); err != nil {
		m.forget(s.id)
		return nil, err
	}
	go func() {
		var err error
		if req.Body != nil {
			_, err = io.Copy(writerFunc(s.write), req.Body)
			_ = req.Body.Close()
		}
		f := frame{T: "end", ID: s.id}
		if err != nil {
			f.Err = err.Error()
		}
		_ = m.send(f)
	}()

	select {
	case h := <-s.head:
		resp := &http.Response{
			Status:     strconv.Itoa(h.Status) + " " + http.StatusText(h.Status),
			StatusCode: h.Status,
			Proto:      "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
			Header:        http.Header(h.Hdr),
			Body:          &responseBody{s: s},
			ContentLength: -1,
			Request:       req,
		}
		if resp.Header == nil {
			resp.Header = http.Header{}
		}
		return resp, nil
	case <-s.failed:
		m.forget(s.id)
		s.inMu.Lock()
		err := s.inErr
		s.inMu.Unlock()
		return nil, err
	case <-req.Context().Done():
		_ = m.send(frame{T: "end", ID: s.id, Err: "canceled"})
		m.forget(s.id)
		return nil, req.Context().Err()
	case <-m.done:
		return nil, ErrClosed
	}
}

type responseBody struct {
	s      *stream
	closed atomic.Bool
}

func (b *responseBody) Read(p []byte) (int, error) { return b.s.Read(p) }

func (b *responseBody) Close() error {
	if b.closed.Swap(true) {
		return nil
	}
	b.s.inMu.Lock()
	finished := b.s.inDone
	b.s.inMu.Unlock()
	if !finished {
		_ = b.s.mux.send(frame{T: "end", ID: b.s.id, Err: "closed"})
	}
	b.s.mux.forget(b.s.id)
	return nil
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// serveStream handles a stream the peer opened.
func (m *Mux) serveStream(parent context.Context, open frame) {
	s := newStream(m, open.ID)
	if m.handler == nil || open.ID == "" {
		_ = m.send(frame{T: "end", ID: open.ID, Err: "no handler"})
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	m.register(s)
	go func() {
		defer cancel()
		defer m.forget(s.id)
		req, err := http.NewRequestWithContext(ctx, open.Method, "http://relay"+open.Path, io.NopCloser(s))
		if err != nil {
			_ = m.send(frame{T: "end", ID: s.id, Err: "bad request"})
			return
		}
		req.Header = http.Header(open.Hdr)
		if req.Header == nil {
			req.Header = http.Header{}
		}
		req.ContentLength = -1
		w := &responseWriter{s: s, header: http.Header{}}
		m.handler(open.WS, w, req)
		w.WriteHeader(http.StatusOK)
		_ = m.send(frame{T: "end", ID: s.id})
	}()
}

type responseWriter struct {
	s       *stream
	header  http.Header
	started bool
}

func (w *responseWriter) Header() http.Header { return w.header }

func (w *responseWriter) WriteHeader(code int) {
	if w.started {
		return
	}
	w.started = true
	_ = w.s.mux.send(frame{T: "head", ID: w.s.id, Status: code, Hdr: w.header.Clone()})
}

func (w *responseWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.s.write(p)
}

// Flush satisfies http.Flusher; frames are sent as written.
func (w *responseWriter) Flush() {}
