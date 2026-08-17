//go:build !js

package daemon

import (
	"bufio"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDaemonActivity covers the Begin/End in-flight counter and Touch/Idle
// window semantics that the daemon idle reaper relies on.
func TestDaemonActivity(t *testing.T) {
	t.Run("fresh activity is idle", func(t *testing.T) {
		a := NewDaemonActivity()
		assert.True(t, a.Idle(time.Now(), time.Minute),
			"an activity tracker that never saw traffic must be idle")
	})

	t.Run("idle after window since touch", func(t *testing.T) {
		a := NewDaemonActivity()
		a.Touch()
		now := time.Now()
		assert.False(t, a.Idle(now, time.Minute),
			"activity touched now must not be idle within the window")
		assert.True(t, a.Idle(now.Add(2*time.Minute), time.Minute),
			"activity untouched for longer than the window must be idle")
	})

	t.Run("in-flight begin without end is not idle", func(t *testing.T) {
		a := NewDaemonActivity()
		a.Begin()
		defer a.End()
		assert.False(t, a.Idle(time.Now(), time.Minute),
			"an in-flight request must never count as idle")
	})

	t.Run("completed request leaves recent activity", func(t *testing.T) {
		a := NewDaemonActivity()
		a.Begin()
		a.End()
		now := time.Now()
		assert.False(t, a.Idle(now, time.Minute),
			"a just-completed request must count as recent activity")
		assert.True(t, a.Idle(now.Add(2*time.Minute), time.Minute),
			"a request completed beyond the window must be idle")
	})

	t.Run("concurrent begin end stays balanced", func(t *testing.T) {
		a := NewDaemonActivity()
		const goroutines = 8
		const perGoroutine = 500
		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < perGoroutine; j++ {
					a.Begin()
					a.End()
				}
			}()
		}
		wg.Wait()
		assert.True(t, a.Idle(time.Now().Add(2*time.Minute), time.Minute),
			"every Begin must be matched by an End")
	})
}

// blockingAgentService blocks ListSessions until released so the test can
// observe the activity tracker while a request is in flight.
type blockingAgentService struct {
	*stubAgentService
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingAgentService) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.release:
		return b.stubAgentService.ListSessions(ctx)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TestAgentServer_UpdatesActivity verifies the agent socket server marks
// each request on its Activity tracker: an in-flight request is not idle,
// and a just-completed request leaves recent activity behind.
func TestAgentServer_UpdatesActivity(t *testing.T) {
	svc := &blockingAgentService{
		stubAgentService: newStubAgentService(),
		started:          make(chan struct{}),
		release:          make(chan struct{}),
	}
	activity := NewDaemonActivity()
	sockPath := shortSocketPath(t, "agent-activity")

	srv := &AgentServer{SocketPath: sockPath, Service: svc, Activity: activity}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Start(ctx))
	t.Cleanup(func() { srv.Close() })

	client, err := NewAgentClient(sockPath)
	require.NoError(t, err)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		_, err := client.ListSessions(ctx)
		done <- err
	}()

	select {
	case <-svc.started:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached the service")
	}
	assert.False(t, activity.Idle(time.Now(), time.Minute),
		"an in-flight socket request must keep the daemon alive")

	close(svc.release)
	require.NoError(t, <-done)

	// End runs after the response is flushed, which can lag the client's
	// read — wait until the server has drained the in-flight request.
	require.Eventually(t, func() bool {
		return activity.Idle(time.Now().Add(time.Minute), time.Minute)
	}, 5*time.Second, 10*time.Millisecond, "server must finish the request and drain in-flight")

	now := time.Now()
	assert.False(t, activity.Idle(now, time.Minute),
		"a just-served request must count as recent activity")
	assert.True(t, activity.Idle(now.Add(2*time.Minute), time.Minute),
		"activity must age out of the idle window")
}

// TestAgentServer_NilActivityServes verifies a server without an Activity
// tracker still serves requests (nil-tolerance for tests and embedders).
func TestAgentServer_NilActivityServes(t *testing.T) {
	svc := newStubAgentService()
	sockPath := shortSocketPath(t, "agent-nil-activity")

	srv := &AgentServer{SocketPath: sockPath, Service: svc} // no Activity set
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Start(ctx))
	t.Cleanup(func() { srv.Close() })

	client, err := NewAgentClient(sockPath)
	require.NoError(t, err)
	defer client.Close()

	sessions, err := client.ListSessions(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, sessions, "a server without Activity must still serve")
}

