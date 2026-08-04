package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
)

// One language-server process is shared by every client on the same
// workspace+language (see Manager.GetOrCreate). Each client speaks its own
// JSON-RPC id space starting at 1, so handing them a raw broadcast of the
// server's output breaks in two ways: two clients both send id 1 and each
// receives the other's reply, and the second client's `initialize` is rejected
// by servers that only accept one handshake (gopls answers -32600).
//
// Session isolates a client from its peers. Outbound request ids are rewritten
// into a process-wide id space and restored on the way back, responses are
// delivered only to the client that asked, and the handshake is performed once
// and replayed to everyone else.

const sessionChanBuffer = 256

type handshakeState int

const (
	handshakeNone handshakeState = iota
	handshakeInflight
	handshakeDone
)

// envelope is the minimal shape needed to classify a JSON-RPC message. It
// deliberately omits result/error so that large payloads aren't copied on
// every message just to work out where they should go.
type envelope struct {
	Method string          `json:"method"`
	ID     json.RawMessage `json:"id"`
}

func parseEnvelope(raw string) (envelope, bool) {
	var e envelope
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		return envelope{}, false
	}
	return e, true
}

func (e envelope) hasID() bool          { return len(e.ID) > 0 && string(e.ID) != "null" }
func (e envelope) isRequest() bool      { return e.Method != "" && e.hasID() }
func (e envelope) isNotification() bool { return e.Method != "" && !e.hasID() }

// withID returns raw with its top-level "id" replaced. Key order is not
// significant in JSON-RPC, so a decode/encode round-trip is safe here.
func withID(raw string, id json.RawMessage) (string, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return "", err
	}
	obj["id"] = id
	out, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func jsonInt(n int64) json.RawMessage {
	return json.RawMessage(strconv.FormatInt(n, 10))
}

func cloneRaw(r json.RawMessage) json.RawMessage {
	out := make(json.RawMessage, len(r))
	copy(out, r)
	return out
}

// hasRPCError reports whether a response carries an "error" member.
func hasRPCError(raw string) bool {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return false
	}
	e, ok := obj["error"]
	return ok && string(e) != "null"
}

// Session is one client's view of a shared LSP process.
type Session struct {
	proc *LSPProcess
	out  chan string

	mu       sync.Mutex
	closed   bool
	upstream map[string]json.RawMessage // client id → upstream id, for $/cancelRequest
}

// Out returns the channel of messages destined for this client. It is closed
// when the session or the underlying process shuts down.
func (s *Session) Out() <-chan string { return s.out }

func (s *Session) deliver(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.out <- msg:
	default:
		log.Printf("LSP session: dropping message for slow client")
	}
}

func (s *Session) remember(clientID, up json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.upstream == nil {
		s.upstream = make(map[string]json.RawMessage)
	}
	s.upstream[string(clientID)] = up
}

func (s *Session) forget(clientID json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.upstream, string(clientID))
}

func (s *Session) lookupUpstream(clientID json.RawMessage) (json.RawMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	up, ok := s.upstream[string(clientID)]
	return up, ok
}

// Close detaches the session from the process. Safe to call more than once.
func (s *Session) Close() {
	s.proc.registry.remove(s)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.out)
}

// Send forwards a client message to the shared process, translating ids and
// absorbing the messages that must not reach a server other clients depend on.
func (s *Session) Send(raw string) error {
	env, ok := parseEnvelope(raw)
	if !ok {
		return s.proc.Send(raw)
	}
	reg := s.proc.registry

	switch {
	case env.isRequest():
		switch env.Method {
		case "initialize":
			return reg.handleInitialize(s, env.ID, raw)
		case "shutdown":
			// Shared process: one client leaving must not shut the server down
			// for the others. Answer locally so the client's teardown completes.
			s.deliver(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":null}`, env.ID))
			return nil
		}
		up := reg.registerRequest(s, env.ID)
		s.remember(env.ID, up)
		out, err := withID(raw, up)
		if err != nil {
			return s.proc.Send(raw)
		}
		return s.proc.Send(out)

	case env.isNotification():
		switch env.Method {
		case "initialized":
			if !reg.claimInitialized() {
				return nil
			}
		case "exit":
			return nil
		case "$/cancelRequest":
			out, ok := s.rewriteCancel(raw)
			if !ok {
				return nil
			}
			return s.proc.Send(out)
		}
		return s.proc.Send(raw)
	}

	// A response to a server-initiated request. Those ids are never rewritten
	// (only the primary session ever receives such a request), so pass through.
	return s.proc.Send(raw)
}

// rewriteCancel maps the client-space request id inside a $/cancelRequest onto
// the upstream id the server actually knows about.
func (s *Session) rewriteCancel(raw string) (string, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return "", false
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(obj["params"], &params); err != nil {
		return "", false
	}
	up, ok := s.lookupUpstream(params["id"])
	if !ok {
		return "", false
	}
	params["id"] = up
	encodedParams, err := json.Marshal(params)
	if err != nil {
		return "", false
	}
	obj["params"] = encodedParams
	out, err := json.Marshal(obj)
	if err != nil {
		return "", false
	}
	return string(out), true
}

// pendingRequest records who is waiting on an upstream request id.
type pendingRequest struct {
	sess     *Session
	clientID json.RawMessage
}

// sessionRegistry owns the shared id space and handshake state for one process.
type sessionRegistry struct {
	mu       sync.Mutex
	sessions []*Session
	nextID   int64
	pending  map[int64]pendingRequest

	handshake       handshakeState
	initUpstreamID  int64
	initResponse    string
	initWaiters     []pendingRequest
	initializedSent bool
}

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{pending: make(map[int64]pendingRequest)}
}

