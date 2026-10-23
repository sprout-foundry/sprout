package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// estimateCostFromPricing runs on every LLM response, while SetModel can swap
// the client concurrently from the WebUI. Reading the client field directly
// races with setClient's write, so the cost path must go through the locked
// accessor. Run with -race.
func TestEstimateCostFromPricingReadsClientUnderTheLock(t *testing.T) {
	a := newTestAgent(t)
	sp := &sproutProvider{agent: a}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			_ = sp.estimateCostFromPricing(10, 10)
		}
	}()

	for i := 0; i < 200; i++ {
		// setClient is what SetModel calls; it writes a.client under clientMu.
		a.setClient(&stubModelClient{model: "m"}, api.ClientType("test"))
	}
	<-done
}

// stubModelClient satisfies the parts of api.ClientInterface the cost path
// touches. Embedding the interface keeps the stub small: any method the cost
// path starts calling that is not implemented here fails loudly at runtime
// rather than silently returning a zero value.
type stubModelClient struct {
	api.ClientInterface
	model string
}

func (s *stubModelClient) GetModel() string { return s.model }

func (s *stubModelClient) GetProvider() string { return "test" }
