// Package modelprobe runs a bounded capability probe against a model to check,
// cheaply and objectively, whether it's usable for agentic coding — and whether
// it's strong enough for complex work.
//
// It has two stages:
//
//   - Fast-fail gates (tier 1): a battery of single-turn checks. Each presents a
//     system prompt, a user prompt, and a tool set, then validates only the
//     model's FIRST response — did it call the right tool, with the right
//     argument, in the right order, including sprout's bespoke `repo_map` tool
//     that no model was trained on. These are quick and decisive: any miss is a
//     fast fail (the model isn't reliably usable on this platform), and we stop
//     without paying for the costlier complex stage.
//
//   - Complex task (tier 2): only runs if every gate passes. A multi-turn
//     discovery/analysis task over a small project seeded with distractor and
//     trap files. The model must read a couple of files, find the real root
//     cause, and emit a discrete, sensible TODO list (we stop once it does — we
//     don't have it implement). The todos are captured for evaluation; passing
//     is the signal for primary-grade complex work.
//
// Probing spends real tokens, so callers gate by estimated per-probe cost
// (WithinCostBudget) and a probe count cap.
package modelprobe

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// ProbeVersion identifies the probe scenario/scoring so results can be
// invalidated when the probe changes.
const ProbeVersion = "gates+todos+vision-v7"

// complexMaxTurns bounds the multi-turn complex stage. Set generously: some
// capable models explore one tool call per turn, so a tight cap fails them on
// budget (turn exhaustion) rather than capability. Give them room to finish
// exploring and submit their todos.
const complexMaxTurns = 20

// Estimated token usage for a full probe run (gates + complex), used to
// estimate per-probe cost for the budget gate. Deliberately generous, and sized
// for the higher turn/output bandwidth below (context re-sent each turn grows).
const (
	estInputTokens  = 28000
	estOutputTokens = 6000
)

