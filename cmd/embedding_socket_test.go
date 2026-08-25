//go:build !js

package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/daemon"
	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// writeWorkspaceConfig writes the workspace-level config file used by
// configuration.GetWorkspaceConfigPath for a temp workspace root.
func writeWorkspaceConfig(t *testing.T, root, content string) {
	t.Helper()
	path := filepath.Join(root, ".sprout", "workspace.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// TestDaemonEmbeddingAcquireGate covers the SP-137 Phase 1 gate on the
// daemon's embedding service Acquire: a manager is only handed out when the
// workspace's stored config opts in (enabled && experimental), and a gated
// workspace is refused with a distinguishable error rather than a generic
// "no manager available".
func TestDaemonEmbeddingAcquireGate(t *testing.T) {
	t.Setenv("SPROUT_EXPERIMENTAL_EMBEDDINGS", "")
	svc := newDaemonEmbeddingService()

	t.Run("enabled and experimental", func(t *testing.T) {
		root := t.TempDir()
		writeWorkspaceConfig(t, root, `{"embedding_index":{"enabled":true,"experimental":true}}`)

		mgr, err := svc.Acquire(root)
		require.NoError(t, err, "Acquire must not error when the workspace opted in")
		require.NotNil(t, mgr, "Acquire must return a manager when the workspace opted in")
		svc.Release(mgr)
	})

	t.Run("enabled only", func(t *testing.T) {
		root := t.TempDir()
		writeWorkspaceConfig(t, root, `{"embedding_index":{"enabled":true}}`)

		mgr, err := svc.Acquire(root)
		require.Nil(t, mgr, "enabled without experimental must stay gated")
		require.Error(t, err)
		require.Contains(t, err.Error(), "not enabled for this workspace")
	})

	t.Run("no config file", func(t *testing.T) {
		root := t.TempDir()

		mgr, err := svc.Acquire(root)
		require.Nil(t, mgr, "no workspace config must stay gated")
		require.Error(t, err)
		require.Contains(t, err.Error(), "not enabled for this workspace")
	})

	t.Run("malformed json", func(t *testing.T) {
		root := t.TempDir()
		writeWorkspaceConfig(t, root, `{"embedding_index": {not json`)

		mgr, err := svc.Acquire(root)
		require.Nil(t, mgr, "malformed workspace config must stay gated")
		require.Error(t, err)
		require.Contains(t, err.Error(), "not enabled for this workspace")
	})

	t.Run("service surfaces gate error not no-manager", func(t *testing.T) {
		root := t.TempDir()

		_, err := svc.QuerySimilar(context.Background(), root, "hello", 5, 0.5)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not enabled for this workspace")
		require.NotContains(t, err.Error(), "no manager available")
	})
}

// TestDaemonEmbeddingSocketGatedWorkspace verifies the socket response for a
// gated workspace carries the distinguishable "not enabled" error. It dials
// the socket directly (bypassing the provider's meta handshake, which would
// initialize a real manager) so the gate fires before any model work.
func TestDaemonEmbeddingSocketGatedWorkspace(t *testing.T) {
	t.Setenv("SPROUT_EXPERIMENTAL_EMBEDDINGS", "")
	root := t.TempDir() // no workspace config → gated

	sockPath := shortSocketPath(t, "embed-gate")
	svc := newDaemonEmbeddingService()
	srv := &daemon.EmbeddingServer{SocketPath: sockPath, Service: svc}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Start(ctx))
	t.Cleanup(func() { srv.Close() })

	conn, err := net.Dial("unix", sockPath)
	require.NoError(t, err)
	defer conn.Close()

	req := embedding.RemoteRequest{ID: "gate-test", Op: embedding.RemoteOpQuery,
		Workspace: root, Text: "hello", K: 5, Threshold: 0.5}
	payload, err := json.Marshal(req)
	require.NoError(t, err)
	require.NoError(t, conn.SetDeadline(time.Now().Add(10*time.Second)))
	_, err = conn.Write(append(payload, '\n'))
	require.NoError(t, err)

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	require.NoError(t, err)
	var resp embedding.RemoteResponse
	require.NoError(t, json.Unmarshal(line, &resp))
	require.Contains(t, resp.Error, "not enabled for this workspace")
}
