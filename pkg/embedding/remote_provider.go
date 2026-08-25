//go:build !js

package embedding

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// Remote daemon socket protocol (SP-136 P3).
//
// The daemon owns the sole ONNX model copy, the sole writer per workspace
// index, and the inference gate. CLI processes route embedding operations to
// it over a JSON-over-Unix-socket protocol instead of loading their own
// 155MB model and racing on the same index files.
//
// Wire format: one JSON object per line. Each request carries an ID; the
// response echoes it so a connection can multiplex later. Ops:
//
//	{"id":"1","op":"meta"}                            → MetaResponse
//	{"id":"2","op":"embed","text":"..."}              → EmbedResponse
//	{"id":"3","op":"embed_batch","texts":[...]}       → EmbedResponse
//	{"id":"4","op":"query","text":"...","k":5,"threshold":0.5} → QueryResponse
//	{"id":"5","op":"build_index","workspace_root":"..."}       → BuildResponse
//	{"id":"6","op":"check_duplicates","file_path":"...","content":"..."} → DupResponse
//
// Errors are returned as {"id":"...","error":"..."} with no op payload.

// RemoteOp identifies a protocol operation.
type RemoteOp string

const (
	RemoteOpMeta            RemoteOp = "meta"
	RemoteOpEmbed           RemoteOp = "embed"
	RemoteOpEmbedBatch      RemoteOp = "embed_batch"
	RemoteOpQuery           RemoteOp = "query"
	RemoteOpBuildIndex      RemoteOp = "build_index"
	RemoteOpCheckDuplicates RemoteOp = "check_duplicates"
)

// RemoteRequest is a single protocol request.
type RemoteRequest struct {
	ID        string   `json:"id"`
	Op        RemoteOp `json:"op"`
	Text      string   `json:"text,omitempty"`
	Texts     []string `json:"texts,omitempty"`
	K         int      `json:"k,omitempty"`
	Threshold float32  `json:"threshold,omitempty"`
	Workspace string   `json:"workspace_root,omitempty"`
	FilePath  string   `json:"file_path,omitempty"`
	Content   string   `json:"content,omitempty"`
	TopK      int      `json:"top_k,omitempty"`
}

// RemoteResponse is a single protocol response.
type RemoteResponse struct {
	ID         string                 `json:"id"`
	Error      string                 `json:"error,omitempty"`
	Name       string                 `json:"name,omitempty"`
	Dimensions int                    `json:"dimensions,omitempty"`
	ModelHash  string                 `json:"model_hash,omitempty"`
	Vector     []float32              `json:"vector,omitempty"`
	Vectors    [][]float32            `json:"vectors,omitempty"`
	Results    []QueryResult          `json:"results,omitempty"`
	Stats      *IndexStats            `json:"stats,omitempty"`
	Duplicates *CheckDuplicatesResult `json:"duplicates,omitempty"`
}

// DefaultRemoteSocketTimeout bounds a single remote operation. Inference can
// be slow on a loaded daemon, but a stuck daemon must not hang the CLI.
const DefaultRemoteSocketTimeout = 60 * time.Second

// remoteConn wraps a single Unix-socket connection with a read/write lock.
// The protocol is request/response over one stream, so a mutex serializes
// ops per connection. Reconnect-on-error is handled by RemoteEmbeddingProvider.
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

