package api

import "fmt"

// CreateProviderClient is a simple factory function that creates providers
// This is just glue code - no logic, no data storage
func CreateProviderClient(clientType ClientType, model string) (ClientInterface, error) {
	switch clientType {
	case OpenAIClientType:
		return NewOpenAIClientWrapper(model)
	case DeepInfraClientType:
		return NewDeepInfraClientWrapper(model)
	case OllamaClientType, OllamaLocalClientType:
		return NewOllamaLocalClient(model)
	case OllamaTurboClientType:
		return NewOllamaTurboClient(model)
	case OpenRouterClientType:
		return NewOpenRouterClientWrapper(model)
	default:
		return nil, fmt.Errorf("unknown client type: %s", clientType)
	}
}
