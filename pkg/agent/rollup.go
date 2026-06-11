package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// SP-066 Phase 2: hierarchical rollup of TurnCheckpoints.
//
// As a conversation grows, the per-turn checkpoint list grows linearly. Even
// though each checkpoint is small, the list itself becomes unwieldy and the
// substitute-every-prompt-build pass starts producing a long, fragmented
// context. The rollup worker folds N items at each level into one coarser
// summary at level+1, keeping the list bounded regardless of conversation
// length.
//
// The substitution logic in seed treats a rolled-up checkpoint exactly like
// a per-turn one — it's just a TurnCheckpoint whose StartIndex/EndIndex span
// a wider historical range and whose Summary covers many turns. See SP-066
// for the architecture.

// Rollup tuning constants. Conservative defaults intended to keep the active
// checkpoint list bounded under ~40 entries for a 500-turn session; tunable
// from telemetry once we have real session data.
const (
	// recentTurnsToPreserve is the number of most-recent Level=0 checkpoints
	// kept at full fidelity. The rollup worker never folds entries in this
	// window even if the level-0 count exceeds the threshold.
	recentTurnsToPreserve = 10

	// rollupSourceCount is the number of source checkpoints folded into a
	// single rollup at any level. Same N at every level for simplicity.
	rollupSourceCount = 20

	// rollupTriggerCount is the per-level checkpoint count that triggers a
	// rollup. Anything ≥ this number at level L (excluding the recency
	// window at level 0) gets folded into a level-(L+1) entry.
	rollupTriggerCount = rollupSourceCount

	// rollupMaxLevel caps how deeply rollups stack. Beyond this depth we
	// stop folding to avoid runaway summary-of-summary degradation.
	rollupMaxLevel = 5

	// rollupTargetWords is the soft word budget passed to the LLM for the
	// rolled-up summary body. Should match the limit in the rollup prompt
	// template (prompts/rollup_prompt.md).
	rollupTargetWords = 400
)

// rollupWorker serializes background rollup execution per Agent. A single
// in-flight rollup at a time is plenty — the trigger fires once per recorded
// turn, so missed cycles get caught on the next turn.
type rollupWorker struct {
	mu      sync.Mutex
	running bool
}

// scheduleRollupIfNeeded fires a background rollup check after a new
// checkpoint is recorded. Safe to call from any goroutine; idempotent if a
// rollup is already running.
func (a *Agent) scheduleRollupIfNeeded() {
	if a == nil {
		return
	}
	w := a.rollupWorker()
	if w == nil {
		return
	}
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	go func() {
		defer func() {
			w.mu.Lock()
			w.running = false
			w.mu.Unlock()
		}()
		if err := a.runRollupPass(context.Background()); err != nil {
			a.Logger().Debug("[rollup] pass failed: %v", err)
		}
	}()
}

// runRollupPass examines the current TurnCheckpoints and, if any level
// exceeds rollupTriggerCount (with the recency window preserved at level 0),
// folds the oldest rollupSourceCount items at that level into one
// level-(L+1) checkpoint via the LLM.
//
// The pass folds at most one rollup per invocation. If multiple levels are
// over budget, subsequent turn-record callbacks will trigger follow-on
// passes until things settle — keeps each LLM call cheap and avoids holding
// the lock for long stretches.
func (a *Agent) runRollupPass(ctx context.Context) error {
	checkpoints := a.copyTurnCheckpoints()
	if len(checkpoints) == 0 {
		return nil
	}

	startIdx, endIdx, level, ok := pickRollupTarget(checkpoints)
	if !ok {
		return nil
	}

	// SP-066 Phase 3d: if embeddings are available, look for a topic-shift
	// boundary inside the candidate range and shrink the rollup to stop
	// at it. Falls back to the default range when embeddings aren't
	// available or no significant drop is detected. Best-effort: the
	// worker never blocks on this.
	endIdx = a.refineRollupEnd(ctx, checkpoints, startIdx, endIdx)

	sources := checkpoints[startIdx : endIdx+1]
	rollup, err := a.buildRollupCheckpoint(ctx, sources, level+1)
	if err != nil {
		return fmt.Errorf("build rollup: %w", err)
	}

	a.replaceWithRollup(startIdx, endIdx, rollup)

	// SP-066 Phase 3a: embed the rollup so semantic recall can surface it
	// after its source per-turn entries are absorbed (and beyond, after any
	// future /compact wipe). The conversation store is the permanent memory
	// layer; the checkpoint list is just the active substitution window.
	sessionID := ""
	if a.state != nil {
		sessionID = a.state.GetSessionID()
	}
	a.embedRollupCheckpoint(ctx, sessionID, rollup)
	return nil
}

