package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestShellConfig_LoadWithShellSection(t *testing.T) {
	cfgJSON := `{
		"version": "2.0",
		"last_used_provider": "openai",
		"provider_models": {"openai": "gpt-4"},
		"provider_priority": ["openai"],
		"mcp": {"servers": {}},
		"shell": {
			"user_safe_patterns": [
				{"match": "my-deploy-script", "kind": "prefix"},
				{"match": "kubectl rollout", "kind": "prefix"}
			],
			"user_dangerous_patterns": [
				{"match": "terraform destroy", "kind": "prefix", "reason": "Production-destructive"},
				{"match": "helm uninstall", "kind": "prefix"}
			],
			"workspace_overlay": {
				"mode": "tighten_only"
			}
		}
	}`

	tmp := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmp)
	t.Setenv("SPROUT_CONFIG", tmp)

	configPath := filepath.Join(tmp, ConfigFileName)
	if err := os.WriteFile(configPath, []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	// Check safe patterns
	if len(cfg.Shell.UserSafePatterns) != 2 {
		t.Fatalf("expected 2 safe patterns, got %d", len(cfg.Shell.UserSafePatterns))
	}
	if cfg.Shell.UserSafePatterns[0].Match != "my-deploy-script" {
		t.Errorf("expected safe pattern[0] match 'my-deploy-script', got %q", cfg.Shell.UserSafePatterns[0].Match)
	}
	if cfg.Shell.UserSafePatterns[0].Kind != "prefix" {
		t.Errorf("expected safe pattern[0] kind 'prefix', got %q", cfg.Shell.UserSafePatterns[0].Kind)
	}
	if cfg.Shell.UserSafePatterns[1].Match != "kubectl rollout" {
		t.Errorf("expected safe pattern[1] match 'kubectl rollout', got %q", cfg.Shell.UserSafePatterns[1].Match)
	}

	// Check dangerous patterns
	if len(cfg.Shell.UserDangerousPatterns) != 2 {
		t.Fatalf("expected 2 dangerous patterns, got %d", len(cfg.Shell.UserDangerousPatterns))
	}
	if cfg.Shell.UserDangerousPatterns[0].Match != "terraform destroy" {
		t.Errorf("expected dangerous pattern[0] match 'terraform destroy', got %q", cfg.Shell.UserDangerousPatterns[0].Match)
	}
	if cfg.Shell.UserDangerousPatterns[0].Reason != "Production-destructive" {
		t.Errorf("expected dangerous pattern[0] reason 'Production-destructive', got %q", cfg.Shell.UserDangerousPatterns[0].Reason)
	}

	// Check workspace overlay
	if cfg.Shell.WorkspaceOverlay.Mode != "tighten_only" {
		t.Errorf("expected workspace overlay mode 'tighten_only', got %q", cfg.Shell.WorkspaceOverlay.Mode)
	}
}

func TestShellConfig_LoadWithoutShellSection(t *testing.T) {
	cfgJSON := `{
		"version": "2.0",
		"last_used_provider": "openai",
		"provider_models": {"openai": "gpt-4"},
		"provider_priority": ["openai"],
		"mcp": {"servers": {}}
	}`

	tmp := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmp)
	t.Setenv("SPROUT_CONFIG", tmp)

	configPath := filepath.Join(tmp, ConfigFileName)
	if err := os.WriteFile(configPath, []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	// Zero-value defaults — no error should be raised
	if cfg.Shell.UserSafePatterns != nil {
		t.Errorf("expected nil safe patterns, got %v", cfg.Shell.UserSafePatterns)
	}
	if cfg.Shell.UserDangerousPatterns != nil {
		t.Errorf("expected nil dangerous patterns, got %v", cfg.Shell.UserDangerousPatterns)
	}
	if cfg.Shell.WorkspaceOverlay.Mode != "" {
		t.Errorf("expected empty workspace overlay mode, got %q", cfg.Shell.WorkspaceOverlay.Mode)
	}
}

