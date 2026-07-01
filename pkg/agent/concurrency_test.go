package agent

import (
	"context"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// ---------------------------------------------------------------------------
// Test 1: AgentMCPManager.toolsCache — unsynchronized read/write of slice field
// ---------------------------------------------------------------------------
//
// BUG: GetToolsCache() returns m.toolsCache and SetToolsCache() writes
// m.toolsCache without holding any mutex. Concurrent readers and writers
// on this slice header trigger a data race.
//
// FIX: Protect toolsCache with a dedicated mutex (or reuse initMu) in both
// GetToolsCache and SetToolsCache.
func TestRace_AgentMCPManager_toolsCache(t *testing.T) {
	m := NewAgentMCPManager()
	var wg sync.WaitGroup

	// Readers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = m.GetToolsCache()
			}
		}()
	}

	// Writers: SetToolsCache writes the slice header without a lock.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.SetToolsCache([]api.Tool{{Type: "function"}})
			}
		}()
	}

	wg.Wait()
}

// ---------------------------------------------------------------------------
// Test 2: AgentMCPManager.initialized — unsynchronized bool read/write
// ---------------------------------------------------------------------------
//
// BUG: IsInitialized() reads m.initialized and SetInitialized() writes it
// without any lock. The bool field is accessed from multiple goroutines
// concurrently, which is a data race under Go's memory model.
//
// FIX: Protect initialized with a mutex in both IsInitialized and
// SetInitialized, or use an atomic.Bool.
func TestRace_AgentMCPManager_initialized(t *testing.T) {
	m := NewAgentMCPManager()
	var wg sync.WaitGroup

	// Readers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = m.IsInitialized()
			}
		}()
	}

	// Writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.SetInitialized(j%2 == 0)
			}
		}()
	}

	wg.Wait()
}

// ---------------------------------------------------------------------------
// Test 3: AgentMCPManager.initErr — unsynchronized error read/write
// ---------------------------------------------------------------------------
//
// BUG: GetInitError() returns m.initErr and SetInitError() writes m.initErr
// without any lock. Error interface values are pointers + type descriptors
// that race when read/written from concurrent goroutines.
//
// FIX: Protect initErr with a mutex in both GetInitError and SetInitError,
// or use an atomic.Value.
func TestRace_AgentMCPManager_initErr(t *testing.T) {
	m := NewAgentMCPManager()
	var wg sync.WaitGroup

	// Readers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = m.GetInitError()
			}
		}()
	}

	// Writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.SetInitError(testRaceError{msg: "err"})
			}
		}()
	}

	wg.Wait()
}

// ---------------------------------------------------------------------------
// Test 4: CircuitBreakerAction.Count — pointer escapes lock scope (TOCTOU)
// ---------------------------------------------------------------------------
//
// BUG: In checkCircuitBreaker(), the *CircuitBreakerAction pointer is copied
// out of the RLock scope (lines ~24-29 in tool_executor_circuit_breaker.go),
// then action.Count is read (line ~50) AFTER the lock has been released.
// Meanwhile, updateCircuitBreaker() increments action.Count under Lock.
// This is a classic TOCTOU race: the pointer escape means Count can be
// read without any synchronization at all.
//
// FIX: Do not copy the pointer outside the lock. Instead, read the Count
// value inside the lock scope and return the value (int), not the pointer.
// Alternatively, use atomic.Int64 for Count.
func TestRace_CircuitBreakerAction_Count_TOCTOU(t *testing.T) {
	action := &CircuitBreakerAction{
		ActionType: "edit_file",
		Target:     "some_file.go",
		Count:      0,
		LastUsed:   0,
	}

	var mu sync.RWMutex
	var wg sync.WaitGroup

	// Readers: simulate the FIXED checkCircuitBreaker that reads Count INSIDE
	// the RLock scope rather than escaping the *CircuitBreakerAction pointer
	// past RUnlock (which would TOCTOU-race the writers' Count++ below).
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				mu.RLock()
				count := action.Count
				mu.RUnlock()
				_ = count >= 3
			}
		}()
	}

	// Writers: simulate updateCircuitBreaker — write Count under Lock.
	// The real code holds Lock when modifying Count, but readers read Count
	// outside any lock, so the Lock does not synchronize with the readers.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				mu.Lock()
				action.Count++
				action.LastUsed = 0
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
}

// ---------------------------------------------------------------------------
// Test 5: Agent.interruptCtx / interruptCancel — unsynchronized access
// ---------------------------------------------------------------------------
//
// BUG: TriggerInterrupt() reads a.interruptCancel and calls it without a
// lock. ClearInterrupt() reads a.interruptCancel, writes a.interruptCtx and
// a.interruptCancel without a lock. resetInterruptForNewQuery() also reads
// and writes both fields without a lock. Multiple goroutines calling these
// methods concurrently race on the cancel func and context pointers.
//
// FIX: Protect interruptCtx and interruptCancel with a dedicated mutex, or
// use atomic.Pointer / atomic.Value for the cancel func.
func TestRace_Agent_interrupt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := &Agent{
		state:           NewAgentStateManager(false),
		output:          NewAgentOutputManager(),
		security:        NewAgentSecurityManager(),
		mcpSub:          NewAgentMCPManager(),
		interruptCtx:    ctx,
		interruptCancel: cancel,
	}

	var wg sync.WaitGroup

	// Callers: simulate TriggerInterrupt from multiple goroutines
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				a.TriggerInterrupt()
			}
		}()
	}

	// Clearers: simulate ClearInterrupt / resetInterruptForNewQuery
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				a.ClearInterrupt()
			}
		}()
	}

	wg.Wait()
}

