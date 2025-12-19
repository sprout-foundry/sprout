package agent

import (
	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/factory"
)

// newStubClient creates a stub/mock client for testing purposes
// It uses the TestClient from the factory package which implements the ClientInterface
func newStubClient(provider, model string) api.ClientInterface {
	// Create a test client with the specified provider and model
	client := &factory.TestClient{}
	client.SetModel(model)
	return client
}
