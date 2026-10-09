//go:build js

package agent

// auditForwarder is a no-op in the js/wasm build, which has no host audit
// endpoint transport (net/http request execution is unavailable). Events are
// still written to the local audit log by the sink.
type auditForwarder struct{}

func newAuditForwarder(endpoint string, batchSize, flushIntervalSeconds int) *auditForwarder {
	return &auditForwarder{}
}

func (f *auditForwarder) Enqueue(data []byte) {}

func (f *auditForwarder) Close() {}
