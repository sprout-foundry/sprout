package tools

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// --- NewShellPolicy tests ---

func TestNewShellPolicy_EmptyConfig(t *testing.T) {
	p, err := NewShellPolicy(configuration.ShellConfig{})
	if err != nil {
		t.Fatalf("NewShellPolicy(empty) error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil ShellPolicy")
	}
	if len(p.safePatterns) != 0 {
		t.Errorf("expected 0 safePatterns, got %d", len(p.safePatterns))
	}
	if len(p.dangerousPatterns) != 0 {
		t.Errorf("expected 0 dangerousPatterns, got %d", len(p.dangerousPatterns))
	}
}

func TestNewShellPolicy_PrefixPatterns(t *testing.T) {
	cfg := configuration.ShellConfig{
		UserSafePatterns: []configuration.ShellPattern{
			{Match: "my-deploy-script", Kind: "prefix", Reason: "our deploy tool"},
			{Match: "curl ", Kind: "prefix"},
		},
		UserDangerousPatterns: []configuration.ShellPattern{
			{Match: "terraform destroy", Kind: "prefix", Reason: "destructive terraform"},
			{Match: "", Kind: "prefix"}, // empty match — should be ignored
		},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}
	if len(p.safePatterns) != 2 {
		t.Errorf("expected 2 safePatterns, got %d", len(p.safePatterns))
	}
	if len(p.dangerousPatterns) != 1 {
		t.Errorf("expected 1 dangerousPattern (empty ignored), got %d", len(p.dangerousPatterns))
	}
	if p.safePatterns[0].prefix != "my-deploy-script" {
		t.Errorf("safePatterns[0].prefix = %q, want %q", p.safePatterns[0].prefix, "my-deploy-script")
	}
	if p.safePatterns[0].reason != "our deploy tool" {
		t.Errorf("safePatterns[0].reason = %q, want %q", p.safePatterns[0].reason, "our deploy tool")
	}
}

func TestNewShellPolicy_RegexPatterns(t *testing.T) {
	cfg := configuration.ShellConfig{
		UserDangerousPatterns: []configuration.ShellPattern{
			{Match: `^kubect1 delete`, Kind: "regex", Reason: "k8s destruction"},
		},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}
	if len(p.dangerousPatterns) != 1 {
		t.Fatalf("expected 1 dangerousPattern, got %d", len(p.dangerousPatterns))
	}
	if p.dangerousPatterns[0].regex == nil {
		t.Fatal("regex pattern should have non-nil regex")
	}
}

func TestNewShellPolicy_BadRegex(t *testing.T) {
	cfg := configuration.ShellConfig{
		UserDangerousPatterns: []configuration.ShellPattern{
			{Match: `[invalid(`, Kind: "regex"},
		},
	}
	_, err := NewShellPolicy(cfg)
	if err == nil {
		t.Fatal("expected error for bad regex, got nil")
	}
}

func TestNewShellPolicy_PrefixCaseInsensitive(t *testing.T) {
	cfg := configuration.ShellConfig{
		UserSafePatterns: []configuration.ShellPattern{
			{Match: "MY-DEPLOY", Kind: "prefix"},
		},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}
	if p.safePatterns[0].prefix != "my-deploy" {
		t.Errorf("prefix should be lowercased, got %q", p.safePatterns[0].prefix)
	}
}

// --- SetShellPolicy / GetShellPolicy tests ---

func TestSetAndGetShellPolicy(t *testing.T) {
	// Save current policy and restore after test
	old := GetShellPolicy()
	defer SetShellPolicy(old)

	cfg := configuration.ShellConfig{
		UserSafePatterns: []configuration.ShellPattern{
			{Match: "foo", Kind: "prefix"},
		},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}

	SetShellPolicy(p)
	got := GetShellPolicy()
	if got != p {
		t.Error("GetShellPolicy did not return the set policy")
	}

	// Verify nil reset
	SetShellPolicy(nil)
	got = GetShellPolicy()
	if got != nil {
		t.Errorf("GetShellPolicy after nil set = %+v, want nil", got)
	}
}

// --- applyTo tests ---

func TestApplyTo_NoPolicy(t *testing.T) {
	// No policy set — classifyShellCommand should work as before
	if p := GetShellPolicy(); p != nil {
		t.Skip("globalShellPolicy is already set; skipping")
	}
}