// do sends one request and reads the matching response. Callers must hold
// c.mu. A request/response carry an ID so a future multiplexed connection can
// correlate; today the mutex makes it strictly sequential.
func (c *remoteConn) do(ctx context.Context, req RemoteRequest) (*RemoteResponse, error) {
	if req.ID == "" {
		req.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	// Enforce a deadline per operation so a wedged daemon fails the CLI
	// instead of hanging it (SP-136 P1 graceful degradation).
	deadline := time.Now().Add(DefaultRemoteSocketTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set socket deadline: %w", err)
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal remote request: %w", err)
	}
	if _, err := c.rw.Write(append(payload, '\n')); err != nil {
		return nil, fmt.Errorf("write remote request: %w", err)
	}
	if err := c.rw.Flush(); err != nil {
		return nil, fmt.Errorf("flush remote request: %w", err)
	}

	line, err := c.rw.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read remote response: %w", err)
	}
	var resp RemoteResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("decode remote response: %w", err)
	}
	if resp.ID != req.ID {
		return nil, fmt.Errorf("remote response ID mismatch: got %q want %q", resp.ID, req.ID)
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// RemoteEmbeddingProvider
// ---------------------------------------------------------------------------

// RemoteEmbeddingProvider implements EmbeddingProvider by proxying embedding
// operations to the sprout daemon over a Unix socket. The daemon owns the
// model copy and inference gate; this provider is a thin client.
//
// If the socket is unavailable at construction, NewRemoteEmbeddingProvider
// returns an error and callers fall back to in-process ONNX. If the socket
// dies later, operations fail with a descriptive error; the provider re-dials
// on the next operation (transient-failure reconnect).
type RemoteEmbeddingProvider struct {
	socketPath string
	conn       *remoteConn
	mu         sync.Mutex // guards conn

	dims      int
	name      string
	modelHash string
	metaOnce  sync.Once
	metaErr   error
}

// NewRemoteEmbeddingProvider creates a provider backed by the daemon socket
// at socketPath. It performs a meta handshake immediately so configuration
// errors surface at construction time.
func NewRemoteEmbeddingProvider(socketPath string) (*RemoteEmbeddingProvider, error) {
	p := &RemoteEmbeddingProvider{socketPath: socketPath}
	if err := p.ensureConn(); err != nil {
		return nil, err
	}
	if _, err := p.meta(context.Background()); err != nil {
		p.closeConn()
		return nil, fmt.Errorf("daemon meta handshake: %w", err)
	}
	return p, nil
}

// SocketPath returns the daemon socket path this provider talks to.
func (p *RemoteEmbeddingProvider) SocketPath() string {
	return p.socketPath
}

func (p *RemoteEmbeddingProvider) ensureConn() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		return nil
	}
	conn, err := dialRemote(p.socketPath)
	if err != nil {
		return err
	}
	p.conn = conn
	return nil
}

func (p *RemoteEmbeddingProvider) closeConn() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		_ = p.conn.close()
		p.conn = nil
	}
}

// doWithReconnect sends a request, re-dialing once if the connection is
// stale (daemon restarted, socket rotated).
func (p *RemoteEmbeddingProvider) doWithReconnect(ctx context.Context, req RemoteRequest) (*RemoteResponse, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if err := p.ensureConn(); err != nil {
			return nil, err
		}
		p.mu.Lock()
		resp, err := p.conn.do(ctx, req)
		p.mu.Unlock()
		if err == nil {
			return resp, nil
		}
		// Connection broken — drop it and retry once with a fresh dial.
		p.closeConn()
		if attempt == 0 {
			continue
		}
		return nil, err
	}
	return nil, errors.New("remote provider: unreachable")
}

// meta fetches daemon metadata (name, dimensions, model hash) once.
func (p *RemoteEmbeddingProvider) meta(ctx context.Context) (*RemoteResponse, error) {
	p.metaOnce.Do(func() {
		resp, err := p.doWithReconnect(ctx, RemoteRequest{Op: RemoteOpMeta})
		if err != nil {
			p.metaErr = err
			return
		}
		p.name = resp.Name
		p.dims = resp.Dimensions
		p.modelHash = resp.ModelHash
	})
	if p.metaErr != nil {
		return nil, p.metaErr
	}
	return &RemoteResponse{Name: p.name, Dimensions: p.dims, ModelHash: p.modelHash}, nil
}

// Embed implements EmbeddingProvider.
func (p *RemoteEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := p.doWithReconnect(ctx, RemoteRequest{Op: RemoteOpEmbed, Text: text})
	if err != nil {
		return nil, err
	}
	return resp.Vector, nil
}

