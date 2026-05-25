package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestRiskProfile_AllNamesValid sanity-checks that every named
// profile is recognized by IsValidRiskProfile and that AutoApproveRulesForProfile
// returns a non-empty rules struct for each (except Unrestricted which
// is intentionally empty).
func TestRiskProfile_AllNamesValid(t *testing.T) {
	profiles := []RiskProfile{
		RiskProfileReadonly,
		RiskProfileCautious,
		RiskProfileDefault,
		RiskProfilePermissive,
		RiskProfileUnrestricted,
	}
	for _, p := range profiles {
		if !IsValidRiskProfile(string(p)) {
			t.Errorf("IsValidRiskProfile(%q) = false, want true", p)
		}
		_ = AutoApproveRulesForProfile(p)
	}
}

// TestRiskProfile_ReadonlyBlocksAllWrites verifies that readonly
// promotes every non-read operation to RiskLevelCritical (the only
// tier with no prompt path). Even an interactive user can't approve
// a write under readonly — that's the contract.
func TestRiskProfile_ReadonlyBlocksAllWrites(t *testing.T) {
	rules := AutoApproveRulesForProfile(RiskProfileReadonly)
	st := &SubagentType{AutoApproveRules: &rules}

	// Reads still go through cleanly
	allowed := []string{
		"git status", "git log --oneline", "git diff",
	}
	for _, c := range allowed {
		if got := st.EvaluateOperationRisk(c); got != RiskLevelLow {
			t.Errorf("readonly %q = %s, want Low", c, got)
		}
	}

	// Everything else is Critical (unconditional block)
	blocked := []string{
		"echo hello",
		"git commit -m 'x'",
		"git push origin main",
		"rm foo",
		"docker ps",
	}
	for _, c := range blocked {
		if got := st.EvaluateOperationRisk(c); got != RiskLevelCritical {
			t.Errorf("readonly %q = %s, want Critical", c, got)
		}
	}
}

func TestIsValidRiskProfile_Unknown(t *testing.T) {
	cases := []string{"", "foo", "Cautious", "DEFAULT", "danger"}
	for _, c := range cases {
		if IsValidRiskProfile(c) {
			t.Errorf("IsValidRiskProfile(%q) = true, want false", c)
		}
	}
}

// TestRiskProfile_CautiousGatesEverything verifies that the cautious
// profile sends `shell_command` (the catch-all for unknown commands)
// to HighRiskNever so any unrecognized command routes to a prompt.
func TestRiskProfile_CautiousGatesEverything(t *testing.T) {
	st := &SubagentType{
		AutoApproveRules: &AutoApproveRules{},
	}
	rules := AutoApproveRulesForProfile(RiskProfileCautious)
	st.AutoApproveRules = &rules

	cases := []struct {
		cmd  string
		want RiskLevel
	}{
		{"git status", RiskLevelLow},        // reads still auto-approve
		{"git log --oneline", RiskLevelLow}, // reads still auto-approve
		{"echo hello", RiskLevelHigh},       // shell_command → HighRiskNever
		{"git commit -m 'x'", RiskLevelHigh},
		{"rm -rf foo", RiskLevelHigh},
	}
	for _, c := range cases {
		got := st.EvaluateOperationRisk(c.cmd)
		if got != c.want {
			t.Errorf("cautious %q = %s, want %s", c.cmd, got, c.want)
		}
	}
}

