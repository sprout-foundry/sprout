package agent

import (
	"fmt"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestRaceConcurrentMessageAccess verifies that concurrent reads and writes
// to the messages slice through AgentStateManager are safe.
func TestRaceConcurrentMessageAccess(t *testing.T) {
	mgr := NewAgentStateManager(false)
	var wg sync.WaitGroup

	// Writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				mgr.AddMessage(api.Message{Role: "user", Content: fmt.Sprintf("msg-%d-%d", n, j)})
			}
		}(i)
	}

	// Readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = mgr.GetMessages()
			}
		}()
	}

	// Setters (replace entire slice)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			mgr.SetMessages([]api.Message{{Role: "system", Content: fmt.Sprintf("reset-%d", j)}})
		}
	}()

	wg.Wait()
}

// TestRaceConcurrentMetricsAccess verifies that concurrent reads and writes
// to token/cost counters through AgentStateManager are safe.
func TestRaceConcurrentMetricsAccess(t *testing.T) {
	mgr := NewAgentStateManager(false)
	var wg sync.WaitGroup

	// Increment cost from multiple goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				mgr.AddCost(0.001)
			}
		}()
	}

	// Increment tokens
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				mgr.SetTotalTokens(mgr.GetTotalTokens() + 10)
			}
		}()
	}

	// Increment LLM call count
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				mgr.IncrementLLMCallCount()
			}
		}()
	}

	// Readers for various counters
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = mgr.GetTotalCost()
				_ = mgr.GetTotalTokens()
				_ = mgr.GetPromptTokens()
				_ = mgr.GetCompletionTokens()
				_ = mgr.GetLLMCallCount()
				_ = mgr.GetCachedTokens()
				_ = mgr.GetCachedCostSavings()
				_ = mgr.GetEstimatedTokenResponses()
			}
		}()
	}

	// Context token updaters
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			mgr.SetCurrentContextTokens(j * 100)
			mgr.SetMaxContextTokens(100000)
		}
	}()

	wg.Wait()
}

// TestRaceConcurrentIterationAccess verifies that concurrent reads and writes
// to the iteration counter through AgentStateManager are safe.
func TestRaceConcurrentIterationAccess(t *testing.T) {
	mgr := NewAgentStateManager(false)
	var wg sync.WaitGroup

	// Writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				mgr.SetCurrentIteration(n*100 + j)
			}
		}(i)
	}

	// Readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = mgr.GetCurrentIteration()
			}
		}()
	}

	wg.Wait()
}

// TestRaceConcurrentShellCommandHistory verifies that concurrent access to
// shell command history on the Agent is safe.
func TestRaceConcurrentShellCommandHistory(t *testing.T) {
	agent := &Agent{
		state:               NewAgentStateManager(false),
		output:              NewAgentOutputManager(),
		shellCommandHistory: make(map[string]*ShellCommandResult),
	}

	var wg sync.WaitGroup

	// Writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cmd := fmt.Sprintf("echo test-%d-%d", n, j)
				agent.SetShellCommandHistoryEntry(cmd, &ShellCommandResult{
					Command:    cmd,
					FullOutput: "test output",
				})
			}
		}(i)
	}

	// Readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = agent.GetShellCommandHistoryEntry(fmt.Sprintf("echo test-%d", j))
			}
		}()
	}

	// GetAll
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			_ = agent.GetAllShellCommandHistory()
		}
	}()

	// Clear
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 10; j++ {
			agent.ClearShellCommandHistory()
		}
	}()

	wg.Wait()
}

// TestRaceConcurrentTaskActionsAccess verifies that concurrent AddTaskAction
// calls on the Agent are safe.
func TestRaceConcurrentTaskActionsAccess(t *testing.T) {
	agent := &Agent{
		state:  NewAgentStateManager(false),
		output: NewAgentOutputManager(),
	}

	var wg sync.WaitGroup

	// Concurrent AddTaskAction calls
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				agent.AddTaskAction("test", fmt.Sprintf("action-%d-%d", n, j), "details")
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = agent.GetTaskActions()
			}
		}()
	}

	wg.Wait()
}

// TestRaceConcurrentStatsCallback verifies that concurrent SetStatsUpdateCallback
// and TrackMetricsFromResponse calls are safe via atomic.Value.
func TestRaceConcurrentStatsCallback(t *testing.T) {
	agent := &Agent{
		state:  NewAgentStateManager(false),
		output: NewAgentOutputManager(),
	}

	var wg sync.WaitGroup
	callbackCount := int64(0)

	// One goroutine sets callbacks
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			agent.SetStatsUpdateCallback(func(tokens int, cost float64) {
				// noop for race test
			})
		}
	}()

	// Another goroutine triggers metrics updates
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			agent.TrackMetricsFromResponse(100, 50, 150, 0.01, 0, 0)
		}
	}()

	// Reader goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			_ = agent.GetTotalTokens()
			_ = agent.GetTotalCost()
		}
	}()

	wg.Wait()
	_ = callbackCount
}

// TestRaceConcurrentContextTokenAccess verifies that concurrent context token
// reads and writes are safe.
func TestRaceConcurrentContextTokenAccess(t *testing.T) {
	mgr := NewAgentStateManager(false)
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				mgr.SetCurrentContextTokens(n*1000 + j)
				mgr.SetMaxContextTokens(128000)
				_ = mgr.GetCurrentContextTokens()
				_ = mgr.GetMaxContextTokens()
				_ = mgr.IsContextWarningIssued()
				mgr.SetContextWarningIssued(j%2 == 0)
			}
		}(i)
	}

	wg.Wait()
}

// TestRaceConcurrentSecurityManager verifies that concurrent security state
// access on AgentSecurityManager is safe.
func TestRaceConcurrentSecurityManager(t *testing.T) {
	mgr := NewAgentSecurityManager()
	var wg sync.WaitGroup

	// Concurrent allowlist additions exercise the per-folder
	// session allowlist's mutex. Different folder names each
	// iteration to force the dedup path to walk the list while
	// readers are checking it.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			mgr.AddSessionAllowedFolder(fmt.Sprintf("/tmp/race-%d", j))
		}
	}()

	// Concurrent bypass readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = mgr.IsSecurityBypassApproved()
			}
		}()
	}

	// Concurrent concern ignore writes
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			mgr.SetConcernIgnored(fmt.Sprintf("file-%d.go", j), "secret_pattern")
		}
	}()

	// Concurrent concern ignore reads
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = mgr.IsConcernIgnored(fmt.Sprintf("file-%d.go", j), "secret_pattern")
			}
		}()
	}

	// Unsafe mode toggles
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			mgr.SetUnsafeMode(j%2 == 0)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			_ = mgr.GetUnsafeMode()
		}
	}()

	wg.Wait()
}
