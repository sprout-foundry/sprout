//go:build !darwin || !arm64 || !cgo

package localmodel

import (
	"context"
	"fmt"
	"sync"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// LocalProvider is a stub on platforms without MLX. All methods return
// errors indicating local LLM is unavailable. This allows the rest of
// sprout to compile and run on non-Apple-Silicon platforms.
type LocalProvider struct{}

var (
	globalProvider     *LocalProvider
	globalProviderOnce sync.Once
)

func GetLocalProvider() *LocalProvider {
	globalProviderOnce.Do(func() {
		globalProvider = &LocalProvider{}
	})
	return globalProvider
}

// isModelLoaded reports whether the model is currently in memory.
func (p *LocalProvider) isModelLoaded() bool    { return false }
func (p *LocalProvider) loadedModelDir() string { return "" }
func (p *LocalProvider) loadedModelID() string  { return "local" }

func (p *LocalProvider) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return nil, fmt.Errorf("local LLM requires Apple Silicon with MLX")
}
func (p *LocalProvider) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	return nil, fmt.Errorf("local LLM requires Apple Silicon with MLX")
}
func (p *LocalProvider) CheckConnection() error {
	return fmt.Errorf("local LLM requires Apple Silicon with MLX")
}
func (p *LocalProvider) SetDebug(bool)         {}
func (p *LocalProvider) SetModel(string) error { return nil }
func (p *LocalProvider) GetModel() string      { return "local" }
func (p *LocalProvider) GetProvider() string   { return "sprout-local" }
func (p *LocalProvider) GetModelContextLimit() (int, error) {
	return 0, fmt.Errorf("local LLM requires Apple Silicon with MLX")
}
func (p *LocalProvider) ListModels(ctx context.Context) ([]api.ModelInfo, error) { return nil, nil }
func (p *LocalProvider) SupportsVision() bool                                    { return false }
func (p *LocalProvider) SupportsConversationalVision() bool                      { return false }
func (p *LocalProvider) VisionCapabilities() api.VisionCapabilities {
	return api.VisionCapabilities{}
}
func (p *LocalProvider) GetVisionModel() string { return "" }
func (p *LocalProvider) SendVisionRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return nil, fmt.Errorf("local LLM requires Apple Silicon with MLX")
}
func (p *LocalProvider) GetLastTPS() float64             { return 0 }
func (p *LocalProvider) GetAverageTPS() float64          { return 0 }
func (p *LocalProvider) GetTPSStats() map[string]float64 { return nil }
func (p *LocalProvider) ResetTPSStats()                  {}
func (p *LocalProvider) Close() error                    { return nil }
