//go:build !js

package workflow

import (
	"context"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// QueryExecutor is the signature of cmd.ProcessQuery. The runner and loop
// accept this as a dependency so the workflow package never imports cmd/.
type QueryExecutor func(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, query string) error

// CLIOverrides provides the callback functions that applyWorkflowCommandOverrides
// needs to mutate CLI-level global flags (web UI, port, daemon, streaming,
// budget, heartbeat). The cmd/ package constructs this with closures over its
// real flag variables.
type CLIOverrides struct {
	SetWebUI    func(disabled bool)
	SetWebPort  func(port int)
	SetDaemon   func(enabled bool)
	SetNoStream func(enabled bool)
	GetNoStream func() bool

	// Budget/heartbeat CLI overrides. Zero values mean "inherit JSON".
	BudgetUSD        float64
	BudgetWarn       string
	HeartbeatSeconds int
}
