// evidence.go — the runner's run-evidence capture: for a run the policy
// selects, the run's working-copy diff (against the baseline commit), the
// agent's transcript and the verification outcome are written under
// <EvidenceDir>/runs/<task>-<model>-<n>/, so a failed run can be inspected
// after the fresh copy is gone.
//
// The capture runs inside runOnce, after the turn and before the copy is
// removed, so the diff reflects the turn's working-copy changes. It is
// best-effort: the policy decides which runs are kept, a missing evidence
// root means no evidence is kept at all, and any write failure is logged
// and swallowed — evidence is diagnostic material, never an input to the
// run's verdict.
package benchmark

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/git"
)

// KeepRuns is the run-evidence policy: which runs leave their working-copy
// diff, agent transcript and verification output under the runner's
// EvidenceDir. The zero value ("") means KeepRunsFailed — the default.
type KeepRuns string

const (
	// KeepRunsFailed keeps evidence for every run that is not Passed (the
	// default): a failed run is exactly when the diff, transcript and
	// verification output are needed.
	KeepRunsFailed KeepRuns = "failed"
	// KeepRunsAll keeps evidence for every run, passed or not.
	KeepRunsAll KeepRuns = "all"
	// KeepRunsNone never keeps evidence.
	KeepRunsNone KeepRuns = "none"
)

// ParseKeepRuns parses a --keep-runs value: "failed", "all" or "none"
// (case-insensitive, surrounding space trimmed). Any other value is an
// error naming the accepted ones, so a bad CLI value is a clear usage
// error rather than a silently ignored flag.
func ParseKeepRuns(value string) (KeepRuns, error) {
	switch KeepRuns(strings.ToLower(strings.TrimSpace(value))) {
	case KeepRunsFailed:
		return KeepRunsFailed, nil
	case KeepRunsAll:
		return KeepRunsAll, nil
	case KeepRunsNone:
		return KeepRunsNone, nil
	default:
		return "", fmt.Errorf("bad --keep-runs %q (want failed, all or none)", value)
	}
}

// resolves is the effective policy: the zero value ("") resolves to
// KeepRunsFailed, so a zero-value runner (tests, custom harnesses)
// behaves like the CLI default.
func (k KeepRuns) resolves() KeepRuns {
	if k == "" {
		return KeepRunsFailed
	}
	return k
}

// keepsRun reports whether the policy keeps evidence for a run with the
// given outcome. There is no evidence without an evidence root: a nil
// runner, or one with an empty EvidenceDir, keeps nothing regardless of
// the policy — there is nowhere to write.
func (r *Runner) keepsRun(passed bool) bool {
	if r == nil || strings.TrimSpace(r.EvidenceDir) == "" {
		return false
	}
	switch r.KeepRuns.resolves() {
	case KeepRunsNone:
		return false
	case KeepRunsAll:
		return true
	default:
		return !passed
	}
}

// captureRunEvidence writes a run's evidence under
// <EvidenceDir>/runs/<task>-<model>-<n>/ when the runner's policy keeps it.
// It is best-effort: a policy miss, a missing evidence root, or a write
// failure leaves the run untouched (the failure is logged, never returned).
// ag is the run's agent, nil for a run whose setup never built one — then
// only a note is written in place of the transcript.
//
// The run record is read (TaskID, Starter, Model, Provider, Passed, Result,
// NotVerifiedReason) but never mutated: evidence is diagnostic output, and
// a write failure must not flip a passing run to a failure.
func captureRunEvidence(r *Runner, task *Task, spec ModelSpec, runNumber int, runDir string, ag *agent.Agent, run *Run) {
	if r == nil || run == nil || task == nil {
		return
	}
	if !r.keepsRun(run.Passed) {
		return
	}
	var messages []api.Message
	if ag != nil {
		messages = ag.GetMessages()
	}
	root := filepath.Join(strings.TrimSpace(r.EvidenceDir), "runs")
	dir := reserveEvidenceDir(root, task.ID, spec, runNumber)
	if dir == "" {
		return
	}
	if err := writeRunDiff(runDir, dir); err != nil {
		logEvidenceFailure("write run diff into %s", dir, err)
	}
	if err := writeRunTranscript(dir, messages); err != nil {
		logEvidenceFailure("write run transcript into %s", dir, err)
	}
	if err := writeRunVerification(dir, run, messages); err != nil {
		logEvidenceFailure("write run verification into %s", dir, err)
	}
}