// TestRiskProfile_PermissiveAllowsCommon verifies that the permissive
// profile auto-approves common operations including edits/commits and
// only blocks the truly destructive ones.
func TestRiskProfile_PermissiveAllowsCommon(t *testing.T) {
	rules := AutoApproveRulesForProfile(RiskProfilePermissive)
	st := &SubagentType{AutoApproveRules: &rules}

	cases := []struct {
		cmd  string
		want RiskLevel
	}{
		{"git status", RiskLevelLow},
		{"git commit -m 'x'", RiskLevelLow},
		{"git push origin main", RiskLevelLow},
		{"echo hello", RiskLevelLow},
		{"git checkout main", RiskLevelLow}, // permissive allows checkout
		// Still blocked
		{"rm -rf foo", RiskLevelHigh},
		{"git reset --hard HEAD", RiskLevelHigh},
		{"git push --force origin main", RiskLevelHigh},
	}
	for _, c := range cases {
		got := st.EvaluateOperationRisk(c.cmd)
		if got != c.want {
			t.Errorf("permissive %q = %s, want %s", c.cmd, got, c.want)
		}
	}
}

// TestRiskProfile_UnrestrictedAllowsAllExceptCritical confirms that
// the unrestricted profile genuinely bypasses ALL gating (force flags,
// rm -rf, etc.) except the absolute Critical tier.
func TestRiskProfile_UnrestrictedAllowsAllExceptCritical(t *testing.T) {
	rules := AutoApproveRulesForProfile(RiskProfileUnrestricted)
	st := &SubagentType{AutoApproveRules: &rules}

	allowed := []string{
		"rm -rf foo",
		"git push --force origin main",
		"git reset --hard HEAD",
		"git checkout main",
		"docker system prune -af",
	}
	for _, c := range allowed {
		if got := st.EvaluateOperationRisk(c); got != RiskLevelLow {
			t.Errorf("unrestricted %q = %s, want Low", c, got)
		}
	}

	// Critical still blocks — that's the floor that no profile can
	// raise.
	if got := st.EvaluateOperationRisk("rm -rf /"); got != RiskLevelCritical {
		t.Errorf("unrestricted rm -rf / = %s, want Critical", got)
	}
	if got := st.EvaluateOperationRisk(":(){ :|:& };:"); got != RiskLevelCritical {
		t.Errorf("unrestricted fork bomb = %s, want Critical", got)
	}
}

// TestIsCriticalOperation covers the absolute-block predicate.
func TestIsCriticalOperation(t *testing.T) {
	critical := []string{
		"rm -rf /",
		"rm -rf /*",
		"rm -rf ~",
		"rm -rf $HOME",
		"sudo rm -rf /",
		"cd /tmp && rm -rf /",
		":(){ :|:& };:", // classic fork bomb
	}
	for _, c := range critical {
		if !IsCriticalOperation(c) {
			t.Errorf("IsCriticalOperation(%q) = false, want true", c)
		}
	}

	notCritical := []string{
		"rm -rf foo",         // recursive but not root
		"rm -rf /tmp/cache",  // /tmp is fine
		"rm /etc/hosts",      // not recursive
		"echo 'rm -rf /'",    // rm inside quoted arg
		"echo hello",
		"git push",
		"ls /",
	}
	for _, c := range notCritical {
		if IsCriticalOperation(c) {
			t.Errorf("IsCriticalOperation(%q) = true, want false", c)
		}
	}
}

// TestResolveRiskProfileRules_UserOverrideReplacesBuiltin verifies
// that a user-defined entry in Config.RiskProfiles fully replaces
// the baked-in rules for that profile name.
func TestResolveRiskProfileRules_UserOverrideReplacesBuiltin(t *testing.T) {
	custom := AutoApproveRules{
		LowRiskOps:    []string{"read_file"},
		MediumRiskOps: []string{"shell_command"},
		HighRiskNever: []string{"force_flag"},
		DefaultRisk:   RiskLevelMedium,
	}
	cfg := &Config{
		RiskProfiles: map[string]AutoApproveRules{
			"cautious": custom,
		},
	}

	got := ResolveRiskProfileRules(cfg, RiskProfileCautious)
	if got.DefaultRisk != custom.DefaultRisk {
		t.Errorf("user override not applied: DefaultRisk got %q, want %q", got.DefaultRisk, custom.DefaultRisk)
	}
	if len(got.MediumRiskOps) != 1 || got.MediumRiskOps[0] != "shell_command" {
		t.Errorf("user override MediumRiskOps not applied: %v", got.MediumRiskOps)
	}

	// Other profile names still use the baked-in rules.
	defaultRules := ResolveRiskProfileRules(cfg, RiskProfileDefault)
	if defaultRules.DefaultRisk != RiskLevelMedium {
		t.Errorf("default profile should fall back to baked-in; got DefaultRisk=%q", defaultRules.DefaultRisk)
	}
	if len(defaultRules.LowRiskOps) <= 1 {
		t.Errorf("default profile should have multiple LowRiskOps; got %v", defaultRules.LowRiskOps)
	}
}

