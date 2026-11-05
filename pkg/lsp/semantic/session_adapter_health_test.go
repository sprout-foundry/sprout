package semantic

import "testing"

// Both adapters start their backing process lazily on the first Run, and both
// factories hand the pool an adapter with no process yet. Healthy() must
// therefore report true for a not-yet-started adapter: the pool closes and
// evicts anything that reports unhealthy, so a false here would discard the
// session on every acquire and the worker would never get to start.
func TestSessionAdaptersAreHealthyBeforeTheirProcessStarts(t *testing.T) {
	t.Run("go", func(t *testing.T) {
		a := &goSessionAdapter{}
		if !a.Healthy() {
			t.Error("a go session with no server yet must be healthy, or the pool discards it before gopls starts")
		}
	})

	t.Run("typescript", func(t *testing.T) {
		a := &typeScriptSessionAdapter{}
		if !a.Healthy() {
			t.Error("a typescript session with no worker yet must be healthy, or the pool discards it before the worker starts")
		}
	})
}

// A closed adapter is never healthy, regardless of whether its process ran.
func TestClosedSessionAdaptersAreNotHealthy(t *testing.T) {
	goAdapter := &goSessionAdapter{closed: true}
	if goAdapter.Healthy() {
		t.Error("a closed go session reported healthy")
	}

	tsAdapter := &typeScriptSessionAdapter{closed: true}
	if tsAdapter.Healthy() {
		t.Error("a closed typescript session reported healthy")
	}
}