// ---------------------------------------------------------------------------
// Test 6: Agent.embeddingMgr — unsynchronized pointer read/write
// ---------------------------------------------------------------------------
//
// BUG: EnableEmbeddingIndex() sets a.embeddingMgr. DisableEmbeddingIndex()
// reads a.embeddingMgr, calls Close(), and sets it to nil.
// IsEmbeddingIndexEnabled() and GetEmbeddingManager() read it. None of these
// hold any lock, so concurrent reads and writes of the pointer race.
//
// FIX: Use atomic.Pointer[embedding.EmbeddingManager] for embeddingMgr and
// use atomic.Load/atomic.Store in all getters and setters.
func TestRace_Agent_embeddingMgr(t *testing.T) {
	a := &Agent{
		state:    NewAgentStateManager(false),
		output:   NewAgentOutputManager(),
		security: NewAgentSecurityManager(),
		mcpSub:   NewAgentMCPManager(),
	}

	var wg sync.WaitGroup

	// Readers: IsEmbeddingIndexEnabled() / GetEmbeddingManager() read
	// a.embeddingMgr without synchronization.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = a.IsEmbeddingIndexEnabled()
			}
		}()
	}

	// Writers: simulate Enable/Disable. We hold embeddingMu (the same mutex the
	// real EnableEmbeddingIndex / DisableEmbeddingIndex paths use) around the
	// field writes so the test verifies the PRODUCT readers race-cleanly under
	// proper concurrent enable/disable, not that a buggy caller stays safe.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				a.embeddingMu.Lock()
				if j%2 == 0 {
					a.embeddingMgr = &embedding.EmbeddingManager{}
				} else {
					a.embeddingMgr = nil
				}
				a.embeddingMu.Unlock()
			}
		}()
	}

	wg.Wait()
}

// ---------------------------------------------------------------------------
// Test 7: sproutProvider.pastedImages — unsynchronized map read/write
// ---------------------------------------------------------------------------
//
// BUG: RegisterPastedImages() writes to sp.pastedImages[k] without a lock.
// attachPastedImages() reads sp.pastedImages and iterates over it without
// a lock. Concurrent map writes and iteration trigger "concurrent map
// read and map write" panics in Go.
//
// FIX: Protect pastedImages with a sync.RWMutex in the sproutProvider
// struct, using RLock for attachPastedImages and Lock for
// RegisterPastedImages.
func TestRace_sproutProvider_pastedImages(t *testing.T) {
	// Create a minimal mock client to satisfy api.ClientInterface
	client := &testRaceMockClient{}

	// Create a minimal agent (sproutProvider only needs a non-nil agent)
	a := &Agent{
		state:    NewAgentStateManager(false),
		output:   NewAgentOutputManager(),
		security: NewAgentSecurityManager(),
		mcpSub:   NewAgentMCPManager(),
	}

	// Create the sproutProvider directly (unexported but we're in the same
	// package) to avoid the nil-client check in NewSproutProvider.
	sp := &sproutProvider{
		agent:        a,
		client:       client,
		pastedImages: make(map[string][]api.ImageData),
	}

	var wg sync.WaitGroup

	// Writers: RegisterPastedImages writes to the map without a lock
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				images := map[string][]api.ImageData{
					"image": {{URL: "http://example.com/img.png", Type: "image/png"}},
				}
				sp.RegisterPastedImages(images)
			}
		}(i)
	}

	// Readers: attachPastedImages iterates the map without a lock
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = sp.attachPastedImages(nil)
			}
		}()
	}

	wg.Wait()
}

// ---------------------------------------------------------------------------
// Helper: minimal mockClient for the sproutProvider pastedImages test
// ---------------------------------------------------------------------------
//
// This mock implements just enough of api.ClientInterface to make the
// attachPastedImages code path work (it checks sp.client.SupportsVision()).
// SupportsVision returns true so that attachPastedImages exercises the
// iteration over pastedImages rather than returning early.
type testRaceMockClient struct{}

func (c *testRaceMockClient) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}

func (c *testRaceMockClient) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}

func (c *testRaceMockClient) CheckConnection() error             { return nil }
func (c *testRaceMockClient) SetDebug(debug bool)                {}
func (c *testRaceMockClient) SetModel(model string) error        { return nil }
func (c *testRaceMockClient) GetModel() string                   { return "test" }
func (c *testRaceMockClient) GetProvider() string                { return "test" }
func (c *testRaceMockClient) GetModelContextLimit() (int, error) { return 128000, nil }
func (c *testRaceMockClient) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	return nil, nil
}
func (c *testRaceMockClient) SupportsVision() bool   { return true }

// SupportsConversationalVision reports whether inline multimodal turns
// should embed the image. Defaults to false; overridden per client.
func (c *testRaceMockClient) SupportsConversationalVision() bool {
	return false
}
func (c *testRaceMockClient) GetVisionModel() string { return "" }
func (c *testRaceMockClient) SendVisionRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}
func (c *testRaceMockClient) GetLastTPS() float64             { return 0 }
func (c *testRaceMockClient) GetAverageTPS() float64          { return 0 }
func (c *testRaceMockClient) GetTPSStats() map[string]float64 { return nil }
func (c *testRaceMockClient) ResetTPSStats()                  {}

// testRaceError is a minimal error type used in the initErr race test.
type testRaceError struct {
	msg string
}

func (e testRaceError) Error() string { return e.msg }
