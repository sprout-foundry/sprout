package configuration

import (
	"testing"
)

// The flat custom-provider key include_usage round-trips into
// StreamingConfig.IncludeUsage. Unset asks for usage (strict-OpenAI backends
// such as vLLM stream no token counts otherwise); false opts a provider out.
func TestCustomProviderIncludeUsageMapsToStreamingConfig(t *testing.T) {
	off := false
	for name, tc := range map[string]struct {
		set  *bool
		want bool
	}{
		"unset asks for usage": {nil, true},
		"false opts out":       {&off, false},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := CustomProviderConfig{
				Name:         "usage-test",
				Endpoint:     "http://localhost:9/v1/chat/completions",
				ContextSize:  8192,
				IncludeUsage: tc.set,
			}
			pc, err := cfg.ToProviderConfig()
			if err != nil {
				t.Fatalf("ToProviderConfig: %v", err)
			}
			if got := pc.Streaming.WantsUsage(); got != tc.want {
				t.Fatalf("WantsUsage() = %v, want %v", got, tc.want)
			}
		})
	}
}
