// agent_creation_connection_retry.go — the startup connection check's retry
// policy. A provider that is configured can still fail its single startup
// probe on a transient blip (a deploy, a cold gateway); that blip used to
// kill a non-interactive run with "configure a provider", so a retryable
// failure gets up to 4 more attempts over 30 s before recoverProviderStartup
// is consulted. A permanent failure (unknown model, rejected key) fails at
// once, exactly as before.

package agent

import (
	"errors"
	"os"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// providerClientFactory creates the startup client. It defaults to
// factory.CreateProviderClient; a test overrides it (via the package
// variable) to hand a scripted fake client to the provider loop without
// touching the global provider factory.
var providerClientFactory = factory.CreateProviderClient

// connectionCheckRetries caps the retries after the initial check failure:
// up to 4 more attempts, for 5 probes in total.
const connectionCheckRetries = 4

// connectionCheckRetryDelays is the backoff between the retry attempts:
// 2, 4, 8, 16 s — 30 s in all.
var connectionCheckRetryDelays = []time.Duration{
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
}

// testConnectionCheckSleep overrides the waiter so a unit test can drive the
// retry loop without sleeping (it records the requested waits instead).
// Never set in production.
var testConnectionCheckSleep func(time.Duration)

// connectionCheckSleep waits between retry attempts.
func connectionCheckSleep(d time.Duration) {
	if testConnectionCheckSleep != nil {
		testConnectionCheckSleep(d)
		return
	}
	time.Sleep(d)
}

// checkConnectionWithRetry runs the startup connection check, retrying a
// retryable failure (isRetryableProviderError) up to connectionCheckRetries
// times with the connectionCheckRetryDelays backoff. Each retry is logged
// on stderr as a warning. A non-retryable failure, or the last retry's
// failure, is returned so the caller can fall through to
// recoverProviderStartup.
func checkConnectionWithRetry(client api.ClientInterface) error {
	for attempt := 0; attempt < connectionCheckRetries+1; attempt++ {
		err := client.CheckConnection()
		if err == nil {
			return nil
		}
		if attempt >= len(connectionCheckRetryDelays) || !isRetryableProviderError(err) {
			return err
		}
		delay := connectionCheckRetryDelays[attempt]
		console.GlyphWarning.Fprintf(os.Stderr,
			"connection check failed (%v); retrying in %ds", err, int(delay.Seconds()))
		connectionCheckSleep(delay)
	}
	// Unreachable: the loop returns on the last attempt. Kept so the
	// function stays total if the schedule ever shrinks below the cap.
	return errors.New("connection check retries exhausted")
}
