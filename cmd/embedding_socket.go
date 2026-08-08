package cmd

import (
	"context"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/daemon"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// defaultEmbeddingSocketName is the Unix socket the daemon serves the SP-136
// P3 embedding protocol on, under the data dir (override with
// SPROUT_DAEMON_EMBEDDING_SOCKET).
const defaultEmbeddingSocketName = "embed.sock"

// EmbeddingSocketPath returns the daemon embedding socket path, honoring the
// SPROUT_DAEMON_EMBEDDING_SOCKET override.
func EmbeddingSocketPath() string {
	if p := os.Getenv("SPROUT_DAEMON_EMBEDDING_SOCKET"); p != "" {
		return p
	}
	dataDir, err := envutil.DataDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "sprout", defaultEmbeddingSocketName)
	}
	return filepath.Join(dataDir, defaultEmbeddingSocketName)
}

// maybeEnableRemoteEmbedding wires CLI processes to the daemon's embedding
// socket (SP-136 P3): the process-wide provider factory makes every
// EmbeddingManager embed through the daemon instead of loading its own
// 155MB model. The factory falls back to in-process ONNX when the socket is
// unavailable, so the CLI always works.
//
// Skipped when running AS the daemon (daemonMode / SPROUT_DAEMON=1) — the
// daemon hosts the model, it does not proxy to itself.
func maybeEnableRemoteEmbedding(daemonMode bool) {
	if daemonMode {
		return
	}
	if v, ok := os.LookupEnv("SPROUT_DAEMON"); ok && v == "1" {
		return
	}

	socketPath := EmbeddingSocketPath()
	embedding.SetProviderFactory(func(ctx context.Context) (embedding.EmbeddingProvider, error) {
		return embedding.NewRemoteEmbeddingProvider(socketPath)
	})
}

// startDaemonEmbeddingServer starts the daemon-side embedding socket server
// (SP-136 P3). Returns the server (or nil if not in daemon mode / failed).
// The daemon owns the sole model copy and sole index writer per workspace.
func startDaemonEmbeddingServer(ctx context.Context, daemonMode bool) *daemon.EmbeddingServer {
	if !daemonMode {
		return nil
	}

	socketPath := EmbeddingSocketPath()
	svc := &daemon.EmbeddingManagerService{
		Acquire: func(workspaceRoot string) *embedding.EmbeddingManager {
			return embedding.AcquireManager(&configuration.EmbeddingIndexConfig{}, workspaceRoot)
		},
		Release: embedding.ReleaseManager,
	}
	srv := &daemon.EmbeddingServer{SocketPath: socketPath, Service: svc}
	if err := srv.Start(ctx); err != nil {
		console.GlyphWarning.Fprintf(os.Stderr,
			"embedding socket server failed to start at %s: %v\n", socketPath, err)
		return nil
	}
	console.GlyphDim.Printf("Embedding socket serving at %s", socketPath)
	return srv
}