// Result is the outcome of a probe run.
type Result struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`

	// Passed reports the fast-fail gates (tier 1, minimum bar). Complex reports
	// the discovery/analysis/todos stage (tier 2), the primary-grade signal.
	Passed  bool `json:"passed"`
	Complex bool `json:"complex,omitempty"`
	Skipped bool `json:"skipped,omitempty"`

	// Errored marks an inconclusive run: a transport/5xx/timeout error prevented
	// a full assessment, so this is NOT a capability verdict. Callers must not
	// persist or carry it forward — the model should be re-probed next run.
	Errored bool `json:"errored,omitempty"`

	// Score is the combined 0..1 score: gates contribute up to 0.5, the complex
	// stage the remaining 0.5.
	Score        float64 `json:"score"`
	GateScore    float64 `json:"gate_score"`
	ComplexScore float64 `json:"complex_score,omitempty"`

	// Todos is the model's submitted plan from the complex stage, captured
	// verbatim so a human can evaluate whether it actually makes sense.
	Todos string `json:"todos,omitempty"`

	Vision bool `json:"vision"`

	ToolCallOK       bool   `json:"tool_call_ok"`
	Turns            int    `json:"turns"`
	Reason           string `json:"reason"`
	LatencyMS        int64  `json:"latency_ms"`
	PromptTokens     int    `json:"prompt_tokens,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"`
	ProbedAt         string `json:"probed_at"`
	ProbeVersion     string `json:"probe_version"`
}

// EstimatedCostUSD estimates the dollar cost of one probe run for a model with
// the given per-million-token input/output prices.
func EstimatedCostUSD(inputPerMTok, outputPerMTok float64) float64 {
	return float64(estInputTokens)/1e6*inputPerMTok + float64(estOutputTokens)/1e6*outputPerMTok
}

// WithinCostBudget reports whether a model is cheap enough to probe given a
// per-probe dollar budget. maxPerProbe <= 0 disables the check. When set,
// models whose price is unknown OR whose estimated probe cost exceeds the
// budget are rejected — we only spend on models we can confirm are affordable.
func WithinCostBudget(inputPerMTok, outputPerMTok float64, costKnown bool, maxPerProbe float64) (ok bool, reason string) {
	if maxPerProbe <= 0 {
		return true, ""
	}
	if !costKnown {
		return false, fmt.Sprintf("skipped: price unknown; cannot confirm est. probe cost is within $%.4f", maxPerProbe)
	}
	if inputPerMTok < 0 || outputPerMTok < 0 {
		return false, fmt.Sprintf("skipped: price is negative (input $%.2f, output $%.2f/MTok) — likely a sentinel or non-purchasable meta-model", inputPerMTok, outputPerMTok)
	}
	if c := EstimatedCostUSD(inputPerMTok, outputPerMTok); c > maxPerProbe {
		return false, fmt.Sprintf("skipped: est. probe cost $%.4f exceeds $%.4f budget", c, maxPerProbe)
	}
	return true, ""
}

// MaxRequestOutputTokens caps per-request completion tokens during probing. Set
// with headroom so a model with a long todo list or some reasoning preamble
// isn't truncated mid-answer, while still (a) fitting comfortably inside the
// 64K-context floor and (b) keeping a single runaway response from blowing past
// the per-probe cost estimate the budget gate relies on.
const MaxRequestOutputTokens = 16384

// LimitRequestTokens caps the provider request completion-token budget for the
// current process via the env var the provider layer honors. Probe binaries
// call this once at startup.
func LimitRequestTokens() {
	_ = envutil.SetEnv("MAX_REQUEST_COMPLETION_TOKENS", strconv.Itoa(MaxRequestOutputTokens))
}

// SkippedResult builds a Result for a model that was not probed.
func SkippedResult(provider, model, reason string) Result {
	return Result{
		Provider: provider, Model: model, Passed: false, Skipped: true,
		Reason: reason, ProbedAt: time.Now().UTC().Format(time.RFC3339), ProbeVersion: ProbeVersion,
	}
}

// Run drives the probe against a client and returns a scored Result. The
// fast-fail gates always run; the complex stage runs only if every gate passes,
// so we never pay for the costly stage on a model that fails the minimum bar.
func Run(ctx context.Context, client api.ClientInterface, provider, model string) (Result, error) {
	start := time.Now()
	now := func() string { return time.Now().UTC().Format(time.RFC3339) }
	base := func() Result {
		return Result{Provider: provider, Model: model, ProbedAt: now(), ProbeVersion: ProbeVersion}
	}

	vision := runVision(ctx, client)
	if vision.stats.err != nil {
		r := base()
		r.Errored = true
		r.Reason = "vision probe request failed: " + vision.stats.err.Error()
		r.Turns = vision.stats.turns
		r.LatencyMS = time.Since(start).Milliseconds()
		return r, vision.stats.err
	}

	r := base()
	r.Vision = vision.passed
	r.Turns = vision.stats.turns
	r.PromptTokens = vision.stats.prompt
	r.CompletionTokens = vision.stats.compl

	gates := runFastGates(ctx, client)
	if gates.stats.err != nil {
		r.Errored = true
		r.Reason = "probe request failed: " + gates.stats.err.Error()
		r.Turns += gates.stats.turns
		r.LatencyMS = time.Since(start).Milliseconds()
		return r, gates.stats.err
	}

	r.GateScore = gates.score
	r.Passed = gates.passed
	r.ToolCallOK = gates.stats.anyTool
	r.Turns += gates.stats.turns
	r.PromptTokens += gates.stats.prompt
	r.CompletionTokens += gates.stats.compl

	if !gates.passed {
		r.Score = 0.5 * gates.score
		r.Reason = "fast-fail: " + gates.reason
		r.LatencyMS = time.Since(start).Milliseconds()
		return r, nil
	}

	cx := runComplex(ctx, client)
	r.ToolCallOK = r.ToolCallOK || cx.stats.anyTool
	r.Turns += cx.stats.turns
	r.PromptTokens += cx.stats.prompt
	r.CompletionTokens += cx.stats.compl
	r.Todos = cx.todos
	r.LatencyMS = time.Since(start).Milliseconds()

	if cx.stats.err != nil {
		// Gates passed, but the complex stage couldn't be assessed. Mark the
		// whole run inconclusive so callers re-probe rather than persisting a
		// Complex=false verdict that's really "unknown".
		r.Errored = true
		r.Score = 0.5
		r.Reason = "passed gates; complex stage errored (inconclusive): " + cx.stats.err.Error()
		return r, nil
	}

	r.ComplexScore = cx.score
	r.Complex = cx.passed
	r.Score = 0.5 + 0.5*cx.score
	if cx.passed {
		r.Reason = "passed gates + complex discovery/analysis"
	} else {
		r.Reason = "passed gates; complex stage incomplete: " + cx.reason
	}
	return r, nil
}

// tierOutcome is the scored result of a probe stage.
type tierOutcome struct {
	passed bool
	score  float64
	reason string
	todos  string // complex-stage submission, captured for review
	stats  driveStats
}

// scoreChecks returns the passed fraction and the sorted names of failed checks.
// Each stage decides its own pass criterion from the check map.
func scoreChecks(checks map[string]bool) (score float64, failed []string) {
	passed := 0
	for name, ok := range checks {
		if ok {
			passed++
		} else {
			failed = append(failed, name)
		}
	}
	sort.Strings(failed)
	return float64(passed) / float64(len(checks)), failed
}

// --- shared in-memory sandbox + multi-turn driver (complex stage) ---

// sandbox is the in-memory project the complex stage operates on. It records
// reads and listings so verification can score the model's process (did it
// actually explore) alongside its submitted todos.
type sandbox struct {
	files  map[string]string // path -> content (the project)
	reads  []string          // paths successfully read, in order
	listed []string          // dirs listed, in order
	todos  string            // submit_todos payload (summary + todos), if any
}

func newSandbox(files map[string]string) *sandbox {
	return &sandbox{files: files}
}

// driveStats is the telemetry from a stage (one or more requests).
type driveStats struct {
	turns   int
	prompt  int
	compl   int
	anyTool bool
	err     error
}

// traceEnabled turns on per-turn diagnostics (finish_reason, tool calls,
// content) to stderr, so we can tell a genuine instruction lapse (model
// answered in prose, finish=stop) from output truncation (finish=length).
func traceEnabled() bool {
	return envutil.GetEnvSimple("PROBE_TRACE") != "" || os.Getenv("SPROUT_PROBE_TRACE") != ""
}

func traceTurn(stage string, turn int, resp *api.ChatResponse, msg api.Message) {
	if !traceEnabled() {
		return
	}
	finish := ""
	if len(resp.Choices) > 0 {
		finish = resp.Choices[0].FinishReason
	}
	var names []string
	for _, tc := range msg.ToolCalls {
		names = append(names, tc.Function.Name)
	}
	snippet := strings.ReplaceAll(strings.TrimSpace(msg.Content), "\n", " ")
	if len(snippet) > 240 {
		snippet = snippet[:240] + "…"
	}
	fmt.Fprintf(os.Stderr, "[probe-trace] %-18s turn %d finish=%-10q tools=%v out_tok=%d content=%q\n",
		stage, turn, finish, names, resp.Usage.CompletionTokens, snippet)
}

// drive runs the multi-turn tool-use loop for the complex stage against the
// sandbox, stopping when stop(sandbox) is satisfied (todos submitted), the model
// answers in prose, or maxTurns is hit.
func drive(ctx context.Context, client api.ClientInterface, stage, system, task string, tools []api.Tool, sb *sandbox, maxTurns int, stop func(*sandbox) bool) driveStats {
	messages := []api.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: task},
	}
	var st driveStats
	for st.turns = 1; st.turns <= maxTurns; st.turns++ {
		resp, err := client.SendChatRequest(ctx, messages, tools, "", false)
		if err != nil {
			st.err = err
			return st
		}
		st.prompt += resp.Usage.PromptTokens
		st.compl += resp.Usage.CompletionTokens

		if len(resp.Choices) == 0 {
			break
		}
		msg := resp.Choices[0].Message
		if msg.Role == "" {
			msg.Role = "assistant"
		}
		traceTurn(stage, st.turns, resp, msg)
		messages = append(messages, msg)

		if len(msg.ToolCalls) == 0 {
			break // answered in prose / gave up
		}
		st.anyTool = true

		for _, tc := range msg.ToolCalls {
			content := sb.exec(tc)
			messages = append(messages, api.Message{Role: "tool", ToolCallID: tc.ID, Content: content})
		}
		if stop(sb) {
			break
		}
	}
	return st
}