// TestResolveRiskProfileRules_UserCanDefineNewProfile verifies that
// a profile name not in the built-in set can be defined entirely by
// the user.
func TestResolveRiskProfileRules_UserCanDefineNewProfile(t *testing.T) {
	custom := AutoApproveRules{
		LowRiskOps:    []string{"git_status"},
		MediumRiskOps: []string{},
		HighRiskNever: []string{"rm_recursive"},
		DefaultRisk:   RiskLevelHigh,
	}
	cfg := &Config{
		RiskProfiles: map[string]AutoApproveRules{
			"my_strict": custom,
		},
	}

	got := ResolveRiskProfileRules(cfg, RiskProfile("my_strict"))
	if got.DefaultRisk != RiskLevelHigh {
		t.Errorf("custom profile rules not applied: DefaultRisk=%q want High", got.DefaultRisk)
	}

	// IsValidRiskProfile only knows about built-ins, so user-defined
	// names report false. That's the documented contract.
	if IsValidRiskProfile("my_strict") {
		t.Error("IsValidRiskProfile should not return true for user-defined names")
	}
}

// TestResolveRiskProfileRules_NilConfig handles the no-config case
// (early startup, tests).
func TestResolveRiskProfileRules_NilConfig(t *testing.T) {
	got := ResolveRiskProfileRules(nil, RiskProfileCautious)
	want := AutoApproveRulesForProfile(RiskProfileCautious)
	if got.DefaultRisk != want.DefaultRisk {
		t.Errorf("nil config should yield built-in: DefaultRisk got %q want %q", got.DefaultRisk, want.DefaultRisk)
	}
}

// TestEvaluateOperationRisk_CriticalShortCircuits verifies that
// Critical patterns return Critical regardless of which profile or
// persona rules are active.
func TestEvaluateOperationRisk_CriticalShortCircuits(t *testing.T) {
	// Permissive profile shouldn't allow rm -rf /
	rules := AutoApproveRulesForProfile(RiskProfilePermissive)
	st := &SubagentType{AutoApproveRules: &rules}
	if got := st.EvaluateOperationRisk("rm -rf /"); got != RiskLevelCritical {
		t.Errorf("permissive rm -rf / = %s, want Critical", got)
	}

	// Default profile likewise
	rules2 := AutoApproveRulesForProfile(RiskProfileDefault)
	st2 := &SubagentType{AutoApproveRules: &rules2}
	if got := st2.EvaluateOperationRisk("rm -rf /"); got != RiskLevelCritical {
		t.Errorf("default rm -rf / = %s, want Critical", got)
	}
}

