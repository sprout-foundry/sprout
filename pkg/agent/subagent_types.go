// Package agent: the SubagentRunner execution machinery (SP-141 phase 4,
// increment 1). The data types and pure helpers moved to
// pkg/agent/subagents (see subagent_forwarders.go for the aliases); the
// runner and the running-subagent tracker stay here until the runner seam
// lands, because they hold *Agent references (parentAgent, the child
// Agent) directly.
package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	agent_api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// SubagentRunner manages in-process subagent execution
type SubagentRunner struct {
	parentAgent *Agent
	shared      *SharedState
	active      sync.Map // taskID -> *runningSubagent

	// Operational metrics (atomic for concurrent access)
	metricActive       atomic.Int64
	metricQueued       atomic.Int64
	metricCompleted    atomic.Int64
	metricFailed       atomic.Int64
	metricCancelled    atomic.Int64
	metricQueuedWaitMS atomic.Int64

	// testClientFactory overrides client creation for testing only.
	// When non-nil, it is called instead of factory.CreateProviderClient.
	// This field is never set in production code.
	testClientFactory func(clientType agent_api.ClientType, model string) (agent_api.ClientInterface, error)
}

// runningSubagent tracks an active subagent execution
type runningSubagent struct {
	ID        string
	Persona   string
	Prompt    string
	StartedAt time.Time
	Agent     *Agent
	Ctx       context.Context
	Cancel    context.CancelFunc
	Completed atomic.Bool
}