// pickRollupTarget walks the checkpoint list and returns the index range
// + level of the oldest contiguous block of `rollupSourceCount` checkpoints
// at the same level that would benefit from being folded. Returns ok=false
// when no level is over its threshold.
//
// Level 0 (per-turn) has the recency window applied: the most-recent
// `recentTurnsToPreserve` per-turn checkpoints are never folded.
func pickRollupTarget(checkpoints []TurnCheckpoint) (start, end, level int, ok bool) {
	// Count per level, then pick the lowest level that's over budget. Lower
	// levels overflow first; folding them up reduces pressure on higher
	// levels naturally.
	const considerLevels = rollupMaxLevel
	counts := make(map[int]int, considerLevels)
	for _, cp := range checkpoints {
		if cp.Level >= rollupMaxLevel {
			continue
		}
		counts[cp.Level]++
	}

	for lvl := 0; lvl < rollupMaxLevel; lvl++ {
		eligible := counts[lvl]
		// Apply the recency window only at level 0 — higher-level rollups
		// don't have a "recent" concept since the corresponding messages
		// are already substituted.
		if lvl == 0 {
			eligible -= recentTurnsToPreserve
		}
		if eligible < rollupTriggerCount {
			continue
		}

		// Find the first `rollupSourceCount` contiguous level-lvl entries
		// starting from the oldest.
		count := 0
		first := -1
		for i, cp := range checkpoints {
			if cp.Level != lvl {
				if count > 0 {
					// Discontinuity — start over after this point.
					count = 0
					first = -1
				}
				continue
			}
			// Skip the level-0 recency window: the most-recent
			// `recentTurnsToPreserve` level-0 entries are off-limits.
			if lvl == 0 && countLevel0After(checkpoints, i) < recentTurnsToPreserve {
				break
			}
			if first < 0 {
				first = i
			}
			count++
			if count >= rollupSourceCount {
				return first, i, lvl, true
			}
		}
	}
	return 0, 0, 0, false
}

// countLevel0After returns the number of Level-0 entries strictly after
// index i in the slice. Used to gate the recency window.
func countLevel0After(checkpoints []TurnCheckpoint, i int) int {
	n := 0
	for j := i + 1; j < len(checkpoints); j++ {
		if checkpoints[j].Level == 0 {
			n++
		}
	}
	return n
}

// buildRollupCheckpoint builds a level-`newLevel` TurnCheckpoint by calling
// the LLM with the rollup prompt over the concatenated source summaries.
// File-change manifests are unioned so they propagate up the hierarchy
// rather than evaporating at each rollup level.
func (a *Agent) buildRollupCheckpoint(ctx context.Context, sources []TurnCheckpoint, newLevel int) (TurnCheckpoint, error) {
	if len(sources) == 0 {
		return TurnCheckpoint{}, fmt.Errorf("no source checkpoints")
	}

	body, err := a.rollupSummarizeViaLLM(ctx, sources)
	if err != nil {
		return TurnCheckpoint{}, err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return TurnCheckpoint{}, fmt.Errorf("empty rollup body")
	}

	coveredTurns := 0
	sourceIDs := make([]string, 0, len(sources))
	for _, cp := range sources {
		if cp.CoveredTurns > 0 {
			coveredTurns += cp.CoveredTurns
		} else {
			// Level-0 entries before SP-066 may have CoveredTurns=0.
			coveredTurns++
		}
		if cp.ID != "" {
			sourceIDs = append(sourceIDs, cp.ID)
		}
	}

	return TurnCheckpoint{
		ID:                  newCheckpointID(),
		StartIndex:          sources[0].StartIndex,
		EndIndex:            sources[len(sources)-1].EndIndex,
		Summary:             body,
		ActionableSummary:   body, // rollup body is already action-oriented per the prompt template
		FileChanges:         unionFileChanges(sources),
		RevisionID:          latestRevisionID(sources),
		Level:               newLevel,
		CoveredTurns:        coveredTurns,
		SourceCheckpointIDs: sourceIDs,
	}, nil
}

