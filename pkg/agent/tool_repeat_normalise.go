// tool_repeat_normalise.go — near-duplicate and growth extensions to the
// per-turn repeat guard (tool_repeat_guard.go). Two loop shapes defeat the
// exact-key guard: a call that copies the previous one and appends more of the
// same clause (arguments grow, so no two exact keys ever match), and an agent
// that cycles through a few command shapes (most calls repeat an earlier call
// but rarely the previous one). Both reduce to one idea: compare arguments
// under a *near* normalisation that masks the jitter, and count that near key
// across the whole turn.
//
//   - near normalisation: collapse whitespace, then mask the specific
//     variations a repeating loop injects (different -N resamples, timeout
//     jitter, unique per-call echo markers);
//   - runaway refusal: a clause repeated more than 5 times inside a single
//     call's arguments is degenerated, not useful — the call's model-visible
//     result is replaced with a refusal naming the clause;
//   - growth: the same near-normalised call whose arguments grow on each
//     repeat (a streak of longer-args repeats) is a stall even when no exact
//     or near key has reached its own threshold;
//   - scratch-file check: a turn whose only changes are files under the temp
//     dir makes no progress, so the idle rule still applies (real file
//     changes, not scratch writes, are what exempt a turn from idling).

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// nearHeadTailNRe masks the -N count in head/tail resamples so a loop that
// re-runs `tail -n 5 x`, `tail -n 55 x`, `tail -n 555 x` collapses to one
// near key while the exact arguments keep growing (resampling jitter).
var nearHeadTailNRe = regexp.MustCompile(`(\b(head|tail)\s+-n?\s+)\d+`)

// nearTimeoutFlagRe masks the value of --timeout / --timeout=N so a loop that
// jitters the timeout does not defeat the near match (timeout jitter).
var nearTimeoutFlagRe = regexp.MustCompile(`(--timeout[ =])\d+`)

// nearTimeoutCmdRe masks the duration after a bare `timeout` or `sleep`
// command (the command itself, not a --timeout flag).
var nearTimeoutCmdRe = regexp.MustCompile(`\b(timeout|sleep)\s+\d+`)

// nearEchoMarkerRe masks the argument of an `echo` so a loop that prints a
// unique per-call marker (each call's output differs but the shape is the
// same) collapses to one near key (unique per-call markers).
var nearEchoMarkerRe = regexp.MustCompile(`(echo\s+)[^|;&>]+`)

// nearNormStep applies one near-normalisation transform (in the documented
// order) and returns the result. Each step is a named regexp above.
func nearNormStep(s string, re *regexp.Regexp, repl string) string {
	return re.ReplaceAllString(s, repl)
}

// normalizeToolArgsNear starts from the exact normalisation (collapse
// whitespace) and then applies, in order, the near transforms, each masking
// a specific loop shape:
//
//	(\b(head|tail)\s+-n?\s+)\d+ → "${1}N"    (resampling with a different -N)
//	(--timeout[ =])\d+          → "${1}N"    (timeout flag jitter)
//	\b(timeout|sleep)\s+\d+    → "${1} N"   (timeout/sleep duration)
//	(echo\s+)[^|;&>]+          → "${1}MARKER" (unique per-call echo markers)
//
// The result is used only as the *near* repeat-tracker key; the exact key
// still carries the unmasked normalised arguments.
func normalizeToolArgsNear(args string) string {
	s := normalizeToolArgs(args)
	s = nearNormStep(s, nearHeadTailNRe, "${1}N")
	s = nearNormStep(s, nearTimeoutFlagRe, "${1}N")
	s = nearNormStep(s, nearTimeoutCmdRe, "${1} N")
	s = nearNormStep(s, nearEchoMarkerRe, "${1}MARKER")
	return s
}

// nearToolArgKey is the near repeat-tracker key: the channel-stripped tool
// name plus the near-normalised arguments (in place of the exact-normalised
// ones used by toolArgKey).
func nearToolArgKey(toolName, args string) string {
	return stripToolChannel(toolName) + "\x00" + normalizeToolArgsNear(args)
}

// runawayClauseTokens is the number of consecutive tokens that form one
// candidate clause in findRunawayClause (a 3-token run).
const runawayClauseTokens = 3

// runawayClauseMinTimes is the minimum number of occurrences (in the
// 3-token window scan) of a candidate clause for findRunawayClause to
// report it — a clause repeated more than 5 times (the refusal wiring in
// seed_tool_repeat_guard.go checks times > 5 against this count).
const runawayClauseMinTimes = 6

