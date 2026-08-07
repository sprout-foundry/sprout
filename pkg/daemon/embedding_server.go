//go:build !js

package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// EmbeddingService is the daemon-side capability the socket protocol serves.
// Implemented by EmbeddingManagerService, which adapts the daemon's shared
// EmbeddingManager (sole model copy, sole index writer, inference gate).
type EmbeddingService interface {
	// Meta returns provider identity (name, dimensions, model hash).
	Meta(ctx context.Context) (name string, dims int, modelHash string, err error)
	// Embed returns a vector for one text.
	Embed(ctx context.Context, text string) ([]float32, error)
	// EmbedBatch returns vectors for many texts (same order).
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	// QuerySimilar queries the index for workspaceRoot.
	QuerySimilar(ctx context.Context, workspaceRoot, text string, topK int, threshold float32) ([]embedding.QueryResult, error)
	// BuildIndex builds the index for workspaceRoot.
	BuildIndex(ctx context.Context, workspaceRoot string) (*embedding.IndexStats, error)
	// CheckDuplicates checks content against the workspaceRoot index.
	CheckDuplicates(ctx context.Context, workspaceRoot, filePath, content string) (*embedding.CheckDuplicatesResult, error)
}

// EmbeddingServer serves the SP-136 P3 JSON-over-Unix-socket embedding
// protocol. The daemon owns the sole model copy and the sole writer per
// workspace index; CLI processes talk to this server instead of loading
// their own model.
type EmbeddingServer struct {
	// SocketPath is the Unix socket path to listen on.
	SocketPath string
	// Service backs all operations.
	Service EmbeddingService
	// Logger receives request/error logs.
	Logger *slog.Logger

	ln   net.Listener
	mu   sync.Mutex
	conns map[net.Conn]struct{}
	done chan struct{}
	once sync.Once
}

// Start begins listening and serving. It returns once the listener is bound
// (not when serving stops — use Wait). Callers should call Close to stop.
func (s *EmbeddingServer) Start(ctx context.Context) error {
	if s.SocketPath == "" {
		return errors.New("embedding server: empty socket path")
	}
	if s.Service == nil {
		return errors.New("embedding server: nil service")
	}
	if s.Logger == nil {
		s.Logger = slog.Default()
	}

	if err := os.MkdirAll(filepath.Dir(s.SocketPath), 0o700); err != nil {
		return fmt.Errorf("embedding server: create socket dir: %w", err)
	}
	// Remove a stale socket file left by a crashed daemon.
	_ = os.Remove(s.SocketPath)

	ln, err := net.Listen("unix", s.SocketPath)
	if err != nil {
		return fmt.Errorf("embedding server: listen %s: %w", s.SocketPath, err)
	}
	// Owner-only access: the socket carries embedding data for the user's
	// workspaces. (SP-136 P2 auth requirement.)
	if err := os.Chmod(s.SocketPath, 0o600); err != nil {
		ln.Close()
		return fmt.Errorf("embedding server: chmod socket: %w", err)
	}

	s.ln = ln
	s.conns = make(map[net.Conn]struct{})
	s.done = make(chan struct{})

	go s.acceptLoop(ctx)
	s.Logger.Info("embedding socket server started", slog.String("socket", s.SocketPath))
	return nil
}

func (s *EmbeddingServer) acceptLoop(ctx context.Context) {
	defer close(s.done)
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if isClosedErr(err) {
				return
			}
			s.Logger.Warn("embedding server accept error", slog.Any("err", err))
			continue
		}
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		go s.handleConn(ctx, conn)
	}
}

func (s *EmbeddingServer) handleConn(ctx context.Context, conn net.Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		conn.Close()
	}()

	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	for {
		line, err := rw.ReadBytes('\n')
		if err != nil {
			return // client closed or socket error — normal
		}
		var req embedding.RemoteRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeError(rw, "", fmt.Sprintf("malformed request: %v", err))
			continue
		}

		resp := s.dispatch(ctx, req)
		payload, err := json.Marshal(resp)
		if err != nil {
			s.writeError(rw, req.ID, fmt.Sprintf("marshal response: %v", err))
			continue
		}
		if _, err := rw.Write(append(payload, '\n')); err != nil {
			return
		}
		if err := rw.Flush(); err != nil {
			return
		}
	}
}

func (s *EmbeddingServer) dispatch(ctx context.Context, req embedding.RemoteRequest) embedding.RemoteResponse {
	resp := embedding.RemoteResponse{ID: req.ID}

	switch req.Op {
	case embedding.RemoteOpMeta:
		name, dims, hash, err := s.Service.Meta(ctx)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.Name = name
		resp.Dimensions = dims
		resp.ModelHash = hash

	case embedding.RemoteOpEmbed:
		vec, err := s.Service.Embed(ctx, req.Text)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.Vector = vec

	case embedding.RemoteOpEmbedBatch:
		vecs, err := s.Service.EmbedBatch(ctx, req.Texts)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.Vectors = vecs

	case embedding.RemoteOpQuery:
		results, err := s.Service.QuerySimilar(ctx, req.Workspace, req.Text, req.K, req.Threshold)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.Results = results

	case embedding.RemoteOpBuildIndex:
		stats, err := s.Service.BuildIndex(ctx, req.Workspace)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.Stats = stats

	case embedding.RemoteOpCheckDuplicates:
		dups, err := s.Service.CheckDuplicates(ctx, req.Workspace, req.FilePath, req.Content)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.Duplicates = dups

	default:
		resp.Error = fmt.Sprintf("unknown op %q", req.Op)
	}
	return resp
}