func TestApplyTo_UserSafeDowngradesCaution(t *testing.T) {
	SetShellPolicy(nil)
	defer SetShellPolicy(nil)

	cfg := configuration.ShellConfig{
		UserSafePatterns: []configuration.ShellPattern{
			{Match: "my-deploy-script", Kind: "prefix", Reason: "our deploy tool"},
		},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}

	// Simulate built-in CAUTION result for a shell command
	builtin := SecurityResult{
		Risk:         SecurityCaution,
		Reasoning:    "Command requires caution",
		ShouldPrompt: true,
		Category:     RiskCategoryUnknown,
	}

	result, matched := p.applyTo(builtin, "my-deploy-script --prod")
	if !matched {
		t.Fatal("expected user pattern to match")
	}
	if result.Risk != SecuritySafe {
		t.Errorf("risk = %v, want %v", result.Risk, SecuritySafe)
	}
	if result.ShouldPrompt {
		t.Error("should not prompt after user safe downgrades caution")
	}
	if result.ShouldBlock {
		t.Error("should not block after user safe downgrades caution")
	}
	if result.IsHardBlock {
		t.Error("should not hard-block after user safe downgrades caution")
	}
	if result.Reasoning == "" {
		t.Error("reasoning should not be empty")
	}
}

func TestApplyTo_UserSafeDoesNotDowngradeDangerous(t *testing.T) {
	defer SetShellPolicy(nil)

	cfg := configuration.ShellConfig{
		UserSafePatterns: []configuration.ShellPattern{
			{Match: "git push --force", Kind: "prefix"},
		},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}

	// Simulate built-in DANGEROUS result (no hard-block, just dangerous)
	builtin := SecurityResult{
		Risk:        SecurityDangerous,
		Reasoning:   "Dangerous operation",
		ShouldBlock: true,
	}

	result, matched := p.applyTo(builtin, "git push --force origin main")
	// User SAFE must NOT override built-in DANGEROUS.
	if result.Risk != SecurityDangerous {
		t.Errorf("risk = %v, want %v (user safe must not downgrade dangerous)", result.Risk, SecurityDangerous)
	}
	if result.ShouldBlock != true {
		t.Error("should still block built-in dangerous")
	}
	_ = matched // may or may not report a match, but the result must stay dangerous
}

func TestApplyTo_UserSafeIgnoredForHardBlock(t *testing.T) {
	defer SetShellPolicy(nil)

	cfg := configuration.ShellConfig{
		UserSafePatterns: []configuration.ShellPattern{
			{Match: "rm -rf /", Kind: "prefix", Reason: "I'm crazy"},
		},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}

	// Simulate built-in hard-block result (e.g. critical system operation)
	builtin := SecurityResult{
		Risk:        SecurityDangerous,
		Reasoning:   "Critical system operation detected",
		ShouldBlock: true,
		ShouldPrompt: true,
		IsHardBlock: true,
	}

	result, matched := p.applyTo(builtin, "rm -rf /")
	if matched {
		t.Fatal("user pattern should NOT match when built-in is hard-block")
	}
	if result != builtin {
		t.Error("result should be unchanged when built-in is hard-block")
	}
	if result.Risk != SecurityDangerous {
		t.Errorf("risk = %v, want %v", result.Risk, SecurityDangerous)
	}
	if !result.IsHardBlock {
		t.Error("hard-block must be preserved")
	}
}

func TestApplyTo_UserDangerousEscalatesCaution(t *testing.T) {
	defer SetShellPolicy(nil)

	cfg := configuration.ShellConfig{
		UserDangerousPatterns: []configuration.ShellPattern{
			{Match: "terraform destroy", Kind: "prefix", Reason: "will destroy infrastructure"},
		},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}

	// Simulate built-in CAUTION result for a command not in the built-in lists
	builtin := SecurityResult{
		Risk:         SecurityCaution,
		Reasoning:    "Command requires caution",
		ShouldPrompt: true,
		Category:     RiskCategoryUnknown,
	}

	result, matched := p.applyTo(builtin, "terraform destroy -auto-approve")
	if !matched {
		t.Fatal("expected user dangerous pattern to match")
	}
	if result.Risk != SecurityDangerous {
		t.Errorf("risk = %v, want %v", result.Risk, SecurityDangerous)
	}
	if result.ShouldBlock != true {
		t.Error("should block user dangerous pattern")
	}
	if result.ShouldPrompt != true {
		t.Error("should prompt for user dangerous pattern")
	}
	if result.IsHardBlock != true {
		t.Error("user dangerous pattern should set hard-block")
	}
	if result.RiskType != "user_dangerous_pattern" {
		t.Errorf("riskType = %q, want %q", result.RiskType, "user_dangerous_pattern")
	}
	if result.Reasoning == "" {
		t.Error("reasoning should include pattern reason")
	}
}