// blockingEmbeddingService blocks Meta until released so the test can
// observe the activity tracker while an embedding request is in flight.
type blockingEmbeddingService struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingEmbeddingService) Meta(ctx context.Context) (string, int, string, error) {
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.release:
		return "stub-embedding", 3, "stub-hash", nil
	case <-ctx.Done():
		return "", 0, "", ctx.Err()
	}
}

func (b *blockingEmbeddingService) Embed(context.Context, string) ([]float32, error) {
	return nil, errors.New("unused")
}

func (b *blockingEmbeddingService) EmbedBatch(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("unused")
}

func (b *blockingEmbeddingService) QuerySimilar(context.Context, string, string, int, float32) ([]embedding.QueryResult, error) {
	return nil, errors.New("unused")
}

func (b *blockingEmbeddingService) BuildIndex(context.Context, string) (*embedding.IndexStats, error) {
	return nil, errors.New("unused")
}

func (b *blockingEmbeddingService) CheckDuplicates(context.Context, string, string, string) (*embedding.CheckDuplicatesResult, error) {
	return nil, errors.New("unused")
}

// TestEmbeddingServer_UpdatesActivity verifies the embedding socket server
// marks each request on its Activity tracker: an in-flight request is not
// idle, and a just-completed request leaves recent activity behind.
func TestEmbeddingServer_UpdatesActivity(t *testing.T) {
	svc := &blockingEmbeddingService{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	activity := NewDaemonActivity()
	sockPath := shortSocketPath(t, "embedding-activity")

	srv := &EmbeddingServer{SocketPath: sockPath, Service: svc, Activity: activity}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Start(ctx))
	t.Cleanup(func() { srv.Close() })

	conn, err := net.Dial("unix", sockPath)
	require.NoError(t, err)
	defer conn.Close()

	done := make(chan error, 1)
	go func() {
		if _, err := conn.Write([]byte(`{"id":"1","op":"meta"}` + "\n")); err != nil {
			done <- err
			return
		}
		_, err := bufio.NewReader(conn).ReadBytes('\n')
		done <- err
	}()

	select {
	case <-svc.started:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached the embedding service")
	}
	assert.False(t, activity.Idle(time.Now(), time.Minute),
		"an in-flight embedding request must keep the daemon alive")

	close(svc.release)
	require.NoError(t, <-done)

	// End runs after the response is flushed, which can lag the client's
	// read — wait until the server has drained the in-flight request.
	require.Eventually(t, func() bool {
		return activity.Idle(time.Now().Add(time.Minute), time.Minute)
	}, 5*time.Second, 10*time.Millisecond, "server must finish the request and drain in-flight")

	now := time.Now()
	assert.False(t, activity.Idle(now, time.Minute),
		"a just-served embedding request must count as recent activity")
	assert.True(t, activity.Idle(now.Add(2*time.Minute), time.Minute),
		"activity must age out of the idle window")
}

// TestAgentServer_MalformedRequestTouchesActivity verifies a client sending
// garbage still counts as activity: the daemon must not be reaped while a
// client is talking to it, even if what it says is malformed.
func TestAgentServer_MalformedRequestTouchesActivity(t *testing.T) {
	svc := newStubAgentService()
	activity := NewDaemonActivity()
	sockPath := shortSocketPath(t, "agent-malformed")

	srv := &AgentServer{SocketPath: sockPath, Service: svc, Activity: activity}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Start(ctx))
	t.Cleanup(func() { srv.Close() })

	conn, err := net.Dial("unix", sockPath)
	require.NoError(t, err)
	defer conn.Close()

	_, err = conn.Write([]byte("this is not json\n"))
	require.NoError(t, err)

	// Read the error response so we know the server processed the line
	// before asserting on the activity it must have touched.
	_, err = bufio.NewReader(conn).ReadBytes('\n')
	require.NoError(t, err)

	assert.False(t, activity.Idle(time.Now(), time.Minute),
		"a malformed request must still count as recent activity")
}