// reserveEvidenceDir creates the run's evidence directory under root,
// preferring the deterministic <task>-<model>-<n> name and falling back to
// a per-process unique name when that name already exists (a re-run into
// the same root must add evidence, never lose it). It returns "" when no
// directory could be created, after logging the failure.
func reserveEvidenceDir(root, taskID string, spec ModelSpec, runNumber int) string {
	dir := filepath.Join(root, runEvidenceDirName(taskID, spec, runNumber))
	if err := os.Mkdir(dir, 0o755); err == nil {
		return dir
	} else if !os.IsExist(err) {
		if mkErr := os.MkdirAll(root, 0o755); mkErr != nil {
			logEvidenceFailure("create evidence root %s", root, mkErr)
			return ""
		}
		if mkErr := os.Mkdir(dir, 0o755); mkErr != nil {
			logEvidenceFailure("create evidence dir %s", dir, mkErr)
			return ""
		}
		return dir
	}
	// The deterministic name is taken (a prior run into the same root):
	// fall back to a unique directory rather than overwriting the evidence
	// already there.
	for i := 1; i <= 1000; i++ {
		candidate := filepath.Join(root, fmt.Sprintf("%s-%d", runEvidenceDirName(taskID, spec, runNumber), i))
		if err := os.Mkdir(candidate, 0o755); err == nil {
			return candidate
		} else if !os.IsExist(err) {
			logEvidenceFailure("create evidence dir %s", candidate, err)
			return ""
		}
	}
	logEvidenceFailure("create evidence dir under %s", root, fmt.Errorf("no free directory name after 1000 attempts"))
	return ""
}

// logEvidenceFailure logs one best-effort evidence failure. It never
// returns an error: evidence capture must not change a run's verdict, so
// the failure is diagnostic only.
func logEvidenceFailure(format string, path string, err error) {
	log.Printf("benchmark: evidence %s: %v", fmt.Sprintf(format, path), err)
}

// writeRunDiff stages the copy's working tree and writes the staged diff
// against the baseline commit. `git add -A` first is deliberate: a plain
// `git diff` never shows untracked files, and a model's most common edit
// is a NEW file (the failing task created a new page file). Staging is
// safe here — the copy is a throwaway MkdirTemp directory with a
// throwaway baseline commit, never the user's repository.
func writeRunDiff(runDir, outDir string) error {
	if _, err := os.Stat(filepath.Join(runDir, ".git")); err != nil {
		return fmt.Errorf("no baseline repository in the run copy: %w", err)
	}
	if err := runGit(runDir, "add", "-A"); err != nil {
		return fmt.Errorf("stage the run copy: %w", err)
	}
	cmd := git.SafeGitCmd(runDir, "diff", "--cached", "--no-color")
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	diff, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git diff against the baseline: %w", err)
	}
	header := "# Benchmark run evidence: working-copy diff against the baseline\n" +
		"# (git add -A + git diff --cached — staged, so the diff includes new files)\n\n"
	return os.WriteFile(filepath.Join(outDir, "diff.patch"), append([]byte(header), diff...), 0o644)
}