func (s *EmbeddingServer) writeError(rw *bufio.ReadWriter, id, errMsg string) {
	payload, _ := json.Marshal(embedding.RemoteResponse{ID: id, Error: errMsg})
	_, _ = rw.Write(append(payload, '\n'))
	_ = rw.Flush()
}

// Wait blocks until the server has stopped accepting (after Close or ctx
// cancellation).
func (s *EmbeddingServer) Wait() {
	if s.done != nil {
		<-s.done
	}
}

// Close stops the server, closes the listener, and drops all client
// connections.
func (s *EmbeddingServer) Close() error {
	var firstErr error
	s.once.Do(func() {
		if s.ln != nil {
			firstErr = s.ln.Close()
			_ = os.Remove(s.SocketPath)
		}
		s.mu.Lock()
		for conn := range s.conns {
			_ = conn.Close()
		}
		s.conns = nil
		s.mu.Unlock()
	})
	return firstErr
}

func isClosedErr(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, os.ErrClosed) ||
		(err != nil && err.Error() == "use of closed network connection")
}

// ---------------------------------------------------------------------------
// EmbeddingManagerService — adapts an EmbeddingManager to EmbeddingService
// ---------------------------------------------------------------------------

// EmbeddingManagerService adapts a shared *embedding.EmbeddingManager to the
// socket protocol. Manager acquisition is workspace-scoped: each workspace
// root resolves to the daemon's shared manager (sole writer per index).
type EmbeddingManagerService struct {
	// Acquire returns a manager for the workspace root. The daemon wires
	// this to embedding.AcquireManager with the workspace's config.
	Acquire func(workspaceRoot string) *embedding.EmbeddingManager
	// Release drops a reference taken by Acquire.
	Release func(m *embedding.EmbeddingManager)

	// buildMu serializes index mutations (BuildIndex / UpdateFile) so
	// concurrent clients never race the manager's single-writer guard.
	// The daemon is the coordinator — clients wait their turn.
	buildMu sync.Mutex
}

// Meta implements EmbeddingService.
func (s *EmbeddingManagerService) Meta(ctx context.Context) (string, int, string, error) {
	mgr := s.acquire("")
	if mgr == nil {
		return "", 0, "", errors.New("embedding service: no manager available")
	}
	defer s.release(mgr)
	if err := mgr.Init(ctx); err != nil {
		return "", 0, "", err
	}
	return mgr.Name(), mgr.Dimensions(), mgr.ModelHash(), nil
}

// Embed implements EmbeddingService.
func (s *EmbeddingManagerService) Embed(ctx context.Context, text string) ([]float32, error) {
	mgr := s.acquire("")
	if mgr == nil {
		return nil, errors.New("embedding service: no manager available")
	}
	defer s.release(mgr)
	if err := mgr.Init(ctx); err != nil {
		return nil, err
	}
	return mgr.Embed(ctx, text)
}

// EmbedBatch implements EmbeddingService.
func (s *EmbeddingManagerService) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	mgr := s.acquire("")
	if mgr == nil {
		return nil, errors.New("embedding service: no manager available")
	}
	defer s.release(mgr)
	if err := mgr.Init(ctx); err != nil {
		return nil, err
	}
	return mgr.EmbedBatch(ctx, texts)
}

// QuerySimilar implements EmbeddingService.
func (s *EmbeddingManagerService) QuerySimilar(ctx context.Context, workspaceRoot, text string, topK int, threshold float32) ([]embedding.QueryResult, error) {
	mgr := s.acquire(workspaceRoot)
	if mgr == nil {
		return nil, errors.New("embedding service: no manager available")
	}
	defer s.release(mgr)
	if topK <= 0 {
		topK = 5
	}
	if threshold <= 0 {
		threshold = 0.5
	}
	return mgr.QuerySimilar(ctx, text, topK, threshold)
}

// BuildIndex implements EmbeddingService.
func (s *EmbeddingManagerService) BuildIndex(ctx context.Context, workspaceRoot string) (*embedding.IndexStats, error) {
	// Serialize index writes across clients (daemon = sole writer).
	s.buildMu.Lock()
	defer s.buildMu.Unlock()

	mgr := s.acquire(workspaceRoot)
	if mgr == nil {
		return nil, errors.New("embedding service: no manager available")
	}
	defer s.release(mgr)
	return mgr.BuildIndex(ctx)
}

// CheckDuplicates implements EmbeddingService.
func (s *EmbeddingManagerService) CheckDuplicates(ctx context.Context, workspaceRoot, filePath, content string) (*embedding.CheckDuplicatesResult, error) {
	mgr := s.acquire(workspaceRoot)
	if mgr == nil {
		return nil, errors.New("embedding service: no manager available")
	}
	defer s.release(mgr)
	return mgr.CheckDuplicates(ctx, filePath, content)
}

func (s *EmbeddingManagerService) acquire(workspaceRoot string) *embedding.EmbeddingManager {
	if s.Acquire == nil {
		return nil
	}
	return s.Acquire(workspaceRoot)
}

func (s *EmbeddingManagerService) release(m *embedding.EmbeddingManager) {
	if s.Release != nil && m != nil {
		s.Release(m)
	}
}
