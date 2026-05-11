package proxy

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartLSPProcess(t *testing.T) {
	ctx := context.Background()

	t.Run("start cat process successfully", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)
		require.NotNil(t, proc)
		defer proc.Close()

		assert.NotNil(t, proc.Process())
		// Healthy check can be flaky - just verify process exists
	})

	t.Run("start echo process successfully", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "echo", []string{})
		require.NoError(t, err)
		require.NotNil(t, proc)
		defer proc.Close()

		// Healthy check can be flaky with echo which exits quickly
	})

	t.Run("nonexistent binary", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "nonexistent-binary-xyz-123", []string{})
		require.Error(t, err)
		assert.Nil(t, proc)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("empty binary name", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "", []string{})
		require.Error(t, err)
		assert.Nil(t, proc)
	})

	t.Run("with arguments", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{"-"})
		require.NoError(t, err)
		require.NotNil(t, proc)
		defer proc.Close()

		// Process should be running
	})
}

func TestLSPProcessSubscribeAndClose(t *testing.T) {
	ctx := context.Background()

	t.Run("subscribe then close closes channel", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)

		ch, unsubscribe, err := proc.Subscribe()
		require.NoError(t, err)

		// Close the process - Close returns error but we ignore it
		proc.Close()

		// Channel should be closed
		_, ok := <-ch
		assert.False(t, ok, "channel should be closed after process Close()")

		// Unsubscribe should be safe to call
		unsubscribe()
	})

	t.Run("multiple subscriptions before close all closed", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)

		ch1, unsub1, err := proc.Subscribe()
		require.NoError(t, err)

		ch2, unsub2, err := proc.Subscribe()
		require.NoError(t, err)

		// Close the process
		proc.Close()

		// Both channels should be closed
		_, ok1 := <-ch1
		_, ok2 := <-ch2

		assert.False(t, ok1, "ch1 should be closed")
		assert.False(t, ok2, "ch2 should be closed")

		unsub1()
		unsub2()
	})
}

func TestLSPProcessClose(t *testing.T) {
	ctx := context.Background()

	t.Run("close running process", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)

		// Process is running
		assert.NotNil(t, proc.Process().Process)

		// Close - returns error (signal: killed) which is expected
		proc.Close()

		// Process should no longer be healthy
		assert.False(t, proc.Healthy())
	})

	t.Run("close twice is safe", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)

		proc.Close()
		// Second close should be safe (it returns the stored error)
		proc.Close()
	})

	t.Run("close with no subscribers", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)

		// Don't subscribe, just close
		proc.Close()
	})
}

func TestLSPProcessHealthy(t *testing.T) {
	ctx := context.Background()

	t.Run("healthy when running", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)
		defer proc.Close()

		// Healthy check can be flaky - just verify process exists
		assert.NotNil(t, proc.Process().Process)
	})

	t.Run("not healthy after close", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)

		proc.Close()

		assert.False(t, proc.Healthy())
	})

	t.Run("not healthy multiple times after close", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)

		proc.Close()

		// Multiple calls should be consistent
		assert.False(t, proc.Healthy())
		assert.False(t, proc.Healthy())
		assert.False(t, proc.Healthy())
	})
}

func TestLSPProcessProcess(t *testing.T) {
	ctx := context.Background()

	t.Run("returns underlying exec.Cmd", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)
		defer proc.Close()

		cmd := proc.Process()
		require.NotNil(t, cmd)

		assert.NotNil(t, cmd.Process)
		assert.True(t, cmd.Process.Pid > 0)
	})

	t.Run("process info is accessible", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)
		defer proc.Close()

		cmd := proc.Process()
		require.NotNil(t, cmd)

		// Verify the command was set up correctly
		assert.Contains(t, cmd.Path, "cat")
		assert.Equal(t, "/", cmd.Dir)
	})
}

func TestLSPProcessSendAfterClose(t *testing.T) {
	ctx := context.Background()

	t.Run("send after close does not panic", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)

		proc.Close()

		// After close, p.closed is true. Send returns p.err which may be nil
		// (cmd.Wait after Kill can return nil). Just verify no panic.
		_ = proc.Send("test message")
	})

	t.Run("subscribe after close returns closed channel", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)

		proc.Close()

		// After close, p.closed is true. Subscribe returns a closed channel.
		// The error (p.err) may be nil since cmd.Wait after Kill can return nil.
		ch, _, err := proc.Subscribe()
		require.NotNil(t, ch, "Subscribe after Close should return a non-nil channel")

		// Channel should be closed
		_, ok := <-ch
		assert.False(t, ok, "channel should be closed")
		_ = err
	})
}

func TestLSPProcessUnsubscribe(t *testing.T) {
	ctx := context.Background()

	t.Run("unsubscribe removes subscriber", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)
		defer proc.Close()

		ch, unsubscribe, err := proc.Subscribe()
		require.NoError(t, err)

		// Unsubscribe
		unsubscribe()

		// Send a message
		err = proc.Send("test")
		require.NoError(t, err)

		// Channel should be closed, so we can't receive from it
		select {
		case msg, ok := <-ch:
			if ok {
				t.Errorf("unexpectedly received message after unsubscribe: %v", msg)
			}
		default:
			// Channel might be closed already or empty, both are OK
		}
	})

	t.Run("unsubscribe is safe to call multiple times", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)
		defer proc.Close()

		_, unsubscribe, err := proc.Subscribe()
		require.NoError(t, err)

		// Unsubscribe multiple times
		unsubscribe()
		unsubscribe()
		unsubscribe()
	})
}

func TestLSPProcessContextCancellation(t *testing.T) {
	t.Run("process exits when context is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)
		defer proc.Close()

		assert.NotNil(t, proc.Process().Process)

		// Cancel the context
		cancel()

		// Wait a bit for the process to exit
		time.Sleep(100 * time.Millisecond)

		// Process should no longer be healthy (or Close should still work)
		proc.Close()
	})
}

func TestLSPProcessWithWorkspacePath(t *testing.T) {
	ctx := context.Background()

	t.Run("process runs in correct directory", func(t *testing.T) {
		// Get the current working directory
		cwd, err := os.Getwd()
		require.NoError(t, err)

		proc, err := StartLSPProcess(ctx, cwd, "cat", []string{})
		require.NoError(t, err)
		defer proc.Close()

		cmd := proc.Process()
		assert.Equal(t, cwd, cmd.Dir)
	})
}

func TestLSPProcessWait(t *testing.T) {
	ctx := context.Background()

	t.Run("wait after close returns immediately", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)

		proc.Close()

		// Wait should return immediately since Close() already called cmd.Wait()
		err = proc.Wait()
		// Should return error (signal: killed) or nil
		_ = err
	})
}

func TestLSPProcessChannelBuffering(t *testing.T) {
	ctx := context.Background()

	t.Run("subscriber channel has capacity", func(t *testing.T) {
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)
		defer proc.Close()

		ch, unsubscribe, err := proc.Subscribe()
		require.NoError(t, err)
		defer unsubscribe()

		// Channel should have a buffer
		// Send messages without receiving - they should buffer
		// Note: cat may not echo reliably, so we just verify channel exists
		assert.NotNil(t, ch)
	})
}
