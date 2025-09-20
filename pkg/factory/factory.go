package factory

import (
	"fmt"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/agent_providers"
)

// DeepInfraClientWrapper wraps DeepInfraProvider to implement the full ClientInterface
type DeepInfraClientWrapper struct {
	provider *providers.DeepInfraProvider
}

// Delegate all methods to the provider
func (w *DeepInfraClientWrapper) SendChatRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	return w.provider.SendChatRequest(messages, tools, reasoning)
}

func (w *DeepInfraClientWrapper) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	return w.provider.SendChatRequestStream(messages, tools, reasoning, callback)
}

func (w *DeepInfraClientWrapper) CheckConnection() error {
	return w.provider.CheckConnection()
}

func (w *DeepInfraClientWrapper) SetDebug(debug bool) {
	w.provider.SetDebug(debug)
}

func (w *DeepInfraClientWrapper) SetModel(model string) error {
	return w.provider.SetModel(model)
}

func (w *DeepInfraClientWrapper) GetModel() string {
	return w.provider.GetModel()
}

func (w *DeepInfraClientWrapper) GetProvider() string {
	return w.provider.GetProvider()
}

func (w *DeepInfraClientWrapper) GetModelContextLimit() (int, error) {
	return w.provider.GetModelContextLimit()
}

func (w *DeepInfraClientWrapper) SupportsVision() bool {
	return w.provider.SupportsVision()
}

func (w *DeepInfraClientWrapper) GetVisionModel() string {
	return w.provider.GetVisionModel()
}

func (w *DeepInfraClientWrapper) SendVisionRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	return w.provider.SendVisionRequest(messages, tools, reasoning)
}

func (w *DeepInfraClientWrapper) ListModels() ([]api.ModelInfo, error) {
	return w.provider.ListModels()
}

// TPS methods that the provider doesn't implement
func (w *DeepInfraClientWrapper) GetLastTPS() float64 {
	return 0.0 // Provider doesn't track TPS
}

func (w *DeepInfraClientWrapper) GetAverageTPS() float64 {
	return 0.0 // Provider doesn't track TPS
}

func (w *DeepInfraClientWrapper) GetTPSStats() map[string]float64 {
	return map[string]float64{} // Provider doesn't track TPS
}

func (w *DeepInfraClientWrapper) ResetTPSStats() {
	// No-op - provider doesn't track TPS
}

// OpenRouterClientWrapper wraps OpenRouterProvider to implement the full ClientInterface
type OpenRouterClientWrapper struct {
	provider *providers.OpenRouterProvider
}

// Delegate all methods to the provider
func (w *OpenRouterClientWrapper) SendChatRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	return w.provider.SendChatRequest(messages, tools, reasoning)
}

func (w *OpenRouterClientWrapper) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	return w.provider.SendChatRequestStream(messages, tools, reasoning, callback)
}

func (w *OpenRouterClientWrapper) CheckConnection() error {
	return w.provider.CheckConnection()
}

func (w *OpenRouterClientWrapper) SetDebug(debug bool) {
	w.provider.SetDebug(debug)
}

func (w *OpenRouterClientWrapper) SetModel(model string) error {
	return w.provider.SetModel(model)
}

func (w *OpenRouterClientWrapper) GetModel() string {
	return w.provider.GetModel()
}

func (w *OpenRouterClientWrapper) GetProvider() string {
	return w.provider.GetProvider()
}

func (w *OpenRouterClientWrapper) GetModelContextLimit() (int, error) {
	return w.provider.GetModelContextLimit()
}

func (w *OpenRouterClientWrapper) SupportsVision() bool {
	return w.provider.SupportsVision()
}

func (w *OpenRouterClientWrapper) GetVisionModel() string {
	return w.provider.GetVisionModel()
}

func (w *OpenRouterClientWrapper) SendVisionRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	return w.provider.SendVisionRequest(messages, tools, reasoning)
}

func (w *OpenRouterClientWrapper) ListModels() ([]api.ModelInfo, error) {
	return w.provider.ListModels()
}

// TPS methods that the provider now implements
func (w *OpenRouterClientWrapper) GetLastTPS() float64 {
	return w.provider.GetLastTPS()
}

func (w *OpenRouterClientWrapper) GetAverageTPS() float64 {
	return w.provider.GetAverageTPS()
}

func (w *OpenRouterClientWrapper) GetTPSStats() map[string]float64 {
	return w.provider.GetTPSStats()
}

func (w *OpenRouterClientWrapper) ResetTPSStats() {
	w.provider.ResetTPSStats()
}

// CreateProviderClient is a factory function that creates providers
func CreateProviderClient(clientType api.ClientType, model string) (api.ClientInterface, error) {
	switch clientType {
	case api.OpenAIClientType:
		return api.NewOpenAIClientWrapper(model)
	case api.DeepInfraClientType:
		// Use the real DeepInfra provider wrapped to implement ClientInterface
		provider, err := providers.NewDeepInfraProviderWithModel(model)
		if err != nil {
			return nil, err
		}
		return &DeepInfraClientWrapper{provider: provider}, nil
	case api.OllamaClientType, api.OllamaLocalClientType:
		return api.NewOllamaLocalClient(model)
	case api.OllamaTurboClientType:
		return api.NewOllamaTurboClient(model)
	case api.OpenRouterClientType:
		// Use the real OpenRouter provider wrapped to implement ClientInterface
		provider, err := providers.NewOpenRouterProviderWithModel(model)
		if err != nil {
			return nil, err
		}
		return &OpenRouterClientWrapper{provider: provider}, nil
	default:
		return nil, fmt.Errorf("unknown client type: %s", clientType)
	}
}
