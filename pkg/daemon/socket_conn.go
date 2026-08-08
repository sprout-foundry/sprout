//go:build !js

package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

// DefaultRemoteSocketTimeout bounds a single remote operation. Agent runs
// can take a while, but a stuck daemon must not hang the CLI.
const DefaultRemoteSocketTimeout = 10 * time.Minute

// remoteConn wraps a single Unix-socket connection with a read/write lock.
// The daemon protocols (embedding + agent) are request/response over one
// stream, so a mutex serializes ops per connection.
type remoteConn struct {
	mu   sync.Mutex
	conn net.Conn
	rw   *bufio.ReadWriter
}

func dialRemote(socketPath string) (*remoteConn, error) {
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial daemon socket %s: %w", socketPath, err)
	}
	return &remoteConn{
		conn: conn,
		rw:   bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)),
	}, nil
}

func (c *remoteConn) close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// doJSON sends one JSON request and reads the matching response, decoding
// into resp. Callers must hold c.mu. Enforces the context/operation deadline.
func (c *remoteConn) doJSON(ctx context.Context, reqID string, req any, resp any) error {
	deadline := time.Now().Add(DefaultRemoteSocketTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set socket deadline: %w", err)
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal remote request: %w", err)
	}
	if _, err := c.rw.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("write remote request: %w", err)
	}
	if err := c.rw.Flush(); err != nil {
		return fmt.Errorf("flush remote request: %w", err)
	}

	line, err := c.rw.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("read remote response: %w", err)
	}
	if err := json.Unmarshal(line, resp); err != nil {
		return fmt.Errorf("decode remote response: %w", err)
	}
	return nil
}
