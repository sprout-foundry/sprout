package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
)

func TestGetProviderDisplayName(t *testing.T) {
	tests := []struct {
		name     string
		provider api.ClientType
		want     string
	}{
		{
			name:     "OpenAI",
			provider: api.OpenAIClientType,
			want:     "OpenAI",
		},
		{
			name:     "Z.AI",
			provider: api.ZAIClientType,
			want:     "Z.AI",
		},
		{
			name:     "GLM Coding Plan",
			provider: api.ZAICodingClientType,
			want:     "GLM Coding Plan",
		},
		{
			name:     "DeepInfra",
			provider: api.DeepInfraClientType,
			want:     "DeepInfra",
		},
		{
			name:     "DeepSeek",
			provider: api.DeepSeekClientType,
			want:     "DeepSeek",
		},
		{
			name:     "OpenRouter",
			provider: api.OpenRouterClientType,
			want:     "OpenRouter",
		},
		{
			name:     "Ollama",
			provider: api.OllamaClientType,
			want:     "Ollama",
		},
		{
			name:     "Ollama Local",
			provider: api.OllamaLocalClientType,
			want:     "Ollama Local",
		},
		{
			name:     "Ollama Cloud",
			provider: api.OllamaCloudClientType,
			want:     "Ollama Cloud",
		},
		{
			name:     "LM Studio",
			provider: api.LMStudioClientType,
			want:     "LM Studio",
		},
		{
			name:     "Test (CI/Mock)",
			provider: api.TestClientType,
			want:     "Test (CI/Mock)",
		},
		{
			name:     "unknown provider",
			provider: api.ClientType("custom-provider"),
			want:     "custom-provider",
		},
		{
			name:     "empty provider",
			provider: api.ClientType(""),
			want:     "",
		},
		{
			name:     "provider with special characters",
			provider: api.ClientType("my_custom_provider-v2"),
			want:     "my_custom_provider-v2",
		},
		{
			name:     "provider with numbers",
			provider: api.ClientType("provider123"),
			want:     "provider123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getProviderDisplayName(tt.provider)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProviderDropdownItemDisplay(t *testing.T) {
	tests := []struct {
		name string
		item providerDropdownItem
		want string
	}{
		{
			name: "OpenAI provider",
			item: providerDropdownItem{
				provider:    api.OpenAIClientType,
				displayName: "OpenAI",
				available:   true,
			},
			want: "OpenAI",
		},
		{
			name: "Ollama Local provider",
			item: providerDropdownItem{
				provider:    api.OllamaLocalClientType,
				displayName: "Ollama Local",
				available:   true,
			},
			want: "Ollama Local",
		},
		{
			name: "custom provider",
			item: providerDropdownItem{
				provider:    api.ClientType("custom-provider"),
				displayName: "Custom Provider",
				available:   false,
			},
			want: "Custom Provider",
		},
		{
			name: "provider with special characters",
			item: providerDropdownItem{
				provider:    api.ClientType("my_provider-v2"),
				displayName: "My Provider V2",
				available:   true,
			},
			want: "My Provider V2",
		},
		{
			name: "empty display name",
			item: providerDropdownItem{
				provider:    api.OpenAIClientType,
				displayName: "",
				available:   true,
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.item.Display()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProviderDropdownItemSearchText(t *testing.T) {
	tests := []struct {
		name string
		item providerDropdownItem
		want string
	}{
		{
			name: "OpenAI provider",
			item: providerDropdownItem{
				provider:    api.OpenAIClientType,
				displayName: "OpenAI",
				available:   true,
			},
			want: "OpenAI",
		},
		{
			name: "Ollama Local provider",
			item: providerDropdownItem{
				provider:    api.OllamaLocalClientType,
				displayName: "Ollama Local",
				available:   true,
			},
			want: "Ollama Local",
		},
		{
			name: "multiple words",
			item: providerDropdownItem{
				provider:    api.TestClientType,
				displayName: "Test (CI/Mock)",
				available:   true,
			},
			want: "Test (CI/Mock)",
		},
		{
			name: "custom provider",
			item: providerDropdownItem{
				provider:    api.ClientType("custom-provider"),
				displayName: "Custom Provider",
				available:   false,
			},
			want: "Custom Provider",
		},
		{
			name: "empty display name",
			item: providerDropdownItem{
				provider:    api.OpenAIClientType,
				displayName: "",
				available:   true,
			},
			want: "",
		},
		{
			name: "provider with special characters in display",
			item: providerDropdownItem{
				provider:    api.ClientType("my_provider-v2"),
				displayName: "My Provider V2 (Local)",
				available:   true,
			},
			want: "My Provider V2 (Local)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.item.SearchText()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProviderDropdownItemValue(t *testing.T) {
	tests := []struct {
		name string
		item providerDropdownItem
		want api.ClientType
	}{
		{
			name: "OpenAI provider",
			item: providerDropdownItem{
				provider:    api.OpenAIClientType,
				displayName: "OpenAI",
				available:   true,
			},
			want: api.OpenAIClientType,
		},
		{
			name: "Ollama Local provider",
			item: providerDropdownItem{
				provider:    api.OllamaLocalClientType,
				displayName: "Ollama Local",
				available:   true,
			},
			want: api.OllamaLocalClientType,
		},
		{
			name: "custom provider",
			item: providerDropdownItem{
				provider:    api.ClientType("custom-provider"),
				displayName: "Custom Provider",
				available:   false,
			},
			want: api.ClientType("custom-provider"),
		},
		{
			name: "empty provider",
			item: providerDropdownItem{
				provider:    api.ClientType(""),
				displayName: "",
				available:   true,
			},
			want: api.ClientType(""),
		},
		{
			name: "provider with special characters",
			item: providerDropdownItem{
				provider:    api.ClientType("my_provider-v2"),
				displayName: "My Provider V2",
				available:   true,
			},
			want: api.ClientType("my_provider-v2"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.item.Value()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProviderDropdownItemAvailableField(t *testing.T) {
	tests := []struct {
		name      string
		item      providerDropdownItem
		wantAvail bool
	}{
		{
			name: "available provider",
			item: providerDropdownItem{
				provider:    api.OpenAIClientType,
				displayName: "OpenAI",
				available:   true,
			},
			wantAvail: true,
		},
		{
			name: "unavailable provider",
			item: providerDropdownItem{
				provider:    api.ClientType("custom-provider"),
				displayName: "Custom Provider",
				available:   false,
			},
			wantAvail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The available field is not used by any method, but we can still check it exists
			assert.Equal(t, tt.wantAvail, tt.item.available)
		})
	}
}

func TestProviderDropdownItemInterface(t *testing.T) {
	// Test that providerDropdownItem satisfies the expected interface
	item := providerDropdownItem{
		provider:    api.OpenAIClientType,
		displayName: "OpenAI",
		available:   true,
	}

	// Test Display method
	display := item.Display()
	assert.Equal(t, "OpenAI", display)

	// Test SearchText method
	searchText := item.SearchText()
	assert.Equal(t, "OpenAI", searchText)

	// Test Value method
	value := item.Value()
	assert.Equal(t, api.OpenAIClientType, value)

	// Test that value returns correct type
	_, isClientType := value.(api.ClientType)
	assert.True(t, isClientType, "Value() should return api.ClientType")
}

// SP-161g: /custom and /provider are different verbs, so they stay
// separate commands — but an unknown provider is almost always a custom
// endpoint the user has not registered, and the error should say so
// rather than leave them to discover /custom by accident.
func TestProvidersCommand_UnknownProviderPointsAtCustom(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}

	var buf bytes.Buffer
	p := &ProvidersCommand{}
	p.SetOutput(&buf)

	err := p.setProvider("definitely-not-a-provider-xyz", mgr, nil)
	if err == nil {
		t.Fatal("expected an error for an unknown provider")
	}
	// SP-167 made /provider the primary entry point for registering an
	// endpoint, so the pointer follows it.
	if !strings.Contains(err.Error(), "/provider add") {
		t.Errorf("error should point at /provider add, got: %v", err)
	}
}

// A user-registered endpoint is marked in the list so it is clear which
// entries came from /custom add and which ship built in.
func TestProvidersCommand_ListMarksCustomProviders(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}

	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			cfg.CustomProviders = map[string]configuration.CustomProviderConfig{}
		}
		cfg.CustomProviders["my-endpoint"] = configuration.CustomProviderConfig{
			Name:           "my-endpoint",
			Endpoint:       "https://example.invalid/v1",
			ModelName:      "some-model",
			RequiresAPIKey: false,
		}
		return nil
	}); err != nil {
		t.Fatalf("register custom provider: %v", err)
	}

	var buf bytes.Buffer
	p := &ProvidersCommand{}
	p.SetOutput(&buf)

	if err := p.listProviders(mgr); err != nil {
		t.Fatalf("listProviders: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "my-endpoint") {
		t.Errorf("custom provider missing from the list:\n%s", out)
	}
	if !strings.Contains(out, "[custom]") {
		t.Errorf("custom provider not marked as custom:\n%s", out)
	}
}

// SP-167: /provider add|remove|custom delegate to the custom-provider
// wizard, so /provider is the single entry point for everything
// provider-shaped.
//
// The regression this guards: the default branch of Execute treats an
// unknown first argument as a provider *name* to switch to. If the new
// subcommands were not matched first, `/provider add` would try to switch
// to a provider called "add" and fail with "unknown provider 'add'".
func TestProvidersCommand_CustomSubcommandsDoNotFallThroughToSwitch(t *testing.T) {
	type call struct {
		sub  string
		rest []string
	}
	for _, tc := range []struct {
		arg     string
		wantSub string
		wantArg []string
	}{
		{"add", "add", nil},
		{"remove", "remove", nil},
		{"rm", "remove", nil},
		{"delete", "remove", nil},
		{"custom", "list", nil},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			var got []call
			restore := customSubcommandRunner
			customSubcommandRunner = func(sub string, rest []string) error {
				got = append(got, call{sub: sub, rest: rest})
				return nil
			}
			defer func() { customSubcommandRunner = restore }()

			p := &ProvidersCommand{}
			// nil agent: the custom subcommands must not touch it.
			if err := p.Execute([]string{tc.arg}, nil); err != nil {
				t.Fatalf("/provider %s: %v", tc.arg, err)
			}

			if len(got) != 1 {
				t.Fatalf("/provider %s: expected exactly one delegated call, got %d (%v)", tc.arg, len(got), got)
			}
			if got[0].sub != tc.wantSub {
				t.Errorf("/provider %s delegated to %q, want %q", tc.arg, got[0].sub, tc.wantSub)
			}
			if len(got[0].rest) != len(tc.wantArg) {
				t.Errorf("/provider %s passed %v, want %v", tc.arg, got[0].rest, tc.wantArg)
			}
		})
	}
}

// `remove <name>` forwards the name to the wizard.
func TestProvidersCommand_RemoveForwardsProviderName(t *testing.T) {
	var gotRest []string
	restore := customSubcommandRunner
	customSubcommandRunner = func(sub string, rest []string) error {
		gotRest = rest
		return nil
	}
	defer func() { customSubcommandRunner = restore }()

	p := &ProvidersCommand{}
	if err := p.Execute([]string{"remove", "my-endpoint"}, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(gotRest) != 1 || gotRest[0] != "my-endpoint" {
		t.Errorf("expected [my-endpoint] forwarded, got %v", gotRest)
	}
}

// A real provider name must still switch (no regression from the new
// subcommand cases).
func TestProvidersCommand_RealProviderStillDispatchesToSwitch(t *testing.T) {
	var buf bytes.Buffer
	p := &ProvidersCommand{}
	p.SetOutput(&buf)

	// "definitely-not-a-provider-xyz" is not a subcommand, so it must reach
	// setProvider and produce that function's error.
	mgr, cleanup := configuration.NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}
	err := p.Execute([]string{"definitely-not-a-provider-xyz"}, agent.NewTestAgentWithConfigManager(mgr))
	if err == nil {
		t.Fatal("expected an error for an unknown provider name")
	}
	if !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("non-subcommand arg should reach setProvider, got: %v", err)
	}
}

// The new subcommands are offered in completion. (Provider-name
// completions need an agent; the subcommand set does not, so assert that
// without one.)
func TestProvidersCommand_CompleteIncludesCustomSubcommands(t *testing.T) {
	p := &ProvidersCommand{}

	first := p.Complete(nil, nil)
	for _, want := range []string{"add", "remove", "custom"} {
		if !containsStr(first, want) {
			t.Errorf("first-arg completion missing %q: %v", want, first)
		}
	}

	// Prefix filtering still applies to the new subcommands.
	got := p.Complete([]string{"a"}, nil)
	if !containsStr(got, "add") {
		t.Errorf("prefix \"a\" should match add, got: %v", got)
	}
}

// customProviderNames is the shared helper behind both /provider remove and
// /custom remove completion.
func TestCustomProviderNames(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			cfg.CustomProviders = map[string]configuration.CustomProviderConfig{}
		}
		cfg.CustomProviders["my-endpoint"] = configuration.CustomProviderConfig{
			Name: "my-endpoint", Endpoint: "https://example.invalid/v1",
		}
		return nil
	}); err != nil {
		t.Fatalf("register custom provider: %v", err)
	}

	all := customProviderNames(mgr, "")
	if !containsStr(all, "my-endpoint") {
		t.Errorf("expected my-endpoint, got: %v", all)
	}
	if got := customProviderNames(mgr, "zzz"); len(got) != 0 {
		t.Errorf("non-matching prefix should return nothing, got: %v", got)
	}
	if got := customProviderNames(nil, ""); len(got) != 0 {
		t.Errorf("nil manager should return nothing, got: %v", got)
	}
}

// `remove <TAB>` completes custom provider names only — not the subcommand
// set, which would otherwise match every name against an empty prefix.
func TestProvidersCommand_CompleteRemoveOffersOnlyCustomNames(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			cfg.CustomProviders = map[string]configuration.CustomProviderConfig{}
		}
		cfg.CustomProviders["my-endpoint"] = configuration.CustomProviderConfig{
			Name: "my-endpoint", Endpoint: "https://example.invalid/v1",
		}
		return nil
	}); err != nil {
		t.Fatalf("register custom provider: %v", err)
	}

	p := &ProvidersCommand{}
	// `remove <TAB>`: the trailing empty arg is the prefix being completed.
	got := p.Complete([]string{"remove", ""}, agent.NewTestAgentWithConfigManager(mgr))

	if !containsStr(got, "my-endpoint") {
		t.Errorf("expected my-endpoint, got: %v", got)
	}
	for _, unwanted := range []string{"add", "list", "select", "status", "custom"} {
		if containsStr(got, unwanted) {
			t.Errorf("remove completion should not offer subcommand %q: %v", unwanted, got)
		}
	}
}

