package agent

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/agent_audit"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// auditSink is the process-wide agent_audit.Sink. It writes every event to the
// local JSONL audit log (the same file `sprout audit tail` reads) and, when a
// host configures an audit endpoint, forwards events to a bounded background
// batch sender. Writes are facts-only: the events carry digests and metadata,
// never prompt or response content.
type auditSink struct {
	logger *tools.AuditLogger

	forwarder *auditForwarder
}

// newAuditSink builds a sink over the given local logger. When cfg names an
// audit endpoint, it also starts the background forwarder.
func newAuditSink(logger *tools.AuditLogger, cfg *configuration.Config) *auditSink {
	s := &auditSink{logger: logger}
	if cfg != nil && cfg.Audit != nil {
		if endpoint := strings.TrimSpace(cfg.Audit.Endpoint); endpoint != "" {
			s.forwarder = newAuditForwarder(endpoint, cfg.Audit.BatchSize, cfg.Audit.FlushIntervalSeconds)
		}
	}
	return s
}

// EmitCall writes a model-call event to the local log and forwards it.
func (s *auditSink) EmitCall(ev agent_audit.CallEvent) {
	if s == nil {
		return
	}
	s.write(ev)
	s.forward(ev)
}

// EmitTool writes a tool-execution event to the local log and forwards it.
func (s *auditSink) EmitTool(ev agent_audit.ToolEvent) {
	if s == nil {
		return
	}
	s.write(ev)
	s.forward(ev)
}

// Close stops the background forwarder, flushing any buffered events.
func (s *auditSink) Close() {
	if s == nil || s.forwarder == nil {
		return
	}
	s.forwarder.Close()
}

func (s *auditSink) write(ev any) {
	if s.logger == nil {
		return
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_ = s.logger.LogJSON(data)
}

func (s *auditSink) forward(ev any) {
	if s.forwarder == nil {
		return
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	s.forwarder.Enqueue(data)
}

// auditSinkMu serializes install/clear of the process-wide sink so two agents
// starting or shutting down at once cannot interleave a stale install.
var auditSinkMu sync.Mutex

// installAuditSink builds and installs the process-wide audit sink over the
// given logger and config. The previous sink (if any) is closed.
func installAuditSink(logger *tools.AuditLogger, cfg *configuration.Config) *auditSink {
	sink := newAuditSink(logger, cfg)
	auditSinkMu.Lock()
	prev := currentAuditSink()
	agent_audit.SetSink(sink)
	auditSinkMu.Unlock()
	if prev != nil {
		prev.Close()
	}
	return sink
}

// clearAuditSink removes the process-wide sink when it is still the one this
// agent installed, then closes it. Ownership-checked so shutting down one
// agent never silences auditing for another still running.
func clearAuditSink(owned *auditSink) {
	if owned == nil {
		return
	}
	auditSinkMu.Lock()
	if currentAuditSink() == owned {
		agent_audit.SetSink(nil)
	}
	auditSinkMu.Unlock()
	owned.Close()
}

// currentAuditSink returns the installed sink as *auditSink, or nil when none
// is installed or a different implementation is.
func currentAuditSink() *auditSink {
	if s, ok := agent_audit.CurrentSink().(*auditSink); ok {
		return s
	}
	return nil
}