// writeRunTranscript writes the run's agent transcript as JSON. A run whose
// setup never built an agent has no transcript; a small JSON note records
// that, so the evidence directory still explains itself.
func writeRunTranscript(outDir string, messages []api.Message) error {
	path := filepath.Join(outDir, "transcript.json")
	if messages == nil {
		return os.WriteFile(path, []byte("{\"messages\":[],\"note\":\"no agent transcript: the run did not reach the agent turn\"}\n"), 0o644)
	}
	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal the transcript: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// writeRunVerification writes the run's verification outcome: the
// verification result as JSON when the run produced one, otherwise a small
// JSON note carrying why the run was not verified. Alongside it, a
// human-readable summary is written so a reader does not have to parse the
// JSON to see the outcome.
func writeRunVerification(outDir string, run *Run, messages []api.Message) error {
	path := filepath.Join(outDir, "verification.json")
	var body any
	if run.Result != nil {
		body = run.Result
	} else {
		body = map[string]any{
			"verified": false,
			"reason":   run.NotVerifiedReason,
		}
	}
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal the verification output: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "summary.txt"), []byte(runEvidenceSummary(run, len(messages))), 0o644)
}

// runEvidenceSummary renders the human-readable outcome note that
// accompanies the JSON evidence: the run's identity, its verdict and the
// reason a failed run carries no verification result.
func runEvidenceSummary(run *Run, messageCount int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "task: %s\n", run.TaskID)
	fmt.Fprintf(&b, "starter: %s\n", run.Starter)
	fmt.Fprintf(&b, "model: %s\n", run.Model)
	fmt.Fprintf(&b, "provider: %s\n", run.Provider)
	fmt.Fprintf(&b, "run: %d\n", run.RunNumber)
	fmt.Fprintf(&b, "passed: %t\n", run.Passed)
	fmt.Fprintf(&b, "turns: %d\n", run.Turns)
	fmt.Fprintf(&b, "tokens: %d\n", run.Tokens)
	fmt.Fprintf(&b, "cost: %v\n", run.Cost)
	fmt.Fprintf(&b, "transcript messages: %d\n", messageCount)
	if run.Result == nil {
		fmt.Fprintf(&b, "verified: false\n")
		fmt.Fprintf(&b, "not verified reason: %s\n", run.NotVerifiedReason)
	} else {
		fmt.Fprintf(&b, "verified: true\n")
		fmt.Fprintf(&b, "verification passed: %t\n", run.Result.Passed())
		fmt.Fprintf(&b, "verification failed: %t\n", run.Result.Failed())
	}
	if run.Err != nil {
		fmt.Fprintf(&b, "run error: %s\n", run.Err.Error())
	}
	return b.String()
}

// runEvidenceDirName is the evidence directory name for one run:
// <task>-<model>-<n>, sanitized for the filesystem. The provider is
// included when set — <task>-<provider>-<model>-<n> — because the model id
// alone is not unique across providers (the same id under two providers
// would otherwise collide and silently overwrite one run's evidence). The
// task and provider name the disambiguating axes; the run number keeps
// repeated runs of one pair distinct.
func runEvidenceDirName(taskID string, spec ModelSpec, runNumber int) string {
	parts := dropEmpty([]string{
		sanitizeEvidenceSegment(taskID),
		sanitizeEvidenceSegment(spec.Provider),
		sanitizeEvidenceSegment(spec.Model),
	})
	name := strings.Join(parts, "-")
	if name == "" {
		name = "run"
	}
	return fmt.Sprintf("%s-%d", name, runNumber)
}

// dropEmpty removes empty segments, keeping the rest in order.
func dropEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// sanitizeEvidenceSegment replaces any character that could break a path
// (or make it ambiguous) with "-", so a model id like
// "meta-llama/Llama-3.1-8B" or a provider with a space produces a single
// safe path component. It never returns a path separator.
func sanitizeEvidenceSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		if r == '/' || r == '\\' || r == os.PathSeparator || r == 0 ||
			r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' ||
			unicode.IsSpace(r) || !utf8.ValidRune(r) {
			b.WriteByte('-')
			continue
		}
		b.WriteRune(r)
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "unnamed"
	}
	return out
}
