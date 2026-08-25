package configuration

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCommandPolicyActionConstants(t *testing.T) {
	tests := []struct {
		name   string
		action CommandPolicyAction
		want   string
	}{
		{"allow", CommandPolicyAllow, "allow"},
		{"ask", CommandPolicyAsk, "ask"},
		{"deny", CommandPolicyDeny, "deny"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.action) != tt.want {
				t.Errorf("CommandPolicyAction(%q) = %q, want %q", tt.name, tt.action, tt.want)
			}
		})
	}
}

func TestCommandPoliciesJSONRoundTrip(t *testing.T) {
	original := &CommandPolicies{
		Rules: []CommandRule{
			{Pattern: "git push*", Action: CommandPolicyAsk, Reason: "always confirm pushes"},
			{Pattern: "rm -rf /tmp/*", Action: CommandPolicyAllow},
			{Pattern: "kubectl delete*", Action: CommandPolicyDeny, Reason: "hard block"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded CommandPolicies
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !reflect.DeepEqual(*original, decoded) {
		t.Errorf("Round-trip mismatch:\noriginal: %+v\ndecoded:  %+v", *original, decoded)
	}
}

func TestCommandPoliciesJSONRoundTripEmpty(t *testing.T) {
	original := &CommandPolicies{Rules: []CommandRule{}}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal empty: %v", err)
	}

	var decoded CommandPolicies
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal empty: %v", err)
	}

	if len(decoded.Rules) != 0 {
		t.Errorf("Expected empty rules, got %d rules", len(decoded.Rules))
	}
}

func TestCommandRuleJSONWithReason(t *testing.T) {
	rule := CommandRule{
		Pattern: "npm run build",
		Action:  CommandPolicyAllow,
		Reason:  "CI pipeline command",
	}

	data, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded CommandRule
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Reason != rule.Reason {
		t.Errorf("Reason round-trip: got %q, want %q", decoded.Reason, rule.Reason)
	}
}

func TestCommandRuleJSONWithoutReason(t *testing.T) {
	rule := CommandRule{
		Pattern: "git push*",
		Action:  CommandPolicyAsk,
	}

	data, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Verify "reason" key is absent from JSON when empty
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to raw: %v", err)
	}

	if _, ok := raw["reason"]; ok {
		t.Error("Expected 'reason' key to be omitted when empty (omitempty)")
	}
}

func TestMigrateCommandPolicies_FromLiteralCommands(t *testing.T) {
	cfg := &Config{
		ApprovedShellCommands: []string{"npm test", "git status"},
	}

	MigrateCommandPolicies(cfg)

	if cfg.CommandPolicies == nil {
		t.Fatal("Expected CommandPolicies to be set after migration")
	}

	if len(cfg.CommandPolicies.Rules) != 2 {
		t.Fatalf("Expected 2 rules, got %d", len(cfg.CommandPolicies.Rules))
	}

	expected := []CommandRule{
		{Pattern: "npm test", Action: CommandPolicyAllow},
		{Pattern: "git status", Action: CommandPolicyAllow},
	}
	if !reflect.DeepEqual(cfg.CommandPolicies.Rules, expected) {
		t.Errorf("Rules mismatch:\ngot:  %+v\nwant: %+v", cfg.CommandPolicies.Rules, expected)
	}
}

func TestMigrateCommandPolicies_FromPatterns(t *testing.T) {
	cfg := &Config{
		ApprovedShellCommandPatterns: []string{"rm -rf /tmp/*", "echo *"},
	}

	MigrateCommandPolicies(cfg)

	if cfg.CommandPolicies == nil {
		t.Fatal("Expected CommandPolicies to be set after migration")
	}

	if len(cfg.CommandPolicies.Rules) != 2 {
		t.Fatalf("Expected 2 rules, got %d", len(cfg.CommandPolicies.Rules))
	}

	expected := []CommandRule{
		{Pattern: "rm -rf /tmp/*", Action: CommandPolicyAllow},
		{Pattern: "echo *", Action: CommandPolicyAllow},
	}
	if !reflect.DeepEqual(cfg.CommandPolicies.Rules, expected) {
		t.Errorf("Rules mismatch:\ngot:  %+v\nwant: %+v", cfg.CommandPolicies.Rules, expected)
	}
}