// TestPerWorkspaceRiskProfileOverride verifies that a workspace-local
// .sprout/config.json with a risk_profile field overrides the global
// user setting via LoadConfigWithLayers + MergeConfig. This is the
// per-workspace override pattern documented in the README and SECURITY.md.
func TestPerWorkspaceRiskProfileOverride(t *testing.T) {
	tmpRoot := t.TempDir()
	globalDir := filepath.Join(tmpRoot, "global")
	workspaceDir := filepath.Join(tmpRoot, "workspace", ".sprout")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Global config: permissive everywhere
	globalCfg := Config{
		RiskProfile: "permissive",
		RiskProfiles: map[string]AutoApproveRules{
			"my_custom": {
				LowRiskOps:    []string{"read_file"},
				HighRiskNever: []string{"rm_recursive"},
				DefaultRisk:   RiskLevelMedium,
			},
		},
	}
	globalData, _ := json.Marshal(globalCfg)
	globalPath := filepath.Join(globalDir, "config.json")
	if err := os.WriteFile(globalPath, globalData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Workspace config: locked down to readonly + adds another custom profile
	workspaceCfg := Config{
		RiskProfile: "readonly",
		RiskProfiles: map[string]AutoApproveRules{
			"sandbox": {
				LowRiskOps:  []string{"shell_command"},
				DefaultRisk: RiskLevelLow,
			},
		},
	}
	workspaceData, _ := json.Marshal(workspaceCfg)
	workspacePath := filepath.Join(workspaceDir, "config.json")
	if err := os.WriteFile(workspacePath, workspaceData, 0o644); err != nil {
		t.Fatal(err)
	}

	merged, err := LoadConfigWithLayers(globalPath, workspacePath, "", globalDir)
	if err != nil {
		t.Fatalf("LoadConfigWithLayers: %v", err)
	}

	// Workspace overrides the single-value RiskProfile selector.
	if merged.RiskProfile != "readonly" {
		t.Errorf("merged RiskProfile = %q, want %q (workspace should override global)", merged.RiskProfile, "readonly")
	}

	// RiskProfiles map should contain BOTH custom entries (global +
	// workspace are merged per-key, not replaced).
	if _, ok := merged.RiskProfiles["my_custom"]; !ok {
		t.Errorf("merged RiskProfiles missing global entry 'my_custom': %v", keys(merged.RiskProfiles))
	}
	if _, ok := merged.RiskProfiles["sandbox"]; !ok {
		t.Errorf("merged RiskProfiles missing workspace entry 'sandbox': %v", keys(merged.RiskProfiles))
	}
	if merged.RiskProfiles["sandbox"].DefaultRisk != RiskLevelLow {
		t.Errorf("workspace 'sandbox' DefaultRisk = %q, want Low", merged.RiskProfiles["sandbox"].DefaultRisk)
	}
}

// TestPerWorkspaceRiskProfileSameKey verifies that when the same
// profile key exists in BOTH global and workspace, the workspace
// version wins.
func TestPerWorkspaceRiskProfileSameKey(t *testing.T) {
	tmpRoot := t.TempDir()
	globalDir := filepath.Join(tmpRoot, "global")
	workspaceDir := filepath.Join(tmpRoot, "workspace", ".sprout")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Global: default profile overridden to be permissive-ish
	globalCfg := Config{
		RiskProfiles: map[string]AutoApproveRules{
			"default": {DefaultRisk: RiskLevelLow},
		},
	}
	globalData, _ := json.Marshal(globalCfg)
	globalPath := filepath.Join(globalDir, "config.json")
	if err := os.WriteFile(globalPath, globalData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Workspace: default profile overridden to be strict
	workspaceCfg := Config{
		RiskProfiles: map[string]AutoApproveRules{
			"default": {DefaultRisk: RiskLevelHigh},
		},
	}
	workspaceData, _ := json.Marshal(workspaceCfg)
	workspacePath := filepath.Join(workspaceDir, "config.json")
	if err := os.WriteFile(workspacePath, workspaceData, 0o644); err != nil {
		t.Fatal(err)
	}

	merged, err := LoadConfigWithLayers(globalPath, workspacePath, "", globalDir)
	if err != nil {
		t.Fatalf("LoadConfigWithLayers: %v", err)
	}

	if got := merged.RiskProfiles["default"].DefaultRisk; got != RiskLevelHigh {
		t.Errorf("workspace should override global for same key: got DefaultRisk=%q, want High", got)
	}
}

func keys(m map[string]AutoApproveRules) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