// rollupSummarizeViaLLM calls the bound LLM with the rollup prompt template
// and a numbered list of source-summary blocks. Returns the summary body
// (no header wrapping).
func (a *Agent) rollupSummarizeViaLLM(ctx context.Context, sources []TurnCheckpoint) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent unavailable")
	}
	if a.client == nil {
		return "", fmt.Errorf("no LLM client bound; cannot roll up")
	}

	systemPrompt := GetEmbeddedRollupPrompt()
	if strings.TrimSpace(systemPrompt) == "" {
		return "", fmt.Errorf("rollup prompt template is empty")
	}

	userContent := buildRollupInputBlocks(sources)
	if strings.TrimSpace(userContent) == "" {
		return "", fmt.Errorf("rollup input is empty")
	}

	// Bound the rollup LLM call so a stuck provider doesn't pin the worker
	// goroutine. 60s is generous; rollup is background work and not on the
	// user's critical path.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req := []api.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userContent},
	}
	resp, err := a.client.SendChatRequest(ctx, req, nil, "", false)
	if err != nil {
		return "", fmt.Errorf("rollup llm call failed: %w", err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		return "", fmt.Errorf("rollup llm returned no choices")
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

// buildRollupInputBlocks renders the source checkpoints as a numbered
// chronological list. Each block carries the per-turn or rollup Summary
// plus the ActionableSummary if it diverges, plus the Files manifest if
// present. Matches the "Input Format" section of the rollup prompt.
func buildRollupInputBlocks(sources []TurnCheckpoint) string {
	var b strings.Builder
	for i, cp := range sources {
		fmt.Fprintf(&b, "### Source %d", i+1)
		if cp.Level > 0 {
			fmt.Fprintf(&b, " (level %d rollup covering %d turns)", cp.Level, cp.CoveredTurns)
		}
		b.WriteString("\n\n")
		b.WriteString(strings.TrimSpace(cp.Summary))
		b.WriteString("\n")
		if as := strings.TrimSpace(cp.ActionableSummary); as != "" && as != strings.TrimSpace(cp.Summary) {
			b.WriteString("\nActionable: ")
			b.WriteString(as)
			b.WriteString("\n")
		}
		if len(cp.FileChanges) > 0 {
			b.WriteString("\nFiles: ")
			for j, fc := range cp.FileChanges {
				if j > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "%s %s", fc.Op, fc.Path)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	_ = rollupTargetWords // referenced in the prompt template; kept as a documented constant
	return b.String()
}

// unionFileChanges merges the file-change manifests from a set of
// checkpoints into a single deduplicated list. Within a rollup we want
// the most informative op per path: a "modified" + "deleted" sequence
// reduces to "deleted"; "added" + "modified" stays as "added" (the file
// was net-added during the span). The simple last-write-wins rule used
// here captures most of that — the latest source's op overrides earlier
// ones, which matches the chronological-newest-wins intuition.
func unionFileChanges(sources []TurnCheckpoint) []CheckpointFileChange {
	if len(sources) == 0 {
		return nil
	}
	latest := make(map[string]string, 8)
	order := make([]string, 0, 8)
	for _, cp := range sources {
		for _, fc := range cp.FileChanges {
			if _, exists := latest[fc.Path]; !exists {
				order = append(order, fc.Path)
			}
			latest[fc.Path] = fc.Op
		}
	}
	if len(order) == 0 {
		return nil
	}
	out := make([]CheckpointFileChange, len(order))
	for i, path := range order {
		out[i] = CheckpointFileChange{Path: path, Op: latest[path]}
	}
	return out
}

// latestRevisionID returns the most recent non-empty RevisionID from the
// source set. The latest revision is what the model needs to call
// view_history for the full diff covering the rolled-up span.
func latestRevisionID(sources []TurnCheckpoint) string {
	for i := len(sources) - 1; i >= 0; i-- {
		if id := strings.TrimSpace(sources[i].RevisionID); id != "" {
			return id
		}
	}
	return ""
}

// replaceWithRollup splices the rollup checkpoint in place of the source
// range [startIdx..endIdx] in the agent's TurnCheckpoints slice. Locks
// the checkpoint mutex for the duration of the swap.
func (a *Agent) replaceWithRollup(startIdx, endIdx int, rollup TurnCheckpoint) {
	if a == nil {
		return
	}
	mu := a.state.GetCheckpointMutex()
	mu.Lock()
	defer mu.Unlock()

	checkpoints := a.state.GetTurnCheckpoints()
	if startIdx < 0 || endIdx >= len(checkpoints) || startIdx > endIdx {
		return
	}

	out := make([]TurnCheckpoint, 0, len(checkpoints)-(endIdx-startIdx))
	out = append(out, checkpoints[:startIdx]...)
	out = append(out, rollup)
	out = append(out, checkpoints[endIdx+1:]...)
	a.state.SetTurnCheckpoints(out)
}

// rollupWorker returns the agent's single background-rollup serializer,
// lazily initialized so existing tests that construct bare *Agent values
// don't crash on first use.
func (a *Agent) rollupWorker() *rollupWorker {
	if a == nil {
		return nil
	}
	a.rollupOnce.Do(func() {
		a.rollupW = &rollupWorker{}
	})
	return a.rollupW
}

// SummarizerHint mirrors core.SummarizerHint so this file compiles
// independently. Imported via core for the wrapped /compact path; here we
// pass core.SummarizerHint{} for the per-rollup call since the rollup
// prompt template handles word budget itself.
var _ = core.SummarizerHint{}