// runawayMaxArgsBytes bounds the argument length findRunawayClause scans:
// arguments longer than this are skipped (a huge argument is not a runaway
// clause; the scan is a bounded 3-token-window count and is not worth
// running on very large input).
const runawayMaxArgsBytes = 4096

// findRunawayClause returns the most-frequent run of runawayClauseTokens
// consecutive whitespace-separated tokens in args and how many times it
// occurs, or ("", 0) when no run reaches runawayClauseMinTimes. The scan is
// bounded: each 3-token window tokens[i:i+3] is counted in a single pass (a
// map), O(n) in the number of windows. Arguments longer than
// runawayMaxArgsBytes are skipped (return "", 0). The winner is
// deterministic: the clause with the highest count, and on a tie the one
// whose first window occurs earliest in the argument string (map iteration
// order is random in Go, so the tie-break must come from the scan itself).
func findRunawayClause(args string) (string, int) {
	if len(args) > runawayMaxArgsBytes {
		return "", 0
	}
	tokens := strings.Fields(args)
	if len(tokens) < runawayClauseTokens {
		return "", 0
	}
	counts := make(map[string]int)
	firstSeen := make(map[string]int)
	for i := 0; i+runawayClauseTokens <= len(tokens); i++ {
		key := strings.Join(tokens[i:i+runawayClauseTokens], " ")
		if counts[key] == 0 {
			firstSeen[key] = i
		}
		counts[key]++
	}
	best, bestN, bestFirst := "", 0, -1
	for k, n := range counts {
		if n > bestN || (n == bestN && n > 0 && (bestFirst < 0 || firstSeen[k] < bestFirst)) {
			best, bestN, bestFirst = k, n, firstSeen[k]
		}
	}
	if bestN >= runawayClauseMinTimes {
		return best, bestN
	}
	return "", 0
}

// runawayRefusalNote builds the model-visible refusal message for a call whose
// arguments contain a runaway clause (a clause repeated more than 5 times).
// It names the clause and tells the model the command looks degenerated rather
// than useful, so it stops repeating and changes approach.
func runawayRefusalNote(clause string) string {
	return fmt.Sprintf(
		"[repeat guard] refusal: the clause %q is repeated in this call's arguments. "+
			"The command looks degenerated rather than useful — stop repeating and change approach.",
		clause)
}

// growthMinStreak is the number of consecutive times-args-got-longer observed
// on the same near key before a call is classified as a growth stall.
const growthMinStreak = 3

// growthMinCount is the minimum number of calls (on the near key) a growth
// stall requires alongside a long enough growth streak (the same near
// call's arguments keep growing, and it has been issued enough times).
const growthMinCount = 6

// observe records one tool call under BOTH its exact key and its near key and
// returns the exact key's 1-based count and whether its result is
// byte-identical to the previous identical call's result (false on the first
// call). The near key is counted (for near-duplicate and growth detection);
// the exact key drives the byte-identical flag and the per-key
// output/digest. The growth streak (args-got-longer) is tracked on the
// near-key state only, because the growth rule consults the near key (the
// near key is the one that stays stable while the exact arguments grow). It
// is the single bookkeeping site, so the counts, the identical flag, the
// growth streak, and the aggregate can never diverge.
func (t *toolRepeatTracker) observe(exactKey, nearKey, result string) (count int, identical bool) {
	if t == nil {
		return 1, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.keys == nil {
		t.keys = make(map[string]*toolRepeatState)
	}
	if t.nearKeys == nil {
		t.nearKeys = make(map[string]*toolRepeatState)
	}

	st, ok := t.keys[exactKey]
	if !ok {
		st = &toolRepeatState{}
		t.keys[exactKey] = st
		t.distinctCalls++
	}
	st.count++
	count = st.count
	t.totalCalls++
	identical = st.lastDigest != "" && digestOf(result) == st.lastDigest
	st.lastDigest = digestOf(result)
	st.lastOutput = stallSummaryTrunc(result, stallSummaryOutputBytes)

	nst, nok := t.nearKeys[nearKey]
	if !nok {
		nst = &toolRepeatState{}
		t.nearKeys[nearKey] = nst
	}
	nst.count++
	t.updateGrowthState(nst, exactKey)
	return count, identical
}

// updateGrowthState bumps the growth streak on a state entry when the
// current call's exact key (which embeds the exact-normalised arguments) is
// longer than the previous call's for that state, and breaks the streak (0)
// when the argument length did not grow. lastLen is proxied by len(exactKey)
// because the exact key embeds the exact-normalised arguments verbatim.
func (t *toolRepeatTracker) updateGrowthState(st *toolRepeatState, exactKey string) {
	l := len(exactKey)
	if l > st.lastLen {
		st.grewStreak++
	} else {
		st.grewStreak = 0
	}
	st.lastLen = l
}

// nearCount returns how many times a specific near key has run this turn (0
// when unseen).
func (t *toolRepeatTracker) nearCount(key string) int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.nearKeys == nil {
		return 0
	}
	if st, ok := t.nearKeys[key]; ok {
		return st.count
	}
	return 0
}