func (r *sessionRegistry) add(s *Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions = append(r.sessions, s)
}

func (r *sessionRegistry) remove(s *Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, existing := range r.sessions {
		if existing == s {
			r.sessions = append(r.sessions[:i], r.sessions[i+1:]...)
			break
		}
	}
	for id, pend := range r.pending {
		if pend.sess == s {
			delete(r.pending, id)
		}
	}
}

func (r *sessionRegistry) registerRequest(s *Session, clientID json.RawMessage) json.RawMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	id := r.nextID
	r.pending[id] = pendingRequest{sess: s, clientID: cloneRaw(clientID)}
	return jsonInt(id)
}

// claimInitialized reports whether this caller is the one that gets to forward
// the `initialized` notification; every later one is swallowed.
func (r *sessionRegistry) claimInitialized() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.initializedSent {
		return false
	}
	r.initializedSent = true
	return true
}

// handleInitialize performs the handshake once per process and replays the
// result to every other session.
func (r *sessionRegistry) handleInitialize(s *Session, clientID json.RawMessage, raw string) error {
	r.mu.Lock()
	switch r.handshake {
	case handshakeDone:
		cached := r.initResponse
		r.mu.Unlock()
		out, err := withID(cached, clientID)
		if err != nil {
			return err
		}
		s.deliver(out)
		return nil

	case handshakeInflight:
		r.initWaiters = append(r.initWaiters, pendingRequest{sess: s, clientID: cloneRaw(clientID)})
		r.mu.Unlock()
		return nil

	default:
		r.handshake = handshakeInflight
		r.nextID++
		id := r.nextID
		r.initUpstreamID = id
		r.pending[id] = pendingRequest{sess: s, clientID: cloneRaw(clientID)}
		r.mu.Unlock()

		out, err := withID(raw, jsonInt(id))
		if err != nil {
			return err
		}
		return s.proc.Send(out)
	}
}

// route dispatches one server message to the session(s) that should see it.
func (r *sessionRegistry) route(raw string) {
	env, ok := parseEnvelope(raw)
	if !ok {
		r.broadcast(raw)
		return
	}

	switch {
	case env.isNotification():
		// Diagnostics, progress and log messages are for everyone.
		r.broadcast(raw)
	case env.isRequest():
		// A server-initiated request must be answered exactly once, so it goes
		// to a single session rather than to all of them.
		if p := r.primary(); p != nil {
			p.deliver(raw)
		}
	default:
		r.routeResponse(env, raw)
	}
}

func (r *sessionRegistry) routeResponse(env envelope, raw string) {
	var up int64
	if err := json.Unmarshal(env.ID, &up); err != nil {
		// Not an id this registry minted; nothing to restore it to.
		r.broadcast(raw)
		return
	}

	r.mu.Lock()
	pend, found := r.pending[up]
	if !found {
		r.mu.Unlock()
		log.Printf("LSP session: response for unknown request id %d", up)
		return
	}
	delete(r.pending, up)

	var waiters []pendingRequest
	if r.handshake == handshakeInflight && up == r.initUpstreamID {
		if hasRPCError(raw) {
			// Let the next session retry the handshake rather than caching a failure.
			r.handshake = handshakeNone
		} else {
			r.initResponse = raw
			r.handshake = handshakeDone
		}
		waiters = r.initWaiters
		r.initWaiters = nil
	}
	r.mu.Unlock()

	pend.sess.forget(pend.clientID)
	r.deliverWithID(pend, raw)
	for _, w := range waiters {
		r.deliverWithID(w, raw)
	}
}

func (r *sessionRegistry) deliverWithID(pend pendingRequest, raw string) {
	out, err := withID(raw, pend.clientID)
	if err != nil {
		pend.sess.deliver(raw)
		return
	}
	pend.sess.deliver(out)
}

func (r *sessionRegistry) primary() *Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.sessions) == 0 {
		return nil
	}
	return r.sessions[0]
}

func (r *sessionRegistry) broadcast(raw string) {
	r.mu.Lock()
	sessions := make([]*Session, len(r.sessions))
	copy(sessions, r.sessions)
	r.mu.Unlock()

	for _, s := range sessions {
		s.deliver(raw)
	}
}

// closeAll tears down every session, used when the process itself exits.
func (r *sessionRegistry) closeAll() {
	r.mu.Lock()
	sessions := r.sessions
	r.sessions = nil
	r.pending = make(map[int64]pendingRequest)
	r.initWaiters = nil
	r.mu.Unlock()

	for _, s := range sessions {
		s.mu.Lock()
		if !s.closed {
			s.closed = true
			close(s.out)
		}
		s.mu.Unlock()
	}
}
