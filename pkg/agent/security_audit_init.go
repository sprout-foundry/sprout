//go:build !js

// Security audit logger lifecycle: resolving its path, opening it at agent
// start, and closing it on shutdown.
package agent

import (
	"os"
	"path/filepath"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// shellAuditFileName is the audit log file name. It matches the name
// `sprout audit tail` reads (cmd/audit.go's defaultAuditLogPath), so entries
// written by the agent are visible to the CLI.
const shellAuditFileName = "shell-audit.jsonl"

// resolveAuditLogPath returns the path of the security audit log. The
// configuration's computer_use.audit_log_dir is honored when set; otherwise
// the file lives directly under the state directory, matching the CLI's
// default so `sprout audit tail` reads what the agent writes.
func resolveAuditLogPath(cfg *configuration.Config) string {
	if cfg != nil && cfg.ComputerUse != nil {
		if dir := strings.TrimSpace(cfg.ComputerUse.AuditLogDir); dir != "" {
			return filepath.Join(dir, shellAuditFileName)
		}
	}
	stateDir, err := envutil.StateDir()
	if err != nil {
		// Fallback: use the config dir directly, mirroring cmd/audit.go.
		dir, _ := configuration.GetConfigDir()
		return filepath.Join(dir, shellAuditFileName)
	}
	return filepath.Join(stateDir, shellAuditFileName)
}

// initAuditLogger opens the security audit log and attaches it to the agent
// and to the package-level classifier logger, so every security decision
// reaches the file. It is best-effort: a failure to open the log leaves the
// agent without an audit logger rather than aborting startup.
//
// The logger is closed by Agent.Shutdown; the agent owns the file handle for
// its lifetime.
func (a *Agent) initAuditLogger() {
	if a == nil {
		return
	}
	logPath := resolveAuditLogPath(a.GetConfig())
	logger, err := tools.NewAuditLogger(logPath)
	if err != nil {
		if a.debug {
			_, _ = os.Stderr.WriteString("WARNING: Failed to initialize security audit logger: " + err.Error() + "\n")
		}
		return
	}
	a.SetAuditLogger(logger)
	// Install the per-call audit sink over the same logger so model-call and
	// tool-execution events land in the file `sprout audit tail` reads, and
	// reach the host audit endpoint when one is configured.
	a.callAuditSink.Store(installAuditSink(logger, a.GetConfig()))
}

// closeAuditLogger closes the agent's audit log file and detaches it from the
// package-level classifier logger. Idempotent — the agent clears its field so
// a second call is a no-op.
//
// The package-level classifier logger is a process-global pointer shared by
// every agent (including subagents, which do not open their own log). Detach
// it only when this agent is still its current owner, so shutting down one
// agent never silences audit logging for another still running.
func (a *Agent) closeAuditLogger() {
	if a == nil {
		return
	}
	if sink := a.callAuditSink.Swap(nil); sink != nil {
		clearAuditSink(sink)
	}
	logger := a.auditLogger.Load()
	if logger == nil {
		return
	}
	a.auditLogger.CompareAndSwap(logger, nil)
	if tools.ClassifierAuditLogger() == logger {
		tools.SetAuditLogger(nil)
	}
	_ = logger.Close()
}