// EmbedBatch implements EmbeddingProvider.
func (p *RemoteEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	resp, err := p.doWithReconnect(ctx, RemoteRequest{Op: RemoteOpEmbedBatch, Texts: texts})
	if err != nil {
		return nil, err
	}
	return resp.Vectors, nil
}

// Dimensions implements EmbeddingProvider.
func (p *RemoteEmbeddingProvider) Dimensions() int {
	_, _ = p.meta(context.Background())
	return p.dims
}

// Name implements EmbeddingProvider.
func (p *RemoteEmbeddingProvider) Name() string {
	_, _ = p.meta(context.Background())
	return p.name
}

// ModelHash implements EmbeddingProvider.
func (p *RemoteEmbeddingProvider) ModelHash() string {
	_, _ = p.meta(context.Background())
	return p.modelHash
}

// EmbedWithPrefix implements EmbeddingProvider by prepending the prefix
// client-side and delegating to Embed (the daemon tokenizes the final text).
func (p *RemoteEmbeddingProvider) EmbedWithPrefix(ctx context.Context, text string, prefix string) ([]float32, error) {
	return p.Embed(ctx, prefix+text)
}

// EmbedBatchWithPrefix implements EmbeddingProvider by prepending the prefix
// to each text client-side and delegating to EmbedBatch.
func (p *RemoteEmbeddingProvider) EmbedBatchWithPrefix(ctx context.Context, texts []string, prefix string) ([][]float32, error) {
	prefixed := make([]string, len(texts))
	for i, t := range texts {
		prefixed[i] = prefix + t
	}
	return p.EmbedBatch(ctx, prefixed)
}

// Close implements EmbeddingProvider.
func (p *RemoteEmbeddingProvider) Close() error {
	p.closeConn()
	return nil
}

// ---------------------------------------------------------------------------
// RemoteClient — full-operation client (query / build / duplicates)
// ---------------------------------------------------------------------------

// RemoteClient provides the non-provider operations of the daemon socket
// protocol: Query, BuildIndex, and CheckDuplicates. It shares the provider's
// connection so a CLI keeps a single socket.
type RemoteClient struct {
	provider *RemoteEmbeddingProvider
}

// NewRemoteClient creates a client over the given provider's connection.
func NewRemoteClient(p *RemoteEmbeddingProvider) *RemoteClient {
	return &RemoteClient{provider: p}
}

// QuerySimilar runs a semantic query on the daemon-owned index for the
// workspace root.
func (c *RemoteClient) QuerySimilar(ctx context.Context, workspaceRoot, text string, topK int, threshold float32) ([]QueryResult, error) {
	resp, err := c.provider.doWithReconnect(ctx, RemoteRequest{
		Op: RemoteOpQuery, Text: text, Workspace: workspaceRoot, K: topK, Threshold: threshold,
	})
	if err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// BuildIndex asks the daemon to (re)build the workspace index and returns
// the build stats.
func (c *RemoteClient) BuildIndex(ctx context.Context, workspaceRoot string) (*IndexStats, error) {
	resp, err := c.provider.doWithReconnect(ctx, RemoteRequest{
		Op: RemoteOpBuildIndex, Workspace: workspaceRoot,
	})
	if err != nil {
		return nil, err
	}
	return resp.Stats, nil
}

// CheckDuplicates asks the daemon to check file content against its index.
func (c *RemoteClient) CheckDuplicates(ctx context.Context, workspaceRoot, filePath, content string) (*CheckDuplicatesResult, error) {
	resp, err := c.provider.doWithReconnect(ctx, RemoteRequest{
		Op: RemoteOpCheckDuplicates, Workspace: workspaceRoot, FilePath: filePath, Content: content,
	})
	if err != nil {
		return nil, err
	}
	return resp.Duplicates, nil
}
