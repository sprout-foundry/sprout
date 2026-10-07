// verification_stop_rule_test.go — unit tests for the repair loop's
// termination decision (verificationLoopShouldStop), the pure rule that
// ends the repair loop: either every failing check has used
// its per-check repair attempts, or the turn's total repair rounds have
// reached the cap — whichever fires first. The per-check counters alone
// cannot stop two failure patterns: checks alternating failures between
// rounds (each key's counter grows at half speed and never reaches N),
// and interaction checks whose item id — and therefore counter key — is
// new every round. These tests pin that both patterns terminate at the
// total cap, that the per-check rule still fires first for the
// single-check case, and the defensive edges.
//
// They are pure — no agent, no fixture, no shell — so they run in every
// build including js/wasm.

package agent

import (
	"fmt"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// ---------------------------------------------------------------------------
// verificationLoopShouldStop (the repair loop's termination decision)
// ---------------------------------------------------------------------------

// vhFailingCheck is a failing check fixture for the stop-decision tests.
func vhFailingCheck(kind plancontract.Kind, items ...string) verify.Check {
	return verify.Check{Kind: kind, Items: items, Reason: "expected outcome not observed"}
}

// TestVerificationLoopShouldStop_AlternatingFailuresNeedTotalCap pins the
// termination guarantee: when failures alternate between checks, no single
// per-check counter ever reaches the limit N, so the per-check rule alone
// would never fire — the total-rounds cap is what ends the loop.
func TestVerificationLoopShouldStop_AlternatingFailuresNeedTotalCap(t *testing.T) {
	const limit, totalCap = 3, 6

	// Round 4 of an alternating pattern: build has failed twice, test
	// once, neither has reached N=3, and 4 repair rounds have run.
	res := &verify.Result{Checks: []verify.Check{
		vhFailingCheck(plancontract.KindBuild),
		{Kind: plancontract.KindTest, Passed: true},
	}}
	attempts := map[string]int{"build": 2, "test": 1}

	if verificationStopRuleFired(res, attempts, limit) {
		t.Fatal("the per-check rule fired on an alternating pattern; the cap would never be reached by it alone")
	}
	if verificationLoopShouldStop(res, attempts, limit, 4, totalCap) {
		t.Error("stop fired below the total cap while failing checks still had attempts left")
	}
	if !verificationLoopShouldStop(res, attempts, limit, 6, totalCap) {
		t.Error("stop did not fire at the total cap; the loop would run past it")
	}
	// Whichever condition the caller thinks fired, the decision is the
	// same once rounds reach the cap — even when the counters just reset
	// shape (a third check joins the rotation).
	res.Checks = append(res.Checks, vhFailingCheck(plancontract.KindPage))
	if !verificationLoopShouldStop(res, attempts, limit, 6, totalCap) {
		t.Error("a newly failing check must not restart a turn whose total rounds are spent")
	}
}

// TestVerificationLoopShouldStop_FreshInteractionKeysBoundedByCap pins the
// new-counter-key case: interaction checks are keyed per item id, so a
// failure that produces a new item id every round starts a fresh counter
// at 0 each time. The simulation walks the loop exactly as the hook does
// and proves it still terminates — at the cap, with no key ever reaching
// the per-check limit.
func TestVerificationLoopShouldStop_FreshInteractionKeysBoundedByCap(t *testing.T) {
	const limit, totalCap = 3, 6

	attempts := make(map[string]int)
	rounds := 0
	for {
		// Every round fails on a brand-new interaction item id.
		res := &verify.Result{Checks: []verify.Check{
			vhFailingCheck(plancontract.KindInteraction, fmt.Sprintf("i%d", rounds)),
		}}
		if verificationLoopShouldStop(res, attempts, limit, rounds, totalCap) {
			break
		}
		attempts[checkAttemptKey(res.Checks[0])]++
		rounds++
	}
	if rounds != totalCap {
		t.Errorf("loop ended after %d rounds, want %d (the total cap must be what ends it)", rounds, totalCap)
	}
	for key, used := range attempts {
		if used >= limit {
			t.Errorf("counter %q reached %d/%d; the per-check rule cannot explain the stop (fresh keys never reach N)", key, used, limit)
		}
	}
}

// TestVerificationLoopShouldStop_AlternatingKeysBoundedByCap pins the
// two-check rotation through the same simulation: build and test take
// turns failing, so both counters grow at half speed. With N=4 and the
// cap at 6, each key holds 3 attempts when the cap fires — strictly below
// N — so the per-check rule cannot explain the stop; the cap is what ends
// the loop, exactly there and not one round later.
func TestVerificationLoopShouldStop_AlternatingKeysBoundedByCap(t *testing.T) {
	const limit, totalCap = 4, 6

	attempts := make(map[string]int)
	rounds := 0
	for {
		var res *verify.Result
		if rounds%2 == 0 {
			res = &verify.Result{Checks: []verify.Check{
				vhFailingCheck(plancontract.KindBuild),
				{Kind: plancontract.KindTest, Passed: true},
			}}
		} else {
			res = &verify.Result{Checks: []verify.Check{
				{Kind: plancontract.KindBuild, Passed: true},
				vhFailingCheck(plancontract.KindTest),
			}}
		}
		if verificationLoopShouldStop(res, attempts, limit, rounds, totalCap) {
			if rounds != totalCap {
				t.Fatalf("loop ended after %d rounds, want %d (alternating failures must stop at the cap)", rounds, totalCap)
			}
			break
		}
		if rounds > totalCap {
			t.Fatal("loop ran past the total cap; it does not terminate")
		}
		for _, c := range res.Checks {
			if c.Skipped || c.Passed {
				continue
			}
			attempts[checkAttemptKey(c)]++
		}
		rounds++
	}
	for key, used := range attempts {
		if used >= limit {
			t.Errorf("counter %q reached %d/%d; the per-check rule cannot be what fired (alternating keys stay below N at the cap)", key, used, limit)
		}
	}
}

// TestVerificationLoopShouldStop_PerCheckRuleFiresBeforeCap pins that the
// total cap is additive, not a replacement: one check failing on every
// round exhausts its counter after N rounds, and the per-check rule ends
// the loop then — strictly before the total cap.
func TestVerificationLoopShouldStop_PerCheckRuleFiresBeforeCap(t *testing.T) {
	const limit, totalCap = 3, 6

	res := &verify.Result{Checks: []verify.Check{vhFailingCheck(plancontract.KindBuild)}}
	attempts := map[string]int{"build": 3}

	if !verificationStopRuleFired(res, attempts, limit) {
		t.Fatal("the per-check rule must fire once the only failing check has used its N attempts")
	}
	if !verificationLoopShouldStop(res, attempts, limit, 3, totalCap) {
		t.Error("stop did not fire at rounds=3 < cap=6; the per-check rule must end the loop before the cap")
	}
	// One attempt short: the per-check rule keeps the loop alive (the cap
	// is what would eventually catch an alternating pattern, not this).
	attempts["build"] = 2
	if verificationLoopShouldStop(res, attempts, limit, 2, totalCap) {
		t.Error("stop fired while the only failing check still had an attempt left below the cap")
	}
}

// TestVerificationLoopShouldStop_ZeroCapAndRunLevelErrors pins the
// defensive edges: a zero cap allows no repair rounds at all (the first
// failing run ends the loop), and a run that failed without any failing
// check (run-level errors only) fires the per-check rule under the
// combined decision — there is nothing to repair per check.
func TestVerificationLoopShouldStop_ZeroCapAndRunLevelErrors(t *testing.T) {
	res := &verify.Result{Checks: []verify.Check{vhFailingCheck(plancontract.KindBuild)}}
	if !verificationLoopShouldStop(res, map[string]int{}, 3, 0, 0) {
		t.Error("a zero total cap must allow no repair rounds")
	}
	errs := &verify.Result{Errors: []string{"starter manifest: invalid JSON"}}
	if !verificationLoopShouldStop(errs, map[string]int{}, 3, 0, 6) {
		t.Error("a run-level-only failure must stop immediately (nothing to repair per check)")
	}
}
