package agent

import (
	"errors"
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// fakeConnectionCheckClient wraps the in-memory TestClient (which satisfies
// the full api.ClientInterface) and scripts its CheckConnection: the first
// len(failures) calls fail with the given errors in order, every later call
// succeeds. It counts its CheckConnection calls so a test can prove how
// many probes ran and that the scripted client (not a recovered one) is
// what the startup kept.
type fakeConnectionCheckClient struct {
	*factory.TestClient
	failures []error
	checks   int
}

func (f *fakeConnectionCheckClient) CheckConnection() error {
	f.checks++
	if f.checks-1 < len(f.failures) {
		return f.failures[f.checks-1]
	}
	return nil
}

// newConnFake builds a fake that fails its first len(failures) checks.
func newConnFake(failures ...error) *fakeConnectionCheckClient {
	return &fakeConnectionCheckClient{TestClient: &factory.TestClient{}, failures: failures}
}

// connRetryable is a transient connection failure: isRetryableProviderError
// reports it (the "connection reset" fragment matches a provider-reported
// reset).
var connRetryable = errors.New("Post \"https://model.example/v1\": connection reset by peer")

// connPermanent is a permanent provider failure (a rejected key): not
// retryable, so it must reach recovery at once.
var connPermanent = errors.New("HTTP 401: invalid API key")

// runConnCheckStartup drives a full newAgentWithConfigManagerInner startup
// against an injected fake client and captures what happened:
//
//   - factoryCalls: the (provider|model) pairs the client factory was
//     asked for, so a test can see whether a provider switch (recovery)
//     happened;
//   - waits: the backoff waits the retry loop requested. The test injects
//     testConnectionCheckSleep (no real sleeping) and reads them here;
//   - the fake's check count is readable on the returned client.
//
// Setup mirrors the CLI-path tests: an isolated config manager (no real
// ~/.sprout), "lmstudio" as the last-used provider (its auth is "none", so
// no API key is involved), the real-provider path forced on (go test
// otherwise short-circuits to the mock client), SPROUT_MODEL cleared (so
// LastUsedProvider wins the model resolution), and a piped stdin so the run
// is non-interactive (recovery fails fast instead of prompting).
//
// The package-level seams (providerClientFactory, testConnectionCheckSleep)
// are restored after the call, so the test body observes them pre-restore.
func runConnCheckStartup(t *testing.T, fake *fakeConnectionCheckClient) (*Agent, error, []string, []time.Duration) {
	t.Helper()

	manager, mgrCleanup := configuration.NewTestManager(t)
	t.Cleanup(mgrCleanup)

	if err := manager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.LastUsedProvider = "lmstudio"
		if cfg.ProviderModels == nil {
			cfg.ProviderModels = make(map[string]string)
		}
		cfg.ProviderModels["lmstudio"] = "conn-check-model"
		return nil
	}); err != nil {
		t.Fatalf("configure lmstudio as last-used provider: %v", err)
	}

	t.Setenv("SPROUT_ALLOW_REAL_PROVIDER", "1")
	t.Setenv("SPROUT_MODEL", "")
	if _, restore := redirectStdinToPipe(t); restore != nil {
		t.Cleanup(restore)
	}

	var factoryCalls []string
	oldFactory := providerClientFactory
	providerClientFactory = func(ct api.ClientType, model string) (api.ClientInterface, error) {
		factoryCalls = append(factoryCalls, string(ct)+"|"+model)
		return fake, nil
	}
	t.Cleanup(func() { providerClientFactory = oldFactory })

	var waits []time.Duration
	oldSleep := testConnectionCheckSleep
	testConnectionCheckSleep = func(d time.Duration) { waits = append(waits, d) }
	t.Cleanup(func() { testConnectionCheckSleep = oldSleep })

	ag, err := newAgentWithConfigManagerInner(manager, t.TempDir(), "")
	return ag, err, factoryCalls, waits
}

// TestStartupConnectionCheck_RetryableFailureRecovers pins the regression
// for the 2026-11-15 fresh-VM incidents: a transient (retryable)
// connection-check failure must be retried instead of killing a
// non-interactive run. The fake fails twice with a retryable error, then
// succeeds: startup continues with that very client (no provider recovery),
// and the 2 s + 4 s backoff waits were injected, not slept.
func TestStartupConnectionCheck_RetryableFailureRecovers(t *testing.T) {
	fake := newConnFake(connRetryable, connRetryable)
	ag, err, factoryCalls, waits := runConnCheckStartup(t, fake)
	if err != nil {
		t.Fatalf("startup should have recovered from a retryable connection failure: %v", err)
	}
	defer ag.Shutdown()

	if got := ag.GetProviderType(); got != api.LMStudioClientType {
		t.Fatalf("GetProviderType() = %q, want %q", got, api.LMStudioClientType)
	}
	// No provider switch / recovery: the factory was asked for exactly the
	// configured provider, and the agent kept the injected client.
	if len(factoryCalls) != 1 || factoryCalls[0] != "lmstudio|conn-check-model" {
		t.Fatalf("factory calls = %v, want exactly one for the configured provider", factoryCalls)
	}
	if ag.client != api.ClientInterface(fake) {
		t.Fatalf("agent's client = %T, want the injected fake", ag.client)
	}

	// Two probes failed (the two scripted failures), the third succeeded.
	if fake.checks != 3 {
		t.Fatalf("CheckConnection calls = %d, want 3 (two failures, then success)", fake.checks)
	}
	// The waits between the two retries are the 2 s and 4 s schedule.
	if len(waits) != 2 || waits[0] != 2*time.Second || waits[1] != 4*time.Second {
		t.Fatalf("retry waits = %v, want [2s 4s]", waits)
	}
}

// TestStartupConnectionCheck_NonRetryableGoesToRecoveryAtOnce pins the
// other half: a permanent failure (a rejected key) is NOT retried — the
// fake's CheckConnection runs exactly once, no wait is requested, and
// startup falls through to recoverProviderStartup, which in
// non-interactive mode fails with "failed to initialize provider …
// Running in non-interactive mode".
func TestStartupConnectionCheck_NonRetryableGoesToRecoveryAtOnce(t *testing.T) {
	fake := newConnFake(connPermanent)
	_, err, factoryCalls, waits := runConnCheckStartup(t, fake)
	if err == nil {
		t.Fatal("expected startup to fail via provider recovery, got success")
	}
	if want := "failed to initialize provider LM Studio"; !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %q, want it to contain %q", err.Error(), want)
	}
	if len(factoryCalls) != 1 || factoryCalls[0] != "lmstudio|conn-check-model" {
		t.Fatalf("factory calls = %v, want exactly one for the configured provider", factoryCalls)
	}
	// One probe, no backoff: a permanent failure never retries.
	if fake.checks != 1 {
		t.Fatalf("CheckConnection calls = %d, want 1 (no retry of a permanent failure)", fake.checks)
	}
	if len(waits) != 0 {
		t.Fatalf("retry waits = %v, want none", waits)
	}
}
