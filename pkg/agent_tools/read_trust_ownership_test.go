package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/injection"
)

// stubOwnership fixes the ownership verdict for one test, so the trust
// decision can be exercised without a network or a real repository.
func stubOwnership(t *testing.T, verdict repoOwnership) {
	t.Helper()
	prev := trustOwnershipCheck
	trustOwnershipCheck = func(string) repoOwnership { return verdict }
	t.Cleanup(func() { trustOwnershipCheck = prev })
}

// nestedRepoFixture builds a nested clone inside a temp workspace and returns
// the env plus a file inside the clone.
func nestedRepoFixture(t *testing.T) (ToolEnv, string) {
	t.Helper()
	env, root := trustTestEnv(t)
	nested := filepath.Join(root, "clones", "acme")
	if err := os.MkdirAll(filepath.Join(nested, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(nested, "a.go")
	if err := os.WriteFile(target, []byte("package acme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return env, target
}

// TestResolveReadTrust_OwnedRepositoryIsTrustedWithoutAsking is the behaviour
// change: a clone the user owns is their own code, so no prompt is shown and
// the read is not framed.
func TestResolveReadTrust_OwnedRepositoryIsTrustedWithoutAsking(t *testing.T) {
	env, target := nestedRepoFixture(t)
	asker := &recordingAskUser{answer: trustThirdPartyOption}
	env.AskUser = asker
	stubOwnership(t, repoOwnership{trusted: true, reason: "owned by alantheprice"})

	if got := resolveReadTrust(env, target); got != injection.TrustFirstParty {
		t.Errorf("class = %v, want first-party for an owned repository", got)
	}
	if asker.calls() != 0 {
		t.Errorf("AskUser called %d times for an owned repository; want 0", asker.calls())
	}
}

// TestResolveReadTrust_OwnershipIsRemembered pins that the answered-by-ownership
// case is also persisted, so the lookup runs once per machine and repository
// rather than once per session.
func TestResolveReadTrust_OwnershipIsRemembered(t *testing.T) {
	env, target := nestedRepoFixture(t)
	env.AskUser = nil
	stubOwnership(t, repoOwnership{trusted: true, reason: "owned by alantheprice"})

	if got := resolveReadTrust(env, target); got != injection.TrustFirstParty {
		t.Fatalf("class = %v, want first-party", got)
	}
	decisions := env.ConfigManager.GetConfig().TrustedContentRoots[env.WorkspaceRoot]
	if len(decisions) != 1 {
		t.Fatalf("ownership decision not persisted: %v", decisions)
	}
	for path, thirdParty := range decisions {
		if thirdParty {
			t.Errorf("persisted %q as third-party; ownership should record first-party", path)
		}
	}
}

// TestResolveReadTrust_UnprovenOwnershipFallsThroughToTheQuestion is the
// safety half: when ownership cannot be established, the user is asked and the
// conservative answer applies.
func TestResolveReadTrust_UnprovenOwnershipFallsThroughToTheQuestion(t *testing.T) {
	env, target := nestedRepoFixture(t)
	asker := &recordingAskUser{answer: trustThirdPartyOption}
	env.AskUser = asker
	stubOwnership(t, repoOwnership{reason: "no push access"})

	if got := resolveReadTrust(env, target); got != injection.TrustThirdParty {
		t.Errorf("class = %v, want third-party", got)
	}
	if asker.calls() != 1 {
		t.Errorf("AskUser calls = %d, want 1", asker.calls())
	}
}

// TestResolveReadTrust_RecordedDecisionOutranksOwnership pins the ordering: a
// decision the user made explicitly is not overridden by the ownership check.
// Someone who marked a tree third-party meant it, even if they own the remote.
func TestResolveReadTrust_RecordedDecisionOutranksOwnership(t *testing.T) {
	env, target := nestedRepoFixture(t)
	env.AskUser = nil
	ownershipQueried := false
	prev := trustOwnershipCheck
	trustOwnershipCheck = func(string) repoOwnership {
		ownershipQueried = true
		return repoOwnership{trusted: true}
	}
	t.Cleanup(func() { trustOwnershipCheck = prev })

	if err := env.ConfigManager.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.TrustedContentRoots == nil {
			cfg.TrustedContentRoots = make(map[string]map[string]bool)
		}
		cfg.TrustedContentRoots[env.WorkspaceRoot] = map[string]bool{"clones/acme": true}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if got := resolveReadTrust(env, target); got != injection.TrustThirdParty {
		t.Errorf("class = %v, want third-party from the recorded decision", got)
	}
	if ownershipQueried {
		t.Error("ownership was checked despite a recorded decision")
	}
}

// TestTrustQuestionCarriesAShortTimeout pins the deadline the trust question
// asks for. The question's default is the conservative answer, so a long wait
// protects nothing and blocks an unattended session.
func TestTrustQuestionCarriesAShortTimeout(t *testing.T) {
	env, target := nestedRepoFixture(t)
	asker := &recordingAskUser{answer: trustThirdPartyOption}
	env.AskUser = asker
	stubOwnership(t, repoOwnership{reason: "no push access"})

	resolveReadTrust(env, target)

	asker.mu.Lock()
	defer asker.mu.Unlock()
	if len(asker.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(asker.requests))
	}
	req := asker.requests[0]
	if req.Timeout != trustQuestionTimeout {
		t.Errorf("Timeout = %v, want %v", req.Timeout, trustQuestionTimeout)
	}
	if req.Timeout > 5*time.Minute {
		t.Errorf("Timeout = %v; the trust question must not wait long", req.Timeout)
	}
	if req.Default != trustThirdPartyOption {
		t.Errorf("Default = %q, want the conservative %q", req.Default, trustThirdPartyOption)
	}
}

// TestAskUserManager_HonoursRequestTimeout pins that a per-request deadline
// overrides the manager's long default on the event-bus path.
func TestAskUserManager_HonoursRequestTimeout(t *testing.T) {
	mgr := NewAskUserManager()
	bus := events.NewEventBus()

	start := time.Now()
	_, err := mgr.RequestAskUser(context.Background(), bus, AskUserRequest{
		Question: "q",
		Timeout:  50 * time.Millisecond,
	}, "client", "", "")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed > 5*time.Second {
		t.Errorf("request blocked %v; the per-request timeout was ignored", elapsed)
	}
}