func TestProvidersCommand_UsageDocumentsCustomSubcommands(t *testing.T) {
	p := &ProvidersCommand{}
	usage := p.Usage()
	for _, want := range []string{"/provider add", "/provider remove", "/provider custom"} {
		if !strings.Contains(usage, want) {
			t.Errorf("Usage() missing %q\n%s", want, usage)
		}
	}
}

// The list output offers the way to register an endpoint — the user who
// needs it is the one whose endpoint is not in the list.
func TestProvidersCommand_ListPointsAtAdd(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	if cleanup != nil {
		defer cleanup()
	}

	var buf bytes.Buffer
	p := &ProvidersCommand{}
	p.SetOutput(&buf)

	if err := p.listProviders(mgr); err != nil {
		t.Fatalf("listProviders: %v", err)
	}
	if !strings.Contains(buf.String(), "/provider add") {
		t.Errorf("list should point at /provider add:\n%s", buf.String())
	}
}

// /custom is deprecated but still functional: its docs point at /provider.
func TestCustomCommand_UsageMarksDeprecated(t *testing.T) {
	c := &CustomCommand{}
	usage := c.Usage()
	if !strings.Contains(usage, "Deprecated") {
		t.Errorf("Usage() should mark /custom deprecated:\n%s", usage)
	}
	for _, want := range []string{"/provider add", "/provider remove", "/provider custom"} {
		if !strings.Contains(usage, want) {
			t.Errorf("Usage() should point at %q\n%s", want, usage)
		}
	}
	if !strings.Contains(c.Description(), "deprecated") {
		t.Errorf("Description() should mark /custom deprecated: %q", c.Description())
	}
}

func containsStr(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