func TestShellConfig_Validate(t *testing.T) {
	tests := []struct {
		name       string
		shell      ShellConfig
		wantErr    bool
		validateFn func(ShellConfig) error
	}{
		{
			name: "valid prefix patterns",
			shell: ShellConfig{
				UserSafePatterns: []ShellPattern{
					{Match: "my-tool", Kind: "prefix"},
				},
				UserDangerousPatterns: []ShellPattern{
					{Match: "rm -rf", Kind: "prefix"},
				},
				WorkspaceOverlay: WorkspaceOverlayConfig{Mode: "tighten_only"},
			},
			wantErr: false,
		},
		{
			name: "valid regex patterns",
			shell: ShellConfig{
				UserSafePatterns: []ShellPattern{
					{Match: "^my-tool", Kind: "regex"},
				},
				WorkspaceOverlay: WorkspaceOverlayConfig{Mode: "trusted"},
			},
			wantErr: false,
		},
		{
			name: "empty kind normalizes to prefix",
			shell: ShellConfig{
				UserSafePatterns: []ShellPattern{
					{Match: "my-tool", Kind: ""},
				},
			},
			wantErr: false,
		},
		{
			name: "empty mode normalizes to tighten_only",
			shell: ShellConfig{
				WorkspaceOverlay: WorkspaceOverlayConfig{Mode: ""},
			},
			wantErr: false,
		},
		{
			name: "invalid kind returns error",
			shell: ShellConfig{
				UserSafePatterns: []ShellPattern{
					{Match: "my-tool", Kind: "foobar"},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid dangerous kind returns error",
			shell: ShellConfig{
				UserDangerousPatterns: []ShellPattern{
					{Match: "terraform destroy", Kind: "invalid"},
				},
			},
			wantErr: true,
		},
		{
			name:    "nil config validates without error",
			shell:   ShellConfig{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.shell.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestShellConfig_Validate_NormalizesKind(t *testing.T) {
	sc := ShellConfig{
		UserSafePatterns: []ShellPattern{
			{Match: "my-tool", Kind: ""},
		},
	}
	if err := sc.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sc.UserSafePatterns[0].Kind != "prefix" {
		t.Errorf("expected empty kind to normalize to 'prefix', got %q", sc.UserSafePatterns[0].Kind)
	}
}

func TestShellConfig_Validate_NormalizesMode(t *testing.T) {
	sc := ShellConfig{
		WorkspaceOverlay: WorkspaceOverlayConfig{Mode: ""},
	}
	if err := sc.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sc.WorkspaceOverlay.Mode != "tighten_only" {
		t.Errorf("expected empty mode to normalize to 'tighten_only', got %q", sc.WorkspaceOverlay.Mode)
	}
}

func TestShellConfig_JSONRoundTrip(t *testing.T) {
	original := ShellConfig{
		UserSafePatterns: []ShellPattern{
			{Match: "my-deploy", Kind: "prefix"},
			{Match: "kubectl .*", Kind: "regex"},
		},
		UserDangerousPatterns: []ShellPattern{
			{Match: "terraform destroy", Kind: "prefix", Reason: "Production-destructive"},
		},
		WorkspaceOverlay: WorkspaceOverlayConfig{Mode: "trusted"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ShellConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.UserSafePatterns) != 2 {
		t.Fatalf("expected 2 safe patterns after round-trip, got %d", len(decoded.UserSafePatterns))
	}
	if decoded.UserSafePatterns[0].Match != "my-deploy" {
		t.Errorf("safe pattern[0] mismatch: got %q", decoded.UserSafePatterns[0].Match)
	}
	if decoded.UserSafePatterns[1].Kind != "regex" {
		t.Errorf("safe pattern[1] kind mismatch: got %q", decoded.UserSafePatterns[1].Kind)
	}
	if decoded.UserDangerousPatterns[0].Reason != "Production-destructive" {
		t.Errorf("dangerous pattern reason mismatch: got %q", decoded.UserDangerousPatterns[0].Reason)
	}
	if decoded.WorkspaceOverlay.Mode != "trusted" {
		t.Errorf("workspace overlay mode mismatch: got %q", decoded.WorkspaceOverlay.Mode)
	}
}

func TestShellConfig_Merge(t *testing.T) {
	base := &Config{
		Shell: ShellConfig{
			UserSafePatterns: []ShellPattern{
				{Match: "base-safe", Kind: "prefix"},
			},
			WorkspaceOverlay: WorkspaceOverlayConfig{Mode: "tighten_only"},
		},
	}

	override := &Config{
		Shell: ShellConfig{
			UserSafePatterns: []ShellPattern{
				{Match: "override-safe", Kind: "prefix"},
			},
			UserDangerousPatterns: []ShellPattern{
				{Match: "override-dangerous", Kind: "prefix"},
			},
			WorkspaceOverlay: WorkspaceOverlayConfig{Mode: "trusted"},
		},
	}

	result := MergeConfig(base, override)

	// Safe patterns should be overridden
	if len(result.Shell.UserSafePatterns) != 1 {
		t.Fatalf("expected 1 safe pattern after merge, got %d", len(result.Shell.UserSafePatterns))
	}
	if result.Shell.UserSafePatterns[0].Match != "override-safe" {
		t.Errorf("expected merged safe pattern 'override-safe', got %q", result.Shell.UserSafePatterns[0].Match)
	}

	// Dangerous patterns should be merged in
	if len(result.Shell.UserDangerousPatterns) != 1 {
		t.Fatalf("expected 1 dangerous pattern after merge, got %d", len(result.Shell.UserDangerousPatterns))
	}
	if result.Shell.UserDangerousPatterns[0].Match != "override-dangerous" {
		t.Errorf("expected merged dangerous pattern 'override-dangerous', got %q", result.Shell.UserDangerousPatterns[0].Match)
	}

	// Workspace overlay mode should be overridden
	if result.Shell.WorkspaceOverlay.Mode != "trusted" {
		t.Errorf("expected merged workspace overlay mode 'trusted', got %q", result.Shell.WorkspaceOverlay.Mode)
	}
}

func TestShellConfig_Validate_FromConfig(t *testing.T) {
	// Verify Validate() on Config calls into Shell.Validate()
	cfg := &Config{
		Shell: ShellConfig{
			UserSafePatterns: []ShellPattern{
				{Match: "bad", Kind: "invalid_kind"},
			},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for invalid shell pattern kind, got nil")
	}
}
