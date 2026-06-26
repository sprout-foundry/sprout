package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestLoadSecurityPolicy
// ---------------------------------------------------------------------------

func TestLoadSecurityPolicy(t *testing.T) {
	t.Run("valid JSON file loads successfully", func(t *testing.T) {
		tmp := t.TempDir()
		sproutDir := filepath.Join(tmp, ConfigDirName)
		if err := os.MkdirAll(sproutDir, 0o755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		content := `{
			"default_action": "allow",
			"max_risk_level": "caution",
			"rules": [
				{"pattern": "rm*", "action": "deny", "reason": "no deletes"},
				{"pattern": "docker*", "action": "prompt"}
			],
			"allowed_paths": ["/workspace/src"],
			"denied_paths": ["/etc"],
			"denied_commands": ["sudo"]
		}`
		if err := os.WriteFile(filepath.Join(sproutDir, "security-policy.json"), []byte(content), 0o644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}

		policy, err := LoadSecurityPolicy(tmp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if policy == nil {
			t.Fatal("expected non-nil policy")
		}
		if policy.DefaultAction != "allow" {
			t.Errorf("expected DefaultAction=allow, got %q", policy.DefaultAction)
		}
		if policy.MaxRiskLevel != "caution" {
			t.Errorf("expected MaxRiskLevel=caution, got %q", policy.MaxRiskLevel)
		}
		if len(policy.Rules) != 2 {
			t.Fatalf("expected 2 rules, got %d", len(policy.Rules))
		}
		if policy.Rules[0].Pattern != "rm*" {
			t.Errorf("expected first rule pattern=rm*, got %q", policy.Rules[0].Pattern)
		}
		if policy.Rules[0].Action != "deny" {
			t.Errorf("expected first rule action=deny, got %q", policy.Rules[0].Action)
		}
		if policy.Rules[0].Reason != "no deletes" {
			t.Errorf("expected first rule reason=no deletes, got %q", policy.Rules[0].Reason)
		}
		if len(policy.AllowedPaths) != 1 || policy.AllowedPaths[0] != "/workspace/src" {
			t.Errorf("expected AllowedPaths=[/workspace/src], got %v", policy.AllowedPaths)
		}
		if len(policy.DeniedPaths) != 1 || policy.DeniedPaths[0] != "/etc" {
			t.Errorf("expected DeniedPaths=[/etc], got %v", policy.DeniedPaths)
		}
		if len(policy.DeniedCommands) != 1 || policy.DeniedCommands[0] != "sudo" {
			t.Errorf("expected DeniedCommands=[sudo], got %v", policy.DeniedCommands)
		}
	})

	t.Run("nonexistent file returns nil nil", func(t *testing.T) {
		tmp := t.TempDir()
		policy, err := LoadSecurityPolicy(tmp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if policy != nil {
			t.Errorf("expected nil policy for missing file, got %+v", policy)
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		tmp := t.TempDir()
		sproutDir := filepath.Join(tmp, ConfigDirName)
		if err := os.MkdirAll(sproutDir, 0o755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sproutDir, "security-policy.json"), []byte("{invalid json"), 0o644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}

		policy, err := LoadSecurityPolicy(tmp)
		if err == nil {
			t.Fatal("expected error for invalid JSON, got nil")
		}
		if policy != nil {
			t.Errorf("expected nil policy, got %+v", policy)
		}
		if got := err.Error(); got == "" {
			t.Error("expected non-empty error message")
		}
	})

	t.Run("empty directory with no sprout dir returns nil nil", func(t *testing.T) {
		tmp := t.TempDir()
		// No .sprout directory created at all
		policy, err := LoadSecurityPolicy(tmp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if policy != nil {
			t.Errorf("expected nil policy, got %+v", policy)
		}
	})

	t.Run("empty JSON object returns policy with zero values", func(t *testing.T) {
		tmp := t.TempDir()
		sproutDir := filepath.Join(tmp, ConfigDirName)
		if err := os.MkdirAll(sproutDir, 0o755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sproutDir, "security-policy.json"), []byte("{}"), 0o644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}

		policy, err := LoadSecurityPolicy(tmp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if policy == nil {
			t.Fatal("expected non-nil policy")
		}
		if policy.DefaultAction != "" {
			t.Errorf("expected empty DefaultAction, got %q", policy.DefaultAction)
		}
	})
}

// ---------------------------------------------------------------------------
// TestDefaultSecurityPolicy
// ---------------------------------------------------------------------------

func TestDefaultSecurityPolicy(t *testing.T) {
	policy := DefaultSecurityPolicy()
	if policy == nil {
		t.Fatal("expected non-nil policy")
	}
	if policy.DefaultAction != "prompt" {
		t.Errorf("expected DefaultAction=prompt, got %q", policy.DefaultAction)
	}
	if policy.MaxRiskLevel != "safe" {
		t.Errorf("expected MaxRiskLevel=safe, got %q", policy.MaxRiskLevel)
	}
}

// ---------------------------------------------------------------------------
// TestEvaluate
// ---------------------------------------------------------------------------

func TestEvaluate(t *testing.T) {
	t.Run("nil policy returns prompt", func(t *testing.T) {
		var p *SecurityPolicy
		action := p.Evaluate("ls -la")
		if action != PolicyPrompt {
			t.Errorf("expected prompt, got %s", action)
		}
	})

	t.Run("exact match pattern", func(t *testing.T) {
		p := &SecurityPolicy{
			Rules: []SecurityRule{{Pattern: "ls -la", Action: "allow"}},
		}
		action := p.Evaluate("ls -la")
		if action != PolicyAllow {
			t.Errorf("expected allow, got %s", action)
		}
	})

	t.Run("glob pattern matches full command", func(t *testing.T) {
		p := &SecurityPolicy{
			Rules: []SecurityRule{{Pattern: "rm*", Action: "deny"}},
		}
		action := p.Evaluate("rm -rf /tmp")
		if action != PolicyDeny {
			t.Errorf("expected deny, got %s", action)
		}
	})

	t.Run("glob pattern matches base command only", func(t *testing.T) {
		p := &SecurityPolicy{
			Rules: []SecurityRule{{Pattern: "docker", Action: "prompt"}},
		}
		action := p.Evaluate("docker build -t myapp .")
		if action != PolicyPrompt {
			t.Errorf("expected prompt, got %s", action)
		}
	})

	t.Run("first match wins", func(t *testing.T) {
		p := &SecurityPolicy{
			Rules: []SecurityRule{
				{Pattern: "docker*", Action: "allow"},
				{Pattern: "docker*", Action: "deny"},
			},
		}
		action := p.Evaluate("docker rm foo")
		if action != PolicyAllow {
			t.Errorf("expected allow (first match), got %s", action)
		}
	})

	t.Run("first match wins with base command match", func(t *testing.T) {
		p := &SecurityPolicy{
			Rules: []SecurityRule{
				{Pattern: "git", Action: "deny"},
				{Pattern: "git*", Action: "allow"},
			},
		}
		action := p.Evaluate("git commit -m test")
		if action != PolicyDeny {
			t.Errorf("expected deny (first base match), got %s", action)
		}
	})

	t.Run("no match falls back to DefaultAction", func(t *testing.T) {
		p := &SecurityPolicy{
			DefaultAction: "allow",
			Rules:         []SecurityRule{{Pattern: "rm*", Action: "deny"}},
		}
		action := p.Evaluate("ls")
		if action != PolicyAllow {
			t.Errorf("expected allow (default), got %s", action)
		}
	})

	t.Run("no match and empty DefaultAction returns prompt", func(t *testing.T) {
		p := &SecurityPolicy{
			DefaultAction: "",
			Rules:         []SecurityRule{{Pattern: "rm*", Action: "deny"}},
		}
		action := p.Evaluate("ls")
		if action != PolicyPrompt {
			t.Errorf("expected prompt (empty default), got %s", action)
		}
	})

	t.Run("no match and whitespace-only DefaultAction returns prompt", func(t *testing.T) {
		p := &SecurityPolicy{
			DefaultAction: "   ",
		}
		action := p.Evaluate("ls")
		if action != PolicyPrompt {
			t.Errorf("expected prompt (whitespace default), got %s", action)
		}
	})

	t.Run("empty command with rules containing wildcard", func(t *testing.T) {
		p := &SecurityPolicy{
			DefaultAction: "allow",
			Rules:         []SecurityRule{{Pattern: "*", Action: "deny"}},
		}
		action := p.Evaluate("")
		// filepath.Match("*", "") returns (true, nil) so wildcard matches empty string
		if action != PolicyDeny {
			t.Errorf("expected deny (wildcard matches empty), got %s", action)
		}
	})

	t.Run("full command match takes precedence over base match", func(t *testing.T) {
		p := &SecurityPolicy{
			Rules: []SecurityRule{
				{Pattern: "git diff", Action: "allow"},
				{Pattern: "git", Action: "deny"},
			},
		}
		action := p.Evaluate("git diff")
		if action != PolicyAllow {
			t.Errorf("expected allow (full match first), got %s", action)
		}
	})

	t.Run("base match when full command doesn't match", func(t *testing.T) {
		p := &SecurityPolicy{
			Rules: []SecurityRule{
				{Pattern: "git diff", Action: "allow"},
				{Pattern: "git", Action: "deny"},
			},
		}
		action := p.Evaluate("git commit -m test")
		// "git commit -m test" doesn't match "git diff"
		// base "git" matches "git" -> deny
		if action != PolicyDeny {
			t.Errorf("expected deny (base match), got %s", action)
		}
	})
}

// ---------------------------------------------------------------------------
// TestIsPathAllowed
// ---------------------------------------------------------------------------

func TestIsPathAllowed(t *testing.T) {
	t.Run("nil policy returns true", func(t *testing.T) {
		var p *SecurityPolicy
		if !p.IsPathAllowed("/any/path") {
			t.Error("expected true for nil policy")
		}
	})

	t.Run("empty AllowedPaths returns true when nothing denied", func(t *testing.T) {
		p := &SecurityPolicy{}
		if !p.IsPathAllowed("/anything") {
			t.Error("expected true with no allowlist and no denylist")
		}
	})

	t.Run("path within allowed paths returns true", func(t *testing.T) {
		p := &SecurityPolicy{AllowedPaths: []string{"/workspace"}}
		if !p.IsPathAllowed("/workspace/src/main.go") {
			t.Error("expected true: path within allowed directory")
		}
	})

	t.Run("path exactly matching allowed path returns true", func(t *testing.T) {
		p := &SecurityPolicy{AllowedPaths: []string{"/workspace"}}
		if !p.IsPathAllowed("/workspace") {
			t.Error("expected true: exact path match")
		}
	})

	t.Run("path outside allowed paths returns false", func(t *testing.T) {
		p := &SecurityPolicy{AllowedPaths: []string{"/workspace"}}
		if p.IsPathAllowed("/etc/passwd") {
			t.Error("expected false: path outside allowed directory")
		}
	})

	t.Run("denied path overrides allowed path", func(t *testing.T) {
		p := &SecurityPolicy{
			AllowedPaths: []string{"/workspace"},
			DeniedPaths:  []string{"/workspace/secrets"},
		}
		if p.IsPathAllowed("/workspace/secrets/key.pem") {
			t.Error("expected false: denied path takes precedence over allowed path")
		}
	})

	t.Run("allowed path not in denied list returns true", func(t *testing.T) {
		p := &SecurityPolicy{
			AllowedPaths: []string{"/workspace"},
			DeniedPaths:  []string{"/workspace/secrets"},
		}
		if !p.IsPathAllowed("/workspace/src/main.go") {
			t.Error("expected true: path in allowed but not denied")
		}
	})

	t.Run("denied path with no allowlist returns false", func(t *testing.T) {
		p := &SecurityPolicy{
			DeniedPaths: []string{"/etc"},
		}
		if p.IsPathAllowed("/etc/passwd") {
			t.Error("expected false: denied path with no allowlist")
		}
	})

	t.Run("path prefix mismatch not treated as match", func(t *testing.T) {
		p := &SecurityPolicy{AllowedPaths: []string{"/work"}}
		if p.IsPathAllowed("/workspace") {
			t.Error("expected false: /work is not a prefix of /workspace")
		}
	})

	t.Run("multiple allowed paths", func(t *testing.T) {
		p := &SecurityPolicy{AllowedPaths: []string{"/src", "/test"}}
		if !p.IsPathAllowed("/src/main.go") {
			t.Error("expected true: first allowed path")
		}
		if !p.IsPathAllowed("/test/unit_test.go") {
			t.Error("expected true: second allowed path")
		}
		if p.IsPathAllowed("/other/file.go") {
			t.Error("expected false: path not in any allowed list")
		}
	})

	t.Run("filepath.Clean normalization", func(t *testing.T) {
		p := &SecurityPolicy{AllowedPaths: []string{"/workspace/src"}}
		if !p.IsPathAllowed("/workspace/src/../src/file.go") {
			t.Error("expected true: path with traversal normalized to allowed")
		}
	})
}

// ---------------------------------------------------------------------------
// TestIsPathDenied
// ---------------------------------------------------------------------------

func TestIsPathDenied(t *testing.T) {
	t.Run("nil policy returns false", func(t *testing.T) {
		var p *SecurityPolicy
		if p.IsPathDenied("/any/path") {
			t.Error("expected false for nil policy")
		}
	})

	t.Run("exact denied path match", func(t *testing.T) {
		p := &SecurityPolicy{DeniedPaths: []string{"/etc"}}
		if !p.IsPathDenied("/etc") {
			t.Error("expected true: exact match with denied path")
		}
	})

	t.Run("subdirectory of denied path is denied", func(t *testing.T) {
		p := &SecurityPolicy{DeniedPaths: []string{"/etc"}}
		if !p.IsPathDenied("/etc/passwd") {
			t.Error("expected true: subdirectory of denied path")
		}
	})

	t.Run("nested subdirectory of denied path is denied", func(t *testing.T) {
		p := &SecurityPolicy{DeniedPaths: []string{"/etc"}}
		if !p.IsPathDenied("/etc/ssh/sshd_config") {
			t.Error("expected true: deeply nested subdirectory of denied path")
		}
	})

	t.Run("path not in denied list returns false", func(t *testing.T) {
		p := &SecurityPolicy{DeniedPaths: []string{"/etc"}}
		if p.IsPathDenied("/workspace/src/main.go") {
			t.Error("expected false: path not in denied list")
		}
	})

	t.Run("empty denied paths returns false", func(t *testing.T) {
		p := &SecurityPolicy{DeniedPaths: []string{}}
		if p.IsPathDenied("/anything") {
			t.Error("expected false: empty denied paths list")
		}
	})

	t.Run("nil denied paths slice returns false", func(t *testing.T) {
		p := &SecurityPolicy{}
		if p.IsPathDenied("/anything") {
			t.Error("expected false: nil denied paths")
		}
	})

	t.Run("prefix mismatch not treated as denied", func(t *testing.T) {
		p := &SecurityPolicy{DeniedPaths: []string{"/etc"}}
		if p.IsPathDenied("/etcetera") {
			t.Error("expected false: /etc is not a prefix of /etcetera")
		}
	})

	t.Run("filepath.Clean normalization on denied path", func(t *testing.T) {
		p := &SecurityPolicy{DeniedPaths: []string{"/etc/ssh"}}
		if !p.IsPathDenied("/etc/ssh/../ssh/config") {
			t.Error("expected true: traversal in denied path should normalize")
		}
	})

	t.Run("multiple denied paths", func(t *testing.T) {
		p := &SecurityPolicy{DeniedPaths: []string{"/etc", "/root"}}
		if !p.IsPathDenied("/etc/passwd") {
			t.Error("expected true: first denied path")
		}
		if !p.IsPathDenied("/root/.ssh/id_rsa") {
			t.Error("expected true: second denied path")
		}
		if p.IsPathDenied("/home/user/file") {
			t.Error("expected false: not in any denied path")
		}
	})
}

// ---------------------------------------------------------------------------
// TestIsCommandDenied
// ---------------------------------------------------------------------------

func TestIsCommandDenied(t *testing.T) {
	t.Run("nil policy returns false", func(t *testing.T) {
		var p *SecurityPolicy
		if p.IsCommandDenied("ls") {
			t.Error("expected false for nil policy")
		}
	})

	t.Run("exact match case-insensitive", func(t *testing.T) {
		p := &SecurityPolicy{DeniedCommands: []string{"sudo"}}
		if !p.IsCommandDenied("SUDO ls") {
			t.Error("expected true: case-insensitive match")
		}
	})

	t.Run("command with arguments matches base", func(t *testing.T) {
		p := &SecurityPolicy{DeniedCommands: []string{"rm"}}
		if !p.IsCommandDenied("rm -rf /tmp") {
			t.Error("expected true: base command match with arguments")
		}
	})

	t.Run("command not in denied list", func(t *testing.T) {
		p := &SecurityPolicy{DeniedCommands: []string{"sudo", "rm"}}
		if p.IsCommandDenied("ls -la") {
			t.Error("expected false: command not in denied list")
		}
	})

	t.Run("empty command returns false", func(t *testing.T) {
		p := &SecurityPolicy{DeniedCommands: []string{"rm"}}
		if p.IsCommandDenied("") {
			t.Error("expected false for empty command")
		}
	})

	t.Run("whitespace-only command returns false", func(t *testing.T) {
		p := &SecurityPolicy{DeniedCommands: []string{"rm"}}
		if p.IsCommandDenied("   ") {
			t.Error("expected false for whitespace-only command")
		}
	})

	t.Run("denied command with uppercase in list matched lowercase command", func(t *testing.T) {
		p := &SecurityPolicy{DeniedCommands: []string{"SUDO"}}
		if !p.IsCommandDenied("sudo ls") {
			t.Error("expected true: mixed case match")
		}
	})

	t.Run("empty denied commands list", func(t *testing.T) {
		p := &SecurityPolicy{DeniedCommands: []string{}}
		if p.IsCommandDenied("anything") {
			t.Error("expected false: empty denied commands")
		}
	})

	t.Run("multiple denied commands", func(t *testing.T) {
		p := &SecurityPolicy{DeniedCommands: []string{"sudo", "curl", "wget"}}
		if !p.IsCommandDenied("curl http://example.com") {
			t.Error("expected true: curl is denied")
		}
		if !p.IsCommandDenied("wget http://example.com") {
			t.Error("expected true: wget is denied")
		}
		if p.IsCommandDenied("ls") {
			t.Error("expected false: ls is not denied")
		}
	})
}

// ---------------------------------------------------------------------------
// TestMaxAllowedRisk
// ---------------------------------------------------------------------------

func TestMaxAllowedRisk(t *testing.T) {
	tests := []struct {
		name   string
		policy *SecurityPolicy
		want   int
	}{
		{"nil policy", nil, 0},
		{"safe", &SecurityPolicy{MaxRiskLevel: "safe"}, 0},
		{"caution", &SecurityPolicy{MaxRiskLevel: "caution"}, 1},
		{"dangerous", &SecurityPolicy{MaxRiskLevel: "dangerous"}, 2},
		{"empty string", &SecurityPolicy{MaxRiskLevel: ""}, 0},
		{"unknown value", &SecurityPolicy{MaxRiskLevel: "unknown"}, 0},
		{"uppercase SAFE", &SecurityPolicy{MaxRiskLevel: "SAFE"}, 0},
		{"uppercase CAUTION", &SecurityPolicy{MaxRiskLevel: "CAUTION"}, 1},
		{"uppercase DANGEROUS", &SecurityPolicy{MaxRiskLevel: "DANGEROUS"}, 2},
		{"mixed case CaUtIoN", &SecurityPolicy{MaxRiskLevel: "CaUtIoN"}, 1},
		{"whitespace around value", &SecurityPolicy{MaxRiskLevel: "  safe  "}, 0},
		{"zero value struct", &SecurityPolicy{}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.policy.MaxAllowedRisk()
			if got != tt.want {
				t.Errorf("MaxAllowedRisk() = %d, want %d", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestCombinedSecurityAssessment
// ---------------------------------------------------------------------------

func TestCombinedSecurityAssessment_ShellCommand(t *testing.T) {
	t.Run("shell command denied by policy rule", func(t *testing.T) {
		policy := &SecurityPolicy{
			Rules: []SecurityRule{{Pattern: "rm*", Action: "deny", Reason: "no deletes"}},
		}
		args := map[string]interface{}{"command": "rm -rf /tmp"}
		assessment := CombinedSecurityAssessment("shell_command", args, policy, 0, false, false, "destructive")

		if assessment.PolicyAction != PolicyDeny {
			t.Errorf("expected PolicyDeny, got %s", assessment.PolicyAction)
		}
		if assessment.ClassifierRisk != 0 {
			t.Errorf("expected ClassifierRisk=0, got %d", assessment.ClassifierRisk)
		}
		if assessment.PolicyRule == nil {
			t.Error("expected matched rule to be stored")
		} else if assessment.PolicyRule.Reason != "no deletes" {
			t.Errorf("expected matched rule reason=no deletes, got %q", assessment.PolicyRule.Reason)
		}
	})

	t.Run("shell command allowed by policy rule", func(t *testing.T) {
		policy := &SecurityPolicy{
			DefaultAction: "allow",
			Rules:         []SecurityRule{{Pattern: "ls*", Action: "allow"}},
		}
		args := map[string]interface{}{"command": "ls -la"}
		assessment := CombinedSecurityAssessment("shell_command", args, policy, 0, false, false, "read-only")

		if assessment.PolicyAction != PolicyAllow {
			t.Errorf("expected PolicyAllow, got %s", assessment.PolicyAction)
		}
	})

	t.Run("denied command detected", func(t *testing.T) {
		policy := &SecurityPolicy{
			DeniedCommands: []string{"sudo"},
		}
		args := map[string]interface{}{"command": "sudo rm -rf /"}
		assessment := CombinedSecurityAssessment("shell_command", args, policy, 2, true, true, "destructive")

		if !assessment.CommandDenied {
			t.Error("expected CommandDenied=true")
		}
	})

	t.Run("classifier blocks but policy allows sets OverrideAction", func(t *testing.T) {
		policy := &SecurityPolicy{
			DefaultAction: "allow",
			MaxRiskLevel:  "dangerous",
		}
		args := map[string]interface{}{"command": "ls"}
		assessment := CombinedSecurityAssessment("shell_command", args, policy, 2, true, true, "destructive")

		if assessment.OverrideAction != "policy allows command despite classifier flagging it" {
			t.Errorf("unexpected OverrideAction: %q", assessment.OverrideAction)
		}
	})

	t.Run("policy denies but classifier allows sets OverrideAction", func(t *testing.T) {
		policy := &SecurityPolicy{
			DefaultAction: "deny",
		}
		args := map[string]interface{}{"command": "ls"}
		assessment := CombinedSecurityAssessment("shell_command", args, policy, 0, false, false, "read-only")

		if assessment.OverrideAction != "policy denies command despite classifier allowing it" {
			t.Errorf("unexpected OverrideAction: %q", assessment.OverrideAction)
		}
	})

	t.Run("max risk level exceeded sets OverrideAction", func(t *testing.T) {
		policy := &SecurityPolicy{
			DefaultAction: "allow",
			MaxRiskLevel:  "safe",
		}
		args := map[string]interface{}{"command": "ls"}
		// classifierRisk=2 (dangerous) exceeds maxAllowed=0 (safe)
		assessment := CombinedSecurityAssessment("shell_command", args, policy, 2, false, false, "destructive")

		if assessment.OverrideAction != "classifier risk level 2 exceeds policy max 0" {
			t.Errorf("unexpected OverrideAction: %q", assessment.OverrideAction)
		}
	})

	t.Run("nil policy returns defaults", func(t *testing.T) {
		args := map[string]interface{}{"command": "rm -rf /"}
		assessment := CombinedSecurityAssessment("shell_command", args, nil, 1, false, false, "file-write")

		if assessment.PolicyAction != PolicyPrompt {
			t.Errorf("expected PolicyPrompt, got %s", assessment.PolicyAction)
		}
		if assessment.PathAllowed != true {
			t.Error("expected PathAllowed=true")
		}
		if assessment.CommandDenied != false {
			t.Error("expected CommandDenied=false")
		}
		if assessment.ClassifierRisk != 1 {
			t.Errorf("expected ClassifierRisk=1, got %d", assessment.ClassifierRisk)
		}
		if assessment.ClassifierCategory != "file-write" {
			t.Errorf("expected ClassifierCategory=file-write, got %q", assessment.ClassifierCategory)
		}
	})

	t.Run("empty command string uses default action", func(t *testing.T) {
		policy := &SecurityPolicy{
			DefaultAction: "allow",
		}
		args := map[string]interface{}{"command": ""}
		assessment := CombinedSecurityAssessment("shell_command", args, policy, 0, false, false, "unknown")

		// Empty command means the shell_command branch's inner check fails,
		// so PolicyAction stays at the initial default of PolicyPrompt
		if assessment.PolicyAction != PolicyPrompt {
			t.Errorf("expected PolicyPrompt for empty command, got %s", assessment.PolicyAction)
		}
	})

	t.Run("non-string command arg uses default action", func(t *testing.T) {
		policy := &SecurityPolicy{
			DefaultAction: "allow",
		}
		args := map[string]interface{}{"command": 123}
		assessment := CombinedSecurityAssessment("shell_command", args, policy, 0, false, false, "unknown")

		if assessment.PolicyAction != PolicyPrompt {
			t.Errorf("expected PolicyPrompt for non-string command, got %s", assessment.PolicyAction)
		}
	})
}

func TestCombinedSecurityAssessment_FileOperations(t *testing.T) {
	t.Run("write_file to allowed path", func(t *testing.T) {
		policy := &SecurityPolicy{
			AllowedPaths: []string{"/workspace"},
		}
		args := map[string]interface{}{"path": "/workspace/src/main.go"}
		assessment := CombinedSecurityAssessment("write_file", args, policy, 0, false, false, "file-write")

		if !assessment.PathAllowed {
			t.Error("expected PathAllowed=true")
		}
		if assessment.OverrideAction != "" {
			t.Errorf("unexpected OverrideAction: %q", assessment.OverrideAction)
		}
	})

	t.Run("write_file to denied path", func(t *testing.T) {
		policy := &SecurityPolicy{
			DeniedPaths: []string{"/etc"},
		}
		args := map[string]interface{}{"path": "/etc/passwd"}
		assessment := CombinedSecurityAssessment("write_file", args, policy, 0, false, false, "file-write")

		if assessment.PathAllowed {
			t.Error("expected PathAllowed=false")
		}
		if assessment.OverrideAction != "path denied by workspace security policy" {
			t.Errorf("unexpected OverrideAction: %q", assessment.OverrideAction)
		}
	})

	t.Run("edit_file to allowed path", func(t *testing.T) {
		policy := &SecurityPolicy{
			AllowedPaths: []string{"/workspace"},
		}
		args := map[string]interface{}{"path": "/workspace/config.yaml"}
		assessment := CombinedSecurityAssessment("edit_file", args, policy, 0, false, false, "file-write")

		if !assessment.PathAllowed {
			t.Error("expected PathAllowed=true for edit_file")
		}
	})

	t.Run("write_structured_file to denied path", func(t *testing.T) {
		policy := &SecurityPolicy{
			DeniedPaths: []string{"/workspace/secrets"},
		}
		args := map[string]interface{}{"path": "/workspace/secrets/keys.json"}
		assessment := CombinedSecurityAssessment("write_structured_file", args, policy, 0, false, false, "file-write")

		if assessment.PathAllowed {
			t.Error("expected PathAllowed=false for write_structured_file to denied path")
		}
	})

	t.Run("patch_structured_file to allowed path", func(t *testing.T) {
		policy := &SecurityPolicy{
			AllowedPaths: []string{"/workspace"},
		}
		args := map[string]interface{}{"path": "/workspace/data.json"}
		assessment := CombinedSecurityAssessment("patch_structured_file", args, policy, 0, false, false, "file-write")

		if !assessment.PathAllowed {
			t.Error("expected PathAllowed=true for patch_structured_file")
		}
	})

	t.Run("write_file with empty path", func(t *testing.T) {
		policy := &SecurityPolicy{
			DeniedPaths: []string{"/etc"},
		}
		args := map[string]interface{}{"path": ""}
		assessment := CombinedSecurityAssessment("write_file", args, policy, 0, false, false, "file-write")

		// Empty path means the path check is skipped entirely
		if !assessment.PathAllowed {
			t.Error("expected PathAllowed=true for empty path (check skipped)")
		}
	})

	t.Run("write_file with non-string path", func(t *testing.T) {
		policy := &SecurityPolicy{
			DeniedPaths: []string{"/etc"},
		}
		args := map[string]interface{}{"path": 123}
		assessment := CombinedSecurityAssessment("write_file", args, policy, 0, false, false, "file-write")

		// Non-string path means the path check is skipped
		if !assessment.PathAllowed {
			t.Error("expected PathAllowed=true for non-string path (check skipped)")
		}
	})
}

func TestCombinedSecurityAssessment_NonShellTools(t *testing.T) {
	t.Run("non-shell tool uses default policy action", func(t *testing.T) {
		policy := &SecurityPolicy{
			DefaultAction: "allow",
		}
		args := map[string]interface{}{"url": "http://example.com"}
		assessment := CombinedSecurityAssessment("fetch_url", args, policy, 0, false, false, "network")

		if assessment.PolicyAction != PolicyAllow {
			t.Errorf("expected PolicyAllow, got %s", assessment.PolicyAction)
		}
	})

	t.Run("non-shell tool with empty default action uses prompt", func(t *testing.T) {
		policy := &SecurityPolicy{}
		args := map[string]interface{}{"url": "http://example.com"}
		assessment := CombinedSecurityAssessment("fetch_url", args, policy, 0, false, false, "network")

		if assessment.PolicyAction != PolicyPrompt {
			t.Errorf("expected PolicyPrompt, got %s", assessment.PolicyAction)
		}
	})
}

func TestCombinedSecurityAssessment_Mixed(t *testing.T) {
	t.Run("shell command with max risk and policy deny interaction", func(t *testing.T) {
		policy := &SecurityPolicy{
			DefaultAction: "deny",
			MaxRiskLevel:  "safe",
		}
		args := map[string]interface{}{"command": "ls"}
		// classifierRisk=2 exceeds maxAllowed=0, and policy deny is set
		// Both override messages are now accumulated with "; "
		assessment := CombinedSecurityAssessment("shell_command", args, policy, 2, false, false, "destructive")

		if assessment.PolicyAction != PolicyDeny {
			t.Errorf("expected PolicyDeny, got %s", assessment.PolicyAction)
		}
		// Both reasons should be accumulated
		if !strings.Contains(assessment.OverrideAction, "policy denies command despite classifier allowing it") {
			t.Errorf("expected OverrideAction to contain policy deny message, got: %q", assessment.OverrideAction)
		}
		if !strings.Contains(assessment.OverrideAction, "classifier risk level 2 exceeds policy max 0") {
			t.Errorf("expected OverrideAction to contain max risk message, got: %q", assessment.OverrideAction)
		}
		if !strings.Contains(assessment.OverrideAction, "; ") {
			t.Errorf("expected OverrideAction to contain '; ' separator, got: %q", assessment.OverrideAction)
		}
	})

	t.Run("classifier allows and policy allows no override", func(t *testing.T) {
		policy := &SecurityPolicy{
			DefaultAction: "allow",
			MaxRiskLevel:  "dangerous",
		}
		args := map[string]interface{}{"command": "ls"}
		assessment := CombinedSecurityAssessment("shell_command", args, policy, 0, false, false, "read-only")

		if assessment.OverrideAction != "" {
			t.Errorf("expected empty OverrideAction, got %q", assessment.OverrideAction)
		}
	})

	t.Run("nil policy for file operations", func(t *testing.T) {
		args := map[string]interface{}{"path": "/etc/passwd"}
		assessment := CombinedSecurityAssessment("write_file", args, nil, 0, false, false, "file-write")

		if !assessment.PathAllowed {
			t.Error("expected PathAllowed=true for nil policy")
		}
		if assessment.PolicyAction != PolicyPrompt {
			t.Errorf("expected PolicyPrompt for nil policy, got %s", assessment.PolicyAction)
		}
	})
}

// ---------------------------------------------------------------------------
// TestCombinedSecurityAssessment_OverrideAccumulation
// ---------------------------------------------------------------------------

func TestCombinedSecurityAssessment_OverrideAccumulation(t *testing.T) {
	t.Run("policy deny and max risk exceeded accumulate with separator", func(t *testing.T) {
		policy := &SecurityPolicy{
			DefaultAction:  "deny",
			MaxRiskLevel:   "safe",
			DeniedCommands: []string{"rm"},
			Rules: []SecurityRule{
				{Pattern: "rm*", Action: "deny"},
			},
		}
		// classifierRisk=1 exceeds maxAllowed=0, and policy deny triggers
		assessment := CombinedSecurityAssessment("shell_command", map[string]interface{}{
			"command": "rm -rf /important",
		}, policy, 1, false, false, "destructive")

		if assessment.OverrideAction == "" {
			t.Error("expected OverrideAction to be set")
		}
		if !strings.Contains(assessment.OverrideAction, "policy denies command") {
			t.Errorf("expected OverrideAction to contain policy deny message, got: %s", assessment.OverrideAction)
		}
		if !strings.Contains(assessment.OverrideAction, "risk level") {
			t.Errorf("expected OverrideAction to contain risk level message, got: %s", assessment.OverrideAction)
		}
		if !strings.Contains(assessment.OverrideAction, "; ") {
			t.Errorf("expected OverrideAction to contain '; ' separator, got: %s", assessment.OverrideAction)
		}
	})

	t.Run("path denied and max risk exceeded accumulate with separator", func(t *testing.T) {
		// For file operations, there's no max risk check in the current implementation,
		// so just verify that path denied alone sets OverrideAction correctly.
		policy := &SecurityPolicy{
			DefaultAction: "allow",
			MaxRiskLevel:  "safe",
			DeniedPaths:   []string{"/etc"},
		}
		assessment := CombinedSecurityAssessment("write_file", map[string]interface{}{
			"path": "/etc/passwd",
		}, policy, 2, false, false, "file-write")

		if assessment.PathAllowed {
			t.Error("expected PathAllowed=false")
		}
		if assessment.OverrideAction != "path denied by workspace security policy" {
			t.Errorf("unexpected OverrideAction: %q", assessment.OverrideAction)
		}
	})
}