func TestApplyTo_UserDangerousEscalatesSafe(t *testing.T) {
	defer SetShellPolicy(nil)

	cfg := configuration.ShellConfig{
		UserDangerousPatterns: []configuration.ShellPattern{
			{Match: "dangerous-tool", Kind: "prefix", Reason: "known to cause issues"},
		},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}

	// Simulate built-in SAFE result
	builtin := SecurityResult{
		Risk:      SecuritySafe,
		Reasoning: "Safe operation",
	}

	result, matched := p.applyTo(builtin, "dangerous-tool --do-stuff")
	if !matched {
		t.Fatal("expected user dangerous pattern to match")
	}
	if result.Risk != SecurityDangerous {
		t.Errorf("risk = %v, want %v (user dangerous should escalate safe)", result.Risk, SecurityDangerous)
	}
	if result.IsHardBlock != true {
		t.Error("user dangerous should set hard-block")
	}
}

func TestApplyTo_UserDangerousIgnoredForHardBlock(t *testing.T) {
	defer SetShellPolicy(nil)

	cfg := configuration.ShellConfig{
		UserDangerousPatterns: []configuration.ShellPattern{
			{Match: "sudo", Kind: "prefix", Reason: "privilege escalation"},
		},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}

	// Simulate built-in hard-block (critical system op)
	builtin := SecurityResult{
		Risk:        SecurityDangerous,
		Reasoning:   "Critical system operation",
		IsHardBlock: true,
		ShouldBlock: true,
	}

	result, matched := p.applyTo(builtin, "sudo rm -rf /")
	if matched {
		t.Fatal("user pattern should NOT match when built-in is hard-block")
	}
	if result != builtin {
		t.Error("result should be unchanged when built-in is hard-block")
	}
}

func TestApplyTo_LongestMatchWins(t *testing.T) {
	defer SetShellPolicy(nil)

	cfg := configuration.ShellConfig{
		UserSafePatterns: []configuration.ShellPattern{
			{Match: "my", Kind: "prefix", Reason: "broad match"},
			{Match: "my-deploy-script", Kind: "prefix", Reason: "specific match"},
		},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}

	builtin := SecurityResult{
		Risk:         SecurityCaution,
		Reasoning:    "Command requires caution",
		ShouldPrompt: true,
	}

	result, matched := p.applyTo(builtin, "my-deploy-script --prod")
	if !matched {
		t.Fatal("expected match")
	}
	if result.Risk != SecuritySafe {
		t.Errorf("risk = %v, want %v", result.Risk, SecuritySafe)
	}
	// The reasoning should reference the longer/specific pattern
	if result.Reasoning == "User safe pattern matched: broad match" {
		t.Error("should have used the specific (longest) match, not the broad one")
	}
}

func TestApplyTo_LongestMatchWinsDangerous(t *testing.T) {
	defer SetShellPolicy(nil)

	cfg := configuration.ShellConfig{
		UserDangerousPatterns: []configuration.ShellPattern{
			{Match: "docker rm", Kind: "prefix", Reason: "generic container deletion"},
			{Match: "docker rm -f", Kind: "prefix", Reason: "force container deletion"},
		},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}

	builtin := SecurityResult{
		Risk:      SecuritySafe,
		Reasoning: "Safe operation",
	}

	result, matched := p.applyTo(builtin, "docker rm -f mycontainer")
	if !matched {
		t.Fatal("expected match")
	}
	if result.Risk != SecurityDangerous {
		t.Errorf("risk = %v, want %v", result.Risk, SecurityDangerous)
	}
	// Should use the longer, more specific pattern's reason
	if result.Reasoning == "User dangerous pattern matched: generic container deletion" {
		t.Error("should have used the longer (more specific) pattern's reason")
	}
}

func TestApplyTo_RegexPatternMatch(t *testing.T) {
	defer SetShellPolicy(nil)

	cfg := configuration.ShellConfig{
		UserDangerousPatterns: []configuration.ShellPattern{
			{Match: `kubectl\s+delete\s+.*-n\s+prod(\s|$)`, Kind: "regex", Reason: "deletion in prod namespace"},
		},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}

	builtin := SecurityResult{
		Risk:      SecurityCaution,
		Reasoning: "Command requires caution",
	}

	result, matched := p.applyTo(builtin, "kubectl delete pod mypod -n production")
	if matched {
		t.Error("should not match -n production (regex expects -n prod)")
	}
	if result.Risk != SecurityCaution {
		t.Errorf("risk changed unexpectedly: got %v", result.Risk)
	}

	// Now test matching regex
	result, matched = p.applyTo(builtin, "kubectl delete svc myservice -n prod")
	if !matched {
		t.Fatal("expected regex match")
	}
	if result.Risk != SecurityDangerous {
		t.Errorf("risk = %v, want %v", result.Risk, SecurityDangerous)
	}
}

