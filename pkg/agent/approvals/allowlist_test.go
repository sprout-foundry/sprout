package approvals

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestIsShellCommandAllowlisted_LiteralAndPattern covers the two match
// paths + the non-matches on a bare *configuration.Config (no manager).
func TestIsShellCommandAllowlisted_LiteralAndPattern(t *testing.T) {
	cfg := &configuration.Config{
		ApprovedShellCommands:        []string{"npm test", "make build"},
		ApprovedShellCommandPatterns: []string{"git push origin/*"},
	}
	if !IsShellCommandAllowlisted(cfg, "npm test") {
		t.Error("literal command should match")
	}
	if !IsShellCommandAllowlisted(cfg, "git push origin/main") {
		t.Error("glob pattern should match")
	}
	if IsShellCommandAllowlisted(cfg, "git push fork/main") {
		t.Error("pattern should not match a different remote")
	}
	if IsShellCommandAllowlisted(cfg, "") {
		t.Error("empty command must not match")
	}
	if IsShellCommandAllowlisted(nil, "npm test") {
		t.Error("nil config must not allow anything")
	}
}

// TestPersistAllowlistRoundTrip pins the persist → lookup round trip and
// idempotency through a real (isolated) config manager.
func TestPersistAllowlistRoundTrip(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	cmd := "kubectl delete pod some-pod"
	if err := PersistShellCommandAllowlist(mgr, cmd); err != nil {
		t.Fatalf("PersistShellCommandAllowlist: %v", err)
	}
	if !IsShellCommandAllowlisted(mgr.GetConfig(), cmd) {
		t.Error("persisted command not reflected in lookup")
	}
	// Idempotency: no duplicate.
	if err := PersistShellCommandAllowlist(mgr, cmd); err != nil {
		t.Fatalf("re-persist: %v", err)
	}
	count := 0
	for _, c := range mgr.GetConfig().ApprovedShellCommands {
		if c == cmd {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 entry for %q, got %d", cmd, count)
	}
}

// TestPersistPatternRoundTrip covers the glob-pattern list + dedup.
func TestPersistPatternRoundTrip(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	pattern := "make *test*"
	if err := PersistShellCommandPattern(mgr, pattern); err != nil {
		t.Fatalf("PersistShellCommandPattern: %v", err)
	}
	if !IsShellCommandAllowlisted(mgr.GetConfig(), "make unit_test") {
		t.Error("persisted pattern should match a matching command")
	}
	if err := PersistShellCommandPattern(mgr, pattern); err != nil {
		t.Fatalf("re-persist: %v", err)
	}
	if got := len(mgr.GetConfig().ApprovedShellCommandPatterns); got != 1 {
		t.Errorf("expected 1 pattern, got %d", got)
	}
}

// TestPersistAskPolicyAppendsRule pins the ask-policy rule shape.
func TestPersistAskPolicyAppendsRule(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	cmd := "docker system prune"
	if err := PersistShellCommandAskPolicy(mgr, cmd); err != nil {
		t.Fatalf("PersistShellCommandAskPolicy: %v", err)
	}
	policies := mgr.GetConfig().CommandPolicies
	if policies == nil || len(policies.Rules) != 1 {
		t.Fatalf("expected exactly 1 ask-policy rule, got %+v", policies)
	}
	if policies.Rules[0].Pattern != cmd || policies.Rules[0].Action != configuration.CommandPolicyAsk {
		t.Errorf("rule mismatch: %+v", policies.Rules[0])
	}
}

// TestPersistRejectsEmptyAndNilManager pins the validation ordering.
func TestPersistRejectsEmptyAndNilManager(t *testing.T) {
	if err := PersistShellCommandAllowlist(nil, ""); err == nil {
		t.Error("empty command must be rejected (before the nil-manager check)")
	}
	if err := PersistShellCommandAllowlist(nil, "ls"); err == nil {
		t.Error("nil manager must be rejected")
	}
	if err := PersistShellCommandPattern(nil, ""); err == nil {
		t.Error("empty pattern must be rejected")
	}
	if err := PersistShellCommandAskPolicy(nil, "ls"); err == nil {
		t.Error("nil manager must be rejected")
	}
}
