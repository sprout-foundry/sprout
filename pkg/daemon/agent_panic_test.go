//go:build !js

package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// panicAgentService implements AgentService and panics inside Query — for
// pinning the server's request-path panic recovery: a panicking request must
// kill only that request, never the daemon process.
type panicAgentService struct {
	*stubAgentService
}

func (p *panicAgentService) Query(context.Context, string, string, QueryOptions) (string, error) {
	panic("boom from panicAgentService")
}

func (p *panicAgentService) StreamQuery(context.Context, string, string, QueryOptions, func(StreamEvent) error) error {
	panic("boom from panicAgentService")
}

// TestAgentServer_PanicRecovers asserts a panic in the service layer is
// converted into an error response on the wire and the server stays alive
// for the next request.
func TestAgentServer_PanicRecovers(t *testing.T) {
	svc := &panicAgentService{stubAgentService: newStubAgentService()}
	sockPath := shortSocketPath(t, "agent-panic")
	srv := &AgentServer{SocketPath: sockPath, Service: svc}
	require.NoError(t, srv.Start(context.Background()))
	t.Cleanup(func() { srv.Close() })

	conn, err := net.Dial("unix", sockPath)
	require.NoError(t, err)
	defer conn.Close()

	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))

	req := AgentRequest{ID: "p2", Op: AgentOpQuery, Prompt: "x", WorkDir: "/tmp"}
	payload, err := json.Marshal(req)
	require.NoError(t, err)
	_, err = rw.Write(append(payload, '\n'))
	require.NoError(t, err)
	require.NoError(t, rw.Flush())

	line, err := rw.ReadBytes('\n')
	require.NoError(t, err, "the server must answer, not drop the connection")
	var resp AgentResponse
	require.NoError(t, json.Unmarshal(line, &resp))
	assert.Equal(t, "p2", resp.ID)
	assert.Contains(t, resp.Error, "panicked", "panic must surface as an error response")

	// Server must still be alive and serving.
	client, err := NewAgentClient(sockPath)
	require.NoError(t, err)
	defer client.Close()
	sessions, err := client.ListSessions(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, sessions)
}

// TestRequestOptions_NilSafe pins old-client compatibility: a request
// without an options block resolves to zero QueryOptions (daemon defaults),
// never a nil dereference.
func TestRequestOptions_NilSafe(t *testing.T) {
	var req AgentRequest
	require.NoError(t, json.Unmarshal([]byte(`{"id":"1","op":"query","prompt":"hi","work_dir":"/tmp"}`), &req))
	assert.Equal(t, QueryOptions{}, requestOptions(req))

	require.NoError(t, json.Unmarshal([]byte(`{"id":"2","op":"query","options":{"persona":"coder"}}`), &req))
	assert.Equal(t, "coder", requestOptions(req).Persona)
}