// growthStalled reports whether a call on nearKey (after observing the call,
// so its count is already tallied) is a growth stall: the same near
// normalised call whose arguments grew on each repeat for at least
// growthMinStreak consecutive repeats AND has been issued at least
// growthMinCount times.
func (t *toolRepeatTracker) growthStalled(nearKey string) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.nearKeys == nil {
		return false
	}
	st, ok := t.nearKeys[nearKey]
	if !ok {
		return false
	}
	return st.grewStreak >= growthMinStreak && st.count >= growthMinCount
}

// progressPaths returns the changed paths that count as real progress: the
// turn's changed paths with the scratch files (under the OS temp dir) filtered
// out. A turn whose only changes are temp files makes no progress, so it is
// still eligible for the idle rule.
func progressPaths(a *Agent) []string {
	if a == nil {
		return nil
	}
	paths := a.TurnChangedPaths()
	if len(paths) == 0 {
		return nil
	}
	prefix := strings.TrimSuffix(filepath.Clean(os.TempDir()), string(os.PathSeparator)) + string(os.PathSeparator)
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if strings.HasPrefix(filepath.Clean(p), prefix) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// stallDecision classifies a call against the stall rules, in order:
//
//   - the exact call has reached the identical-call threshold →
//     "identical_calls";
//   - else the near-normalised call has reached twice the identical-call
//     threshold → "near_duplicates";
//   - else the same near-normalised call's arguments have grown on each
//     repeat (a growth streak) → "growing_args";
//   - else the turn has accumulated a long stretch of redundant calls and
//     made no real progress (no changed path outside the temp dir) →
//     "idle_calls".
//
// It returns the stall reason and summary (nil when the call did not stall)
// so recordToolCall's switch can delegate to one place.
func stallDecision(
	tracker *toolRepeatTracker,
	a *Agent,
	tool, normArgs, exactKey, nearKey string,
	exactCount int,
) (reason string, summary *StallSummary) {
	thr := a.stallIdenticalThreshold()
	switch {
	case exactCount >= thr:
		// The exact (tool, args) call has hit its own threshold.
		reason, summary = "identical_calls", &StallSummary{
			Tool:    tool,
			Args:    normArgs,
			Output:  tracker.lastOutputFor(exactKey),
			Repeats: exactCount,
			Reason:  "identical_calls",
		}
	case tracker.nearCount(nearKey) >= thr*2:
		// The near-normalised call (masking -N / timeout / echo-marker
		// jitter) has accumulated enough repeats across the turn.
		reason, summary = "near_duplicates", &StallSummary{
			Tool:    tool,
			Args:    normArgs,
			Output:  tracker.lastOutputFor(exactKey),
			Repeats: tracker.nearCount(nearKey),
			Reason:  "near_duplicates",
		}
	case tracker.growthStalled(nearKey):
		// The same near-normalised call whose arguments keep growing on
		// each repeat is a stall even when no exact/near key hit its own
		// threshold (the growth shape).
		reason, summary = "growing_args", &StallSummary{
			Tool:    tool,
			Args:    normArgs,
			Output:  tracker.lastOutputFor(exactKey),
			Repeats: tracker.nearCount(nearKey),
			Reason:  "growing_args",
		}
	case tracker.idleCalls() >= a.stallIdleThreshold() && len(progressPaths(a)) == 0:
		// A long stretch of redundant calls with no new distinct command
		// AND no real file change this turn is a stall. A turn that edits
		// real files is making progress, so it never trips here; a
		// read-only turn whose calls keep introducing new distinct commands
		// never accumulates redundancy.
		reason, summary = "idle_calls", &StallSummary{
			Tool:    tool,
			Args:    normArgs,
			Output:  tracker.lastOutputFor(exactKey),
			Repeats: tracker.idleCalls(),
			Reason:  "idle_calls",
		}
	}
	return reason, summary
}