func TestMigrateCommandPolicies_FromBoth(t *testing.T) {
	cfg := &Config{
		ApprovedShellCommands:        []string{"npm test"},
		ApprovedShellCommandPatterns: []string{"rm -rf /tmp/*"},
	}

	MigrateCommandPolicies(cfg)

	if cfg.CommandPolicies == nil {
		t.Fatal("Expected CommandPolicies to be set after migration")
	}

	if len(cfg.CommandPolicies.Rules) != 2 {
		t.Fatalf("Expected 2 rules, got %d", len(cfg.CommandPolicies.Rules))
	}

	// Literal commands come first, then patterns
	if cfg.CommandPolicies.Rules[0].Pattern != "npm test" {
		t.Errorf("First rule pattern: got %q, want %q", cfg.CommandPolicies.Rules[0].Pattern, "npm test")
	}
	if cfg.CommandPolicies.Rules[1].Pattern != "rm -rf /tmp/*" {
		t.Errorf("Second rule pattern: got %q, want %q", cfg.CommandPolicies.Rules[1].Pattern, "rm -rf /tmp/*")
	}
}

func TestMigrateCommandPolicies_Idempotent(t *testing.T) {
	cfg := &Config{
		ApprovedShellCommands: []string{"npm test"},
		CommandPolicies: &CommandPolicies{
			Rules: []CommandRule{
				{Pattern: "git push*", Action: CommandPolicyAsk, Reason: "existing rule"},
			},
		},
	}

	MigrateCommandPolicies(cfg)

	// Should not have modified the existing CommandPolicies
	if len(cfg.CommandPolicies.Rules) != 1 {
		t.Fatalf("Expected 1 rule (unchanged), got %d", len(cfg.CommandPolicies.Rules))
	}
	if cfg.CommandPolicies.Rules[0].Pattern != "git push*" {
		t.Errorf("Rule was modified: got %q, want %q", cfg.CommandPolicies.Rules[0].Pattern, "git push*")
	}
	if cfg.CommandPolicies.Rules[0].Action != CommandPolicyAsk {
		t.Errorf("Rule action was modified: got %q, want %q", cfg.CommandPolicies.Rules[0].Action, CommandPolicyAsk)
	}
}

func TestMigrateCommandPolicies_EmptyFields(t *testing.T) {
	cfg := &Config{
		ApprovedShellCommands:        []string{},
		ApprovedShellCommandPatterns: []string{},
	}

	MigrateCommandPolicies(cfg)

	if cfg.CommandPolicies != nil {
		t.Errorf("Expected nil CommandPolicies for empty old fields, got %+v", cfg.CommandPolicies)
	}
}

func TestMigrateCommandPolicies_NilFields(t *testing.T) {
	cfg := &Config{}

	MigrateCommandPolicies(cfg)

	if cfg.CommandPolicies != nil {
		t.Errorf("Expected nil CommandPolicies for nil old fields, got %+v", cfg.CommandPolicies)
	}
}

func TestMigrateCommandPolicies_SkipsEmptyStrings(t *testing.T) {
	cfg := &Config{
		ApprovedShellCommands:        []string{"npm test", "", "git status"},
		ApprovedShellCommandPatterns: []string{"", "rm -rf /tmp/*"},
	}

	MigrateCommandPolicies(cfg)

	if cfg.CommandPolicies == nil {
		t.Fatal("Expected CommandPolicies to be set")
	}

	if len(cfg.CommandPolicies.Rules) != 3 {
		t.Fatalf("Expected 3 rules (empty strings skipped), got %d", len(cfg.CommandPolicies.Rules))
	}
}

func TestConfigWithCommandPoliciesJSONSerialization(t *testing.T) {
	cfg := &Config{
		Version: ConfigVersion,
		CommandPolicies: &CommandPolicies{
			Rules: []CommandRule{
				{Pattern: "git push*", Action: CommandPolicyAsk, Reason: "always ask"},
				{Pattern: "kubectl delete*", Action: CommandPolicyDeny},
			},
		},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal config: %v", err)
	}

	var decoded Config
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal config: %v", err)
	}

	if decoded.CommandPolicies == nil {
		t.Fatal("CommandPolicies was lost in round-trip")
	}
	if len(decoded.CommandPolicies.Rules) != 2 {
		t.Fatalf("Expected 2 rules, got %d", len(decoded.CommandPolicies.Rules))
	}
	if decoded.CommandPolicies.Rules[0].Action != CommandPolicyAsk {
		t.Errorf("First rule action: got %q, want %q", decoded.CommandPolicies.Rules[0].Action, CommandPolicyAsk)
	}
	if decoded.CommandPolicies.Rules[1].Action != CommandPolicyDeny {
		t.Errorf("Second rule action: got %q, want %q", decoded.CommandPolicies.Rules[1].Action, CommandPolicyDeny)
	}
}

func TestConfigWithNilCommandPoliciesOmitsField(t *testing.T) {
	cfg := &Config{
		Version: ConfigVersion,
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal config: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to raw: %v", err)
	}

	if _, ok := raw["command_policies"]; ok {
		t.Error("Expected 'command_policies' to be omitted when nil (omitempty)")
	}
}
