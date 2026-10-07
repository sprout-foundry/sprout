// verification_state.go — the accessor for the turn-end hook's stored
// per-turn verification state for
// consumers outside pkg/agent: the benchmark per-task metrics
// and the verification event. The hook stores a fresh
// snapshot on every verification run (pass, fail, stop-rule);
// prepareQueryRun resets the state at each turn start, so a previous
// turn's result never attaches to a later turn's reply.

package agent

import (
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// TurnVerification is one turn's end-verification state
// for consumers outside pkg/agent (the benchmark
// metrics, and the verification event later): the run's
// result, the per-check repair attempts consumed against the
// stopping rule, the configured limit, and how many repair rounds
// the hook ran.
type TurnVerification struct {
	// Result is the turn's last verification run (the hook stores a
	// state on every run, pass, fail, or stop-rule; a non-nil
	// LastTurnVerification always carries a non-nil Result).
	Result *verify.Result
	// Attempts is the per-check repair attempts the hook consumed
	// against the stopping rule before its last verification run,
	// keyed like the hook's per-check counters (checkAttemptKey). It
	// is a copy: mutating it does not affect the agent's stored
	// state. Empty (nil) when the last run saw no repair rounds yet
	// (a passing run, or the first run of a failing sequence).
	Attempts map[string]int
	// Limit is the stopping-rule limit that was in effect for the
	// turn (stored on every run, pass or fail).
	Limit int
	// Rounds is how many repair rounds the hook ran within the turn
	// (0 for a turn that verified once and passed; a turn where the
	// hook never ran reports LastTurnVerification == nil, not
	// Rounds == 0).
	Rounds int
}

// LastTurnVerification returns the turn's stored end-verification state
// (stored by the turn-end hook on every
// verification run), or nil when the hook never ran for the turn
// (verification disabled, the turn changed no code, a subagent turn,
// or a runner setup error) — including on a nil receiver. Attempts is
// a copy: mutating it never affects the agent's stored state.
func (a *Agent) LastTurnVerification() *TurnVerification {
	if a == nil {
		return nil
	}
	stored := a.lastTurnVerificationSnapshot()
	if stored.result == nil {
		return nil
	}
	return &TurnVerification{
		Result:   stored.result,
		Attempts: stored.attempts,
		Limit:    stored.limit,
		Rounds:   stored.rounds,
	}
}
