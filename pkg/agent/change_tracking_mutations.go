// Mutation recording and bulk rollup for ChangeTracker shell-mutation tracking.
//
// Takes the diff from before/after snapshots and records TrackedFileChange
// entries, collapsing high-churn directories into bulk rollups when
// appropriate.
package agent

import (
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// shellBulkThreshold is the per-command mutation count that flips the
// recorder into rollup mode. Below this, every change records
// individually so small refactors and one-off edits keep fine-grained
// entries. Above it, the recorder assumes the command was a build /
// install / clone / unzip and collapses each top-level workspace
// directory's worth of churn into a single "bulk" row.
//
// SP-061-1 v2: the trigger is now per-COMMAND volume only. The
// previous design also required a per-directory floor as the primary
// gate, which missed real-world bulk operations whose output fanned
// out across many small directories:
//
//   - `git clone repo .` → ~5k files spread across hundreds of dirs,
//     none individually heavy → no rollup, the changes panel drowned
//   - `unzip flat-archive.zip` → similar fanout
//   - `pip install --target env/` → output buried in
//     `env/lib/python3.x/site-packages/...` with few files per leaf
//
// Static skip dirs (`node_modules`, `.venv`, `vendor`, …) still get
// pruned by the walker before they ever reach this code path, so the
// common composer / pip-into-.venv / npm cases are already silent.
// This threshold is a long-stop for everything else.
var shellBulkThreshold = 200

// shellBulkBucketMin is a SECONDARY filter applied only within bulk
// mode: buckets with fewer than this many items emit per-file even
// though the overall command was bulk. Without this, a single source
// edit made adjacent to a build command (`make && touch src/lib.go`)
// would render as a useless "src/ — 1 file" rollup row and lose the
// per-file view/revert affordances. The number is intentionally low —
// the primary gate is shellBulkThreshold; this is just protection
// for the handful of meaningful edits that ride along with a heavy
// command. It is NOT the v1 per-dir gate and does not need to be
// high enough to discriminate between fan-out shapes.
var shellBulkBucketMin = 10

// pendingShellMutation is the intermediate shape used by
// RecordShellMutations to defer "emit per-file vs roll up by directory"
// until we've counted everything. Mirrors the appendShellMutation
// argument list verbatim — only the destination differs.
type pendingShellMutation struct {
	path   string
	before *shellSnapshotEntry
	after  *shellSnapshotEntry
	op     string // "create" | "edit" | "delete"
}

// RecordShellMutations diffs a pair of snapshots (taken before/after a
// shell_command invocation) and appends TrackedFileChange entries for
// every file that materially changed. The before-snapshot supplies the
// OriginalCode field so a user can recover untracked-by-git files the
// agent accidentally deleted or mangled.
//
// Dedup: if the path was already recorded this turn via TrackFileWrite
// / TrackFileEdit (the direct tool hooks), the existing entry is kept
// and we don't double-record from the shell diff. The direct entry is
// richer (original_code captured at the source) so it wins.
//
// SP-061-1: when a single shell command churns more than
// shellBulkThreshold paths AND some top-level workspace directory
// owns at least shellBulkPerDirMin of them, that directory is rolled
// up into a single "bulk" entry and added to autoSkipDirs so future
// walks skip it entirely. The rollup carries BulkCount so the UI can
// render "dist/ — 1,247 files (build output)" instead of stacking
// thousands of individual rows.
func (ct *ChangeTracker) RecordShellMutations(before, after map[string]*shellSnapshotEntry, toolCall string) {
	if ct == nil || !ct.IsEnabled() {
		return
	}
	if before == nil {
		before = map[string]*shellSnapshotEntry{}
	}
	if after == nil {
		after = map[string]*shellSnapshotEntry{}
	}

	// Build a set of paths already recorded so the shell pass doesn't
	// double-count direct write/edit hooks fired during the same shell
	// command (rare in practice — shell_command rarely re-invokes
	// agent tools — but cheap to defend against).
	//
	// C3: snapshot ct.changes under ct.mu. RecordShellMutations runs on
	// the tool-execution goroutine while the turn-checkpoint goroutine
	// concurrently reads ct.changes via CollectFileChangesForCheckpoint;
	// without the lock this is a data race on the slice header.
	already := func() map[string]bool {
		ct.mu.Lock()
		defer ct.mu.Unlock()
		m := make(map[string]bool, len(ct.changes))
		for _, ch := range ct.changes {
			m[ch.FilePath] = true
		}
		return m
	}()

	// Phase 1: collect candidate mutations into a deferred slice so we
	// can decide between per-file emission and bulk rollup AFTER we
	// know the total count.
	pending := make([]pendingShellMutation, 0, len(before)+len(after))

	// Deletions: present in `before`, absent in `after`.
	for path, beforeEntry := range before {
		if _, stillThere := after[path]; stillThere {
			continue
		}
		if already[path] {
			continue
		}
		pending = append(pending, pendingShellMutation{path: path, before: beforeEntry, op: "delete"})
		already[path] = true
	}

	// Creations and modifications: present in `after`.
	for path, afterEntry := range after {
		if already[path] {
			continue
		}
		beforeEntry := before[path]
		if beforeEntry == nil {
			pending = append(pending, pendingShellMutation{path: path, after: afterEntry, op: "create"})
			already[path] = true
			continue
		}
		if shellContentsEqual(beforeEntry, afterEntry) {
			continue
		}
		pending = append(pending, pendingShellMutation{path: path, before: beforeEntry, after: afterEntry, op: "edit"})
		already[path] = true
	}

	// Phase 2: emit. Most shell commands fall below the threshold and
	// take the straight per-file path; only build-style commands hit
	// the rollup branch.
	if len(pending) < shellBulkThreshold {
		for _, p := range pending {
			ct.appendShellMutation(p.path, p.before, p.after, p.op, toolCall)
		}
		return
	}
	ct.emitWithBulkRollup(pending, toolCall)
}

// emitWithBulkRollup is the rollup path taken when a single shell
// command exceeded shellBulkThreshold. Strategy:
//
//  1. Bucket the pending mutations by their TOP-LEVEL workspace
//     directory. Root-level files (no enclosing dir) form a special
//     bucket of their own.
//  2. Root-level files always emit per-file — there is no useful
//     directory label for them ("workspace root" reads as everything)
//     and they're overwhelmingly config / lockfile / README edits
//     that the user wants to see.
//  3. Each non-root bucket collapses into ONE rollup row labeled by
//     the deepest workspace-relative path ALL items in the bucket
//     share. So `repo/foo.js + repo/bar/baz.js` → `repo/`; if every
//     item sits under `env/lib/python3.x/site-packages/...` the
//     label sharpens to that deepest shared prefix.
//  4. The top-level dir for each bucket joins autoSkipDirs so the
//     next shell command's walk doesn't re-traverse it.
//
// Honest cross-directory refactors above the threshold (e.g. a
// 250-file rename touching 25 top-level dirs) collapse into 25 rollup
// rows — still terse, still recognizable. The model's primary edit
// tools (write_file, edit_file) bypass this code path entirely, so the
// rollup never absorbs intentional per-file work.
func (ct *ChangeTracker) emitWithBulkRollup(pending []pendingShellMutation, toolCall string) {
	workspaceRoot := ""
	if ct.agent != nil {
		workspaceRoot = ct.agent.GetWorkspaceRoot()
	}
	absWorkspace := workspaceRoot
	if workspaceRoot != "" {
		if abs, err := filepath.Abs(workspaceRoot); err == nil {
			if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil {
				absWorkspace = resolved
			} else {
				absWorkspace = abs
			}
		}
	}

	type bucket struct {
		topDir string // workspace-relative top-level dir, "" for root-level
		items  []pendingShellMutation
	}
	buckets := map[string]*bucket{}
	bucketOrder := []string{} // preserve deterministic emit order

	for _, p := range pending {
		topDir := topLevelDirRelativeTo(absWorkspace, p.path)
		b, ok := buckets[topDir]
		if !ok {
			b = &bucket{topDir: topDir}
			buckets[topDir] = b
			bucketOrder = append(bucketOrder, topDir)
		}
		b.items = append(b.items, p)
	}

	for _, key := range bucketOrder {
		b := buckets[key]
		// Root-level files: always per-file. There's no honest
		// directory label and these are typically meaningful
		// config / lockfile / README edits the user wants to see.
		//
		// Light buckets (below shellBulkBucketMin): also per-file.
		// These are the handful of source edits riding along with a
		// heavy build/install command. Rolling them up would replace
		// "src/lib.go was modified" with "src/ — 1 file" and lose the
		// view/revert affordance for an edit the user almost certainly
		// cares about. The PRIMARY rollup gate (shellBulkThreshold)
		// already fired at the COMMAND level above; this secondary
		// filter just exempts the light buckets from collapse.
		if b.topDir == "" || len(b.items) < shellBulkBucketMin {
			for _, p := range b.items {
				ct.appendShellMutation(p.path, p.before, p.after, p.op, toolCall)
			}
			continue
		}
		label := deepestCommonAncestorRel(absWorkspace, b.items)
		if label == "" {
			label = b.topDir
		}
		// Convert pendingShellMutation (build-rollup's internal shape)
		// into the canonical pendingShellChange so packBulkItems can
		// process both bulk paths through one helper.
		asChanges := make([]pendingShellChange, len(b.items))
		for i, it := range b.items {
			asChanges[i] = pendingShellChange{Path: it.path, Op: it.op, Before: it.before, After: it.after}
		}
		ct.appendBulkRollup(label, asChanges, absWorkspace, toolCall)
		ct.addAutoSkipDir(absWorkspace, b.topDir)
	}
}

// topLevelDirRelativeTo returns the first path component of `path`
// relative to `workspaceRoot`, or "" when the path is at the
// workspace root (no nested directory) or outside it.
func topLevelDirRelativeTo(workspaceRoot, path string) string {
	if workspaceRoot == "" {
		return ""
	}
	absPath := path
	if abs, err := filepath.Abs(path); err == nil {
		absPath = abs
	}
	rel, err := filepath.Rel(workspaceRoot, absPath)
	if err != nil {
		return ""
	}
	if rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	idx := strings.IndexRune(rel, filepath.Separator)
	if idx < 0 {
		return "" // file lives directly at workspace root
	}
	return rel[:idx]
}

// deepestCommonAncestorRel returns the deepest workspace-relative
// directory that contains every path in `items`. Operates on the
// parent directory of each path (because rollups represent the
// containing folder, not the files themselves). Falls back to "" if
// any path can't be made workspace-relative or if there's no shared
// prefix beyond the workspace root.
func deepestCommonAncestorRel(workspaceRoot string, items []pendingShellMutation) string {
	if workspaceRoot == "" || len(items) == 0 {
		return ""
	}
	sep := string(filepath.Separator)
	var commonSegs []string
	initialized := false
	for _, it := range items {
		absPath := it.path
		if abs, err := filepath.Abs(it.path); err == nil {
			absPath = abs
		}
		rel, err := filepath.Rel(workspaceRoot, absPath)
		if err != nil {
			return ""
		}
		if rel == "." || strings.HasPrefix(rel, "..") {
			return ""
		}
		dir := filepath.Dir(rel)
		var segs []string
		if dir == "." {
			segs = []string{}
		} else {
			segs = strings.Split(dir, sep)
		}
		if !initialized {
			commonSegs = segs
			initialized = true
			continue
		}
		// Truncate commonSegs to the shared prefix.
		n := len(commonSegs)
		if len(segs) < n {
			n = len(segs)
		}
		i := 0
		for ; i < n; i++ {
			if commonSegs[i] != segs[i] {
				break
			}
		}
		commonSegs = commonSegs[:i]
		if len(commonSegs) == 0 {
			break
		}
	}
	return strings.Join(commonSegs, sep)
}

// appendBulkRollup records a single TrackedFileChange that stands in
// for the files churned under `dir`. The path stored is workspace-
// relative with a trailing slash so the UI can recognise the entry as
// a directory rollup at a glance.
//
// Items get packed into BulkItems via packBulkItems so the bulk row is
// recoverable per-file. When the cumulative byte total exceeds the
// payload cap, BulkItems is left empty and the entry behaves as the
// historical count-only row — the UI/tools can detect this via
// BulkCount > 0 && len(BulkItems) == 0.
func (ct *ChangeTracker) appendBulkRollup(dir string, items []pendingShellChange, workspaceRoot, toolCall string) {
	displayPath := dir + string(filepath.Separator)
	packed, overBudget := ct.packBulkItems(items)
	if overBudget {
		log.Printf("[change-tracker] bulk rollup over %d-byte cap (%d files) for %q from %q — recording count-only entry and adding to auto-skip", shellDestructiveBulkMaxPayloadBytes, len(items), displayPath, toolCall)
	} else {
		log.Printf("[change-tracker] bulk rollup: %s (%d files) from %q — recording with %d packed items and adding to auto-skip", displayPath, len(items), toolCall, len(packed))
	}
	entry := TrackedFileChange{
		FilePath:  displayPath,
		Operation: "bulk",
		Timestamp: time.Now(),
		ToolCall:  toolCall,
		BulkCount: len(items),
	}
	if !overBudget {
		entry.BulkItems = packed
	}
	ct.appendChange(entry)
	// Publish the rollup over the bus too so the UI can refresh.
	if ct.agent != nil && ct.agent.eventBus != nil {
		absDir := filepath.Join(workspaceRoot, dir)
		ct.agent.eventBus.Publish(
			events.EventTypeFileChanged,
			events.FileChangedEvent(absDir, "shell_bulk", toolCall),
		)
	}
}

// shellDestructiveBulkThreshold is the per-command mutation count above
// which a destructive shell command (e.g. `git checkout .`) collapses
// into a single recoverable bulk entry instead of N per-file rows.
// Set deliberately low (10) — once a destructive op touches enough
// files to be hard to scan flat, the bulk row's expand-to-recover UX
// wins. Below the threshold the per-file shape keeps small reverts
// easy to read in the changes panel.
var shellDestructiveBulkThreshold = 10

// shellDestructiveBulkMaxPayloadBytes caps the total before+after
// content packed into a single recoverable bulk entry. Same ceiling as
// the walk's content budget (shellSnapshotMaxTotalBytes, 32 MiB) so we
// never hold more bytes for recovery than the walker would have read
// in the first place. Past the cap we degrade the bulk to count-only
// and the entry is reported as !Recoverable.
const shellDestructiveBulkMaxPayloadBytes = shellSnapshotMaxTotalBytes

// appendDestructiveBulkRollup records a single TrackedFileChange that
// stands in for an entire destructive shell command (e.g. `git
// checkout .`) reverting many files. Unlike the build-rollup path,
// every per-file mutation is packed into the entry's BulkItems so the
// recovery tools can:
//
//   - restore one specific file inside the bulk (recover_file matches
//     against BulkItems), or
//   - restore everything in one shot (recover_bulk).
//
// When the cumulative before+after payload exceeds the cap, the entry
// degrades to a count-only bulk (BulkItems == nil) and we log a warning
// — the user can still see "300 files reverted" but per-file recovery
// is out of reach. This is the same upper bound the walker applies, so
// hitting it means we ran into a workspace that's outside the change
// tracker's overall budget regardless of bulk shape.
func (ct *ChangeTracker) appendDestructiveBulkRollup(pending []pendingShellChange, toolCall string) {
	items, overBudget := ct.packBulkItems(pending)

	entry := TrackedFileChange{
		FilePath:  toolCall, // command label — the UI renders this as the bulk row's heading
		Operation: "bulk",
		Timestamp: time.Now(),
		ToolCall:  toolCall,
		BulkCount: len(pending),
	}
	if overBudget {
		log.Printf("[change-tracker] destructive bulk over %d-byte cap (%d files); recording count-only entry from %q", shellDestructiveBulkMaxPayloadBytes, len(pending), toolCall)
	} else {
		entry.BulkItems = items
	}
	ct.appendChange(entry)

	// Publish a file-changed event so the UI refreshes. The "path" here
	// is the command label, not a real file path; the changes-panel
	// renderer is the source of truth for resolving bulk entries.
	if ct.agent != nil && ct.agent.eventBus != nil {
		ct.agent.eventBus.Publish(
			events.EventTypeFileChanged,
			events.FileChangedEvent(toolCall, "shell_bulk", toolCall),
		)
	}
}

// packBulkItems extracts the per-file recovery payload from each
// pending mutation, stopping when the cumulative byte total exceeds
// shellDestructiveBulkMaxPayloadBytes. Returns the items collected so
// far (may be empty) and an overBudget flag — callers should drop the
// items when overBudget is true so a count-only bulk row is recorded
// without misleading partial recovery.
func (ct *ChangeTracker) packBulkItems(pending []pendingShellChange) ([]TrackedBulkItem, bool) {
	items := make([]TrackedBulkItem, 0, len(pending))
	var payloadBytes int64
	for _, p := range pending {
		original := ""
		newer := ""
		if p.Before != nil {
			if p.Before.Content != nil {
				original = string(p.Before.Content)
			} else if p.Before.Skipped != "" {
				original = "[CONTENT NOT CAPTURED: " + p.Before.Skipped + "]"
			}
		}
		if p.After != nil {
			if p.After.Content != nil {
				newer = string(p.After.Content)
			} else if p.After.Skipped != "" {
				newer = "[CONTENT NOT CAPTURED: " + p.After.Skipped + "]"
			}
		}
		if ct.isOutsideWorkspace(p.Path) {
			original = RedactedContentMarker
			newer = RedactedContentMarker
		}
		payloadBytes += int64(len(original)) + int64(len(newer))
		if payloadBytes > shellDestructiveBulkMaxPayloadBytes {
			return nil, true
		}
		items = append(items, TrackedBulkItem{
			FilePath:     p.Path,
			OriginalCode: original,
			NewCode:      newer,
			Operation:    p.Op,
		})
	}
	return items, false
}

// appendShellMutation appends a TrackedFileChange given before/after
// snapshot entries. Either pointer may be nil (creation has no before;
// deletion has no after).
func (ct *ChangeTracker) appendShellMutation(path string, before, after *shellSnapshotEntry, op, toolCall string) {
	originalCode := ""
	newCode := ""
	if before != nil {
		if before.Content != nil {
			originalCode = string(before.Content)
		} else if before.Skipped != "" {
			// Path-only record: we know the file existed but its
			// content was filtered out (too big / binary). Use a
			// sentinel so the manifest can communicate "we have no
			// recovery payload" without faking original content.
			originalCode = "[CONTENT NOT CAPTURED: " + before.Skipped + "]"
		}
	}
	if after != nil {
		if after.Content != nil {
			newCode = string(after.Content)
		} else if after.Skipped != "" {
			newCode = "[CONTENT NOT CAPTURED: " + after.Skipped + "]"
		}
	}

	if ct.isOutsideWorkspace(path) {
		originalCode = RedactedContentMarker
		newCode = RedactedContentMarker
	}

	ct.appendChange(TrackedFileChange{
		FilePath:     path,
		OriginalCode: originalCode,
		NewCode:      newCode,
		Operation:    op,
		Timestamp:    time.Now(),
		ToolCall:     toolCall,
	})

	// Publish a file_changed event so the WebUI activity feed surfaces
	// shell-driven mutations in real time, not just direct-tool edits.
	// Action vocabulary mirrors events.FileChangedEvent ("created" /
	// "modified" / "deleted"); map our internal ops accordingly.
	if ct.agent != nil {
		action := "modified"
		switch op {
		case "create", "write":
			action = "created"
		case "delete":
			action = "deleted"
		}
		// For deletes, send the original content (lets the WebUI offer
		// a one-click recover button); for creates, send the new
		// content; for edits, send the new content (the file's
		// current state).
		eventContent := newCode
		if op == "delete" {
			eventContent = originalCode
		}
		ct.agent.PublishFileChange(path, action, eventContent)
	}
}
