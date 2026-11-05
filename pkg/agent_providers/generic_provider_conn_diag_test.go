package providers

import (
	"testing"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// TestNetworkErrorWithConnDiagAttachesDiagnostics pins rel.5 (c): the
// connection-reuse fact (and request start time) travel on the network
// TypedError's Details so the agent's provider-error record can read them.
func TestNetworkErrorWithConnDiagAttachesDiagnostics(t *testing.T) {
	p := &GenericProvider{}
	started := time.Now()
	diag := &connDiagnostics{Reused: true, RequestStartedAt: started}
	err := p.networkErrorWithConnDiag("HTTP request failed", nil, diag)
	if te := agenterrors.AsTypedError(err); te == nil {
		t.Fatal("expected a network TypedError")
	} else {
		if reused, ok := te.Details["conn_reused"].(bool); !ok || !reused {
			t.Errorf("conn_reused detail = %v (%T), want true", te.Details["conn_reused"], te.Details["conn_reused"])
		}
	}

	// A nil diagnostic must not attach a bogus reuse flag.
	err2 := p.networkErrorWithConnDiag("HTTP request failed", nil, nil)
	if te := agenterrors.AsTypedError(err2); te != nil {
		if _, present := te.Details["conn_reused"]; present {
			t.Error("a nil diagnostic must not attach conn_reused")
		}
	}
}
