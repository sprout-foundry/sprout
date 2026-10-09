//go:build js

package agent

// initAuditLogger is a no-op in the WASM (browser) build: the audit log is a
// host-side JSONL file opened with os.OpenFile, which the browser environment
// does not provide.
func (a *Agent) initAuditLogger() {}

// closeAuditLogger is a no-op in the WASM (browser) build — no file handle is
// ever opened.
func (a *Agent) closeAuditLogger() {}