func TestApplyTo_NoMatch(t *testing.T) {
	defer SetShellPolicy(nil)

	cfg := configuration.ShellConfig{
		UserSafePatterns:      []configuration.ShellPattern{},
		UserDangerousPatterns: []configuration.ShellPattern{},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}

	builtin := SecurityResult{
		Risk:      SecurityCaution,
		Reasoning: "Original reasoning",
	}

	result, matched := p.applyTo(builtin, "some-command")
	if matched {
		t.Fatal("should not match when no patterns configured")
	}
	if result != builtin {
		t.Error("result should be unchanged when no patterns match")
	}
}

// --- Integration: ClassifyToolCall with user policy ---

func TestClassifyToolCall_UserSafeOverridesCaution(t *testing.T) {
	// Save and restore global policy
	old := GetShellPolicy()
	defer SetShellPolicy(old)

	cfg := configuration.ShellConfig{
		UserSafePatterns: []configuration.ShellPattern{
			{Match: "my-deploy-script", Kind: "prefix", Reason: "our deploy tool"},
		},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}
	SetShellPolicy(p)

	// my-deploy-script would normally be classified CAUTION (unknown command)
	result := ClassifyToolCall("shell_command", map[string]interface{}{
		"command": "my-deploy-script --prod",
	})
	if result.Risk != SecuritySafe {
		t.Errorf("expected SAFE (user safe overrides CAUTION), got %v", result.Risk)
	}
}

func TestClassifyToolCall_UserDangerousBlocksCaution(t *testing.T) {
	old := GetShellPolicy()
	defer SetShellPolicy(old)

	cfg := configuration.ShellConfig{
		UserDangerousPatterns: []configuration.ShellPattern{
			{Match: "terraform destroy", Kind: "prefix", Reason: "will destroy infrastructure"},
		},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}
	SetShellPolicy(p)

	// terraform destroy would normally be CAUTION (unknown tool)
	result := ClassifyToolCall("shell_command", map[string]interface{}{
		"command": "terraform destroy -auto-approve",
	})
	if result.Risk != SecurityDangerous {
		t.Errorf("expected DANGEROUS, got %v", result.Risk)
	}
	if !result.ShouldBlock {
		t.Error("should block")
	}
	if !result.IsHardBlock {
		t.Error("should hard-block")
	}
}

func TestClassifyToolCall_UserSafeCannotOverrideHardBlock(t *testing.T) {
	old := GetShellPolicy()
	defer SetShellPolicy(old)

	cfg := configuration.ShellConfig{
		UserSafePatterns: []configuration.ShellPattern{
			{Match: "rm -rf /", Kind: "prefix", Reason: "I'm crazy"},
		},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}
	SetShellPolicy(p)

	// rm -rf / is a built-in hard-block (critical system operation)
	result := ClassifyToolCall("shell_command", map[string]interface{}{
		"command": "rm -rf /",
	})
	if result.Risk != SecurityDangerous {
		t.Errorf("expected DANGEROUS (hard-block preserved), got %v", result.Risk)
	}
	if !result.IsHardBlock {
		t.Error("hard-block must be preserved")
	}
	if !result.ShouldBlock {
		t.Error("should block")
	}
}

func TestClassifyToolCall_UserSafeCannotOverrideBuiltInDangerous(t *testing.T) {
	old := GetShellPolicy()
	defer SetShellPolicy(old)

	cfg := configuration.ShellConfig{
		UserSafePatterns: []configuration.ShellPattern{
			{Match: "git push", Kind: "prefix", Reason: "we trust our pushes"},
		},
	}
	p, err := NewShellPolicy(cfg)
	if err != nil {
		t.Fatalf("NewShellPolicy error: %v", err)
	}
	SetShellPolicy(p)

	// git push --force is built-in DANGEROUS (not a hard-block, but still
	// DANGEROUS) — user safe must never downgrade a built-in DANGEROUS.
	result := ClassifyToolCall("shell_command", map[string]interface{}{
		"command": "git push --force origin main",
	})
	if result.Risk != SecurityDangerous {
		t.Errorf("expected DANGEROUS (user safe must not override built-in dangerous), got %v", result.Risk)
	}
}
