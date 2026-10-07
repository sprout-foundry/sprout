// checkpoint.go — checkpoints over the recorded change history: a named,
// restorable marker of a project state.
//
// A checkpoint names the revision that was current when it was taken, plus
// the revision's file set as a summary, so it identifies a project state
// without duplicating the change payloads the history already stores. The
// design leans on pkg/history's existing revision data (GetRevisionGroups,
// RevisionGroup, RevertChangeByRevisionID) rather than a parallel mechanism:
// restoring a checkpoint is one RevertChangeByRevisionID call on the
// revision it captures.
//
// Checkpoints are created automatically when verification passes and when a
// deploy completes, and on demand. Every creation — including the creation
// that a restore records — is stored as a small record under
// .sprout/checkpoints/, so the recorded history of checkpoints (and of the
// restores between them) is itself the timeline the rest of the feature
// reads.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

const (
	projectCheckpointsDir = ".sprout/checkpoints"
	checkpointFileSuffix  = ".json"
)

var (
	checkpointMu   sync.RWMutex
	checkpointsDir string = projectCheckpointsDir
)

// CheckpointOrigin identifies why a checkpoint was created: automatically at
// a passing verification, automatically at a completed deploy, manually on
// demand, or as the record of a restore.
type CheckpointOrigin string

const (
	// CheckpointVerification is a checkpoint taken automatically by the
	// turn-end verification hook when the run passed.
	CheckpointVerification CheckpointOrigin = "verification"
	// CheckpointDeploy is a checkpoint taken automatically when a deploy
	// completes.
	CheckpointDeploy CheckpointOrigin = "deploy"
	// CheckpointManual is a checkpoint taken on demand.
	CheckpointManual CheckpointOrigin = "manual"
	// CheckpointRestore is the record a restore itself creates, so the
	// timeline shows the restore alongside the state it returned to. The
	// captured revision is the restore target.
	CheckpointRestore CheckpointOrigin = "restore"
)

// Checkpoint is a recorded marker of a project state. It captures the
// revision that was current when it was taken (RevisionID) — the identity a
// restore acts on — and a short summary of that revision's files. It is
// plain JSON-serialisable data so the store can persist it under
// .sprout/checkpoints/.
type Checkpoint struct {
	// ID is the checkpoint's stable identity.
	ID string `json:"id"`
	// Timestamp is when the checkpoint was created.
	Timestamp time.Time `json:"timestamp"`
	// Origin is why the checkpoint was created.
	Origin CheckpointOrigin `json:"origin"`
	// Summary is a short human-readable description of the state captured.
	Summary string `json:"summary,omitempty"`
	// RevisionID is the revision that was current when the checkpoint was
	// taken — the change set a restore reverts to.
	RevisionID string `json:"revision_id,omitempty"`
	// Files is the sorted list of files the captured revision touched.
	Files []string `json:"files,omitempty"`
	// ScopeIDs are the plan scope IDs the checkpoint pertains to, when the
	// caller supplied them. Empty otherwise.
	ScopeIDs []string `json:"scope_ids,omitempty"`
}

// NewCheckpoint builds a checkpoint from the given captured revision (the
// revision current when the checkpoint is taken) and origin. The ID is
// derived deterministically from the origin, the revision, and the
// timestamp, so two checkpoints taken at the same instant for the same state
// are the same record. captured may be nil (no revision to capture), which
// yields a checkpoint with no revision and no files. scopeIDs and summary
// are optional.
func NewCheckpoint(origin CheckpointOrigin, captured *RevisionGroup, scopeIDs []string, summary string) Checkpoint {
	cp := Checkpoint{
		Origin:   origin,
		Summary:  summary,
		ScopeIDs: append([]string(nil), scopeIDs...),
	}
	if captured != nil {
		cp.RevisionID = captured.RevisionID
		cp.Files = revisionFileList(captured)
	}
	cp.Timestamp = time.Now().UTC()
	cp.ID = newCheckpointID(cp.Origin, cp.RevisionID, cp.Timestamp)
	return cp
}

// SaveCheckpoint appends a checkpoint to the store, assigning an ID when the
// checkpoint does not already carry one. It returns the stored checkpoint.
func SaveCheckpoint(cp Checkpoint) (Checkpoint, error) {
	if err := ensureCheckpointsDir(); err != nil {
		return Checkpoint{}, err
	}
	if cp.Timestamp.IsZero() {
		cp.Timestamp = time.Now().UTC()
	}
	if cp.ID == "" {
		cp.ID = newCheckpointID(cp.Origin, cp.RevisionID, cp.Timestamp)
	}
	if err := writeCheckpointFile(cp); err != nil {
		return Checkpoint{}, err
	}
	return cp, nil
}

// CreateCheckpoint captures the currently current revision (the most recent
// revision group in the process history store, if any) and stores a
// checkpoint for it. It is the on-demand entry point for callers (the
// checkpoint tool) whose process history store is the project's — the CLI
// runs in the project root, so the process store and the workspace store are
// the same. summary and scopeIDs are optional. A history with no revisions
// still yields a checkpoint (with no captured revision); the checkpoint
// records that the project was at a known state regardless.
func CreateCheckpoint(origin CheckpointOrigin, summary string, scopeIDs []string) (Checkpoint, error) {
	return createCheckpointIn(GetCheckpointsDir(), origin, summary, scopeIDs)
}

// CreateCheckpointInWorkspace is CreateCheckpoint scoped to a specific
// workspace: the checkpoint is stored under <workspace>/.sprout/checkpoints/.
// Like CreateCheckpoint it captures the revision current in the process
// history store; use CreateCheckpointForRevision when the caller knows the
// exact revision that produced the state (the automatic verification and
// deploy seams do), so the captured revision is the one that actually
// produced the outcome rather than merely the most recent in the store. An
// empty workspace falls back to the process-wide store.
func CreateCheckpointInWorkspace(workspace string, origin CheckpointOrigin, summary string, scopeIDs []string) (Checkpoint, error) {
	return createCheckpointIn(checkpointDirForWorkspace(workspace), origin, summary, scopeIDs)
}

// CreateCheckpointForRevision stores a checkpoint under the workspace's
// checkpoint store that captures exactly revisionID — the revision the
// caller knows produced the state — without consulting the process history
// store. It is the form the automatic seams use: a passing verification or a
// completed deploy knows the revision it acted on, so its checkpoint
// identifies that revision rather than whichever revision happens to be most
// recent in the process store. An empty revisionID records a checkpoint with
// no captured revision (nothing to restore), which is preferable to
// capturing an unrelated revision. summary and scopeIDs are optional.
func CreateCheckpointForRevision(workspace string, origin CheckpointOrigin, revisionID, summary string, scopeIDs []string) (Checkpoint, error) {
	dir := checkpointDirForWorkspace(workspace)
	cp := Checkpoint{
		Origin:     origin,
		Summary:    summary,
		ScopeIDs:   append([]string(nil), scopeIDs...),
		RevisionID: strings.TrimSpace(revisionID),
	}
	cp.Timestamp = time.Now().UTC()
	if cp.RevisionID != "" {
		cp.Files = filesForRevisionID(cp.RevisionID)
	}
	cp.ID = newCheckpointID(cp.Origin, cp.RevisionID, cp.Timestamp)
	if err := writeCheckpointFileIn(dir, cp); err != nil {
		return Checkpoint{}, err
	}
	return cp, nil
}

// filesForRevisionID returns the sorted file list of the named revision from
// the process history store, or nil when the revision is unknown. It is a
// best-effort summary enrichment: a checkpoint is still valid without it.
func filesForRevisionID(revisionID string) []string {
	groups, err := GetRevisionGroups()
	if err != nil {
		return nil
	}
	for i := range groups {
		if groups[i].RevisionID == revisionID {
			return revisionFileList(&groups[i])
		}
	}
	return nil
}

func createCheckpointIn(dir string, origin CheckpointOrigin, summary string, scopeIDs []string) (Checkpoint, error) {
	groups, err := GetRevisionGroups()
	if err != nil {
		return Checkpoint{}, fmt.Errorf("reading revision groups: %w", err)
	}
	var captured *RevisionGroup
	if len(groups) > 0 {
		// GetRevisionGroups returns most recent first.
		captured = &groups[0]
	}
	cp := NewCheckpoint(origin, captured, scopeIDs, summary)
	if err := writeCheckpointFileIn(dir, cp); err != nil {
		return Checkpoint{}, err
	}
	return cp, nil
}

// ListCheckpoints returns the stored checkpoints ordered by timestamp.
// orderNewestFirst reverses the natural oldest-first order. A store that
// does not exist yet reads as an empty list, not an error.
func ListCheckpoints(orderNewestFirst bool) ([]Checkpoint, error) {
	return listCheckpointsIn(GetCheckpointsDir(), orderNewestFirst)
}

// ListCheckpointsInWorkspace is ListCheckpoints scoped to a specific
// workspace: it reads the checkpoints stored under
// <workspace>/.sprout/checkpoints/, the same store CreateCheckpointInWorkspace
// writes to. An empty workspace falls back to the process-wide store. It is
// the read side the timeline builder and the checkpoint tool use, so a
// checkpoint the automatic seams captured appears to the reader that looks
// for it.
func ListCheckpointsInWorkspace(workspace string, orderNewestFirst bool) ([]Checkpoint, error) {
	return listCheckpointsIn(checkpointDirForWorkspace(workspace), orderNewestFirst)
}

func listCheckpointsIn(dir string, orderNewestFirst bool) ([]Checkpoint, error) {
	// A read never creates the store: a missing directory is an empty
	// checkpoint list. Only the write path (writeCheckpointFileIn) creates
	// it, so reading a timeline is not an mkdir side effect under a
	// caller-supplied path.
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Checkpoint{}, nil
		}
		return nil, fmt.Errorf("reading checkpoints directory: %w", err)
	}

	var out []Checkpoint
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), checkpointFileSuffix) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		cp, err := readCheckpointFile(path)
		if err != nil {
			// A single unreadable record never hides the rest of the store.
			continue
		}
		out = append(out, cp)
	}

	sortCheckpoints(out, orderNewestFirst)
	return out, nil
}

// GetCheckpoint returns the stored checkpoint with the given ID.
func GetCheckpoint(id string) (Checkpoint, bool, error) {
	return getCheckpointIn(GetCheckpointsDir(), id)
}

// GetCheckpointInWorkspace is GetCheckpoint scoped to a specific workspace's
// checkpoint store. An empty workspace falls back to the process-wide store.
func GetCheckpointInWorkspace(workspace, id string) (Checkpoint, bool, error) {
	return getCheckpointIn(checkpointDirForWorkspace(workspace), id)
}

func getCheckpointIn(dir, id string) (Checkpoint, bool, error) {
	all, err := listCheckpointsIn(dir, false)
	if err != nil {
		return Checkpoint{}, false, err
	}
	for _, cp := range all {
		if cp.ID == id {
			return cp, true, nil
		}
	}
	return Checkpoint{}, false, nil
}

// RestoreCheckpoint restores the project to the state a checkpoint captured
// by reverting the revision the checkpoint names, and records the restore
// itself as a checkpoint (origin CheckpointRestore). Restoring is therefore
// one action and is itself a timeline entry: the returned restore checkpoint
// carries the same captured revision, so the timeline shows the restore next
// to the state it returned to and nothing is lost.
//
// The revert reuses the existing pkg/history revision revert
// (RevertChangeByRevisionID), including its workspace and staleness guards.
func RestoreCheckpoint(id string) (Checkpoint, error) {
	return restoreCheckpointIn(GetCheckpointsDir(), id)
}

// RestoreCheckpointInWorkspace is RestoreCheckpoint scoped to a specific
// workspace's checkpoint store: the checkpoint is read from
// <workspace>/.sprout/checkpoints/ (the store the automatic seams write to)
// and the restore record is written back there. An empty workspace falls back
// to the process-wide store.
func RestoreCheckpointInWorkspace(workspace, id string) (Checkpoint, error) {
	return restoreCheckpointIn(checkpointDirForWorkspace(workspace), id)
}

func restoreCheckpointIn(dir, id string) (Checkpoint, error) {
	cp, found, err := getCheckpointIn(dir, id)
	if err != nil {
		return Checkpoint{}, err
	}
	if !found {
		return Checkpoint{}, fmt.Errorf("checkpoint %q not found", id)
	}
	if cp.RevisionID == "" {
		return Checkpoint{}, fmt.Errorf("checkpoint %q captures no revision to restore", id)
	}
	if err := RevertChangeByRevisionID(cp.RevisionID); err != nil {
		return Checkpoint{}, fmt.Errorf("restoring checkpoint %q: %w", id, err)
	}

	restore := cp
	restore.Origin = CheckpointRestore
	restore.Timestamp = time.Now().UTC()
	restore.Summary = "restored checkpoint " + cp.ID
	restore.ID = newCheckpointID(restore.Origin, cp.RevisionID, restore.Timestamp)
	if err := writeCheckpointFileIn(dir, restore); err != nil {
		return Checkpoint{}, err
	}
	return restore, nil
}

// SetCheckpointsDirForTesting redirects the checkpoint store to a temp path
// for cross-package tests that must not touch the shared .sprout tree. Pair
// it with GetCheckpointsDirForTesting to restore the previous value in
// t.Cleanup.
func SetCheckpointsDirForTesting(dir string) {
	checkpointMu.Lock()
	checkpointsDir = dir
	checkpointMu.Unlock()
}

// GetCheckpointsDir returns the checkpoint store directory path.
func GetCheckpointsDir() string {
	checkpointMu.RLock()
	defer checkpointMu.RUnlock()
	return checkpointsDir
}

func ensureCheckpointsDir() error {
	if err := filesystem.EnsureDir(GetCheckpointsDir()); err != nil {
		return fmt.Errorf("creating checkpoints directory: %w", err)
	}
	return nil
}

// checkpointDirForWorkspace resolves the checkpoint store directory for a
// workspace: <workspace>/.sprout/checkpoints/ when a workspace is given, else
// the process-wide store. It is the single place reads and writes agree on
// which store a workspace uses.
func checkpointDirForWorkspace(workspace string) string {
	if strings.TrimSpace(workspace) == "" {
		return GetCheckpointsDir()
	}
	return filepath.Join(workspace, ".sprout", "checkpoints")
}

func writeCheckpointFile(cp Checkpoint) error {
	return writeCheckpointFileIn(GetCheckpointsDir(), cp)
}

func writeCheckpointFileIn(dir string, cp Checkpoint) error {
	payload, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding checkpoint %q: %w", cp.ID, err)
	}
	if err := filesystem.EnsureDir(dir); err != nil {
		return fmt.Errorf("creating checkpoints directory: %w", err)
	}
	name := SafeCheckpointFilename(cp.ID) + checkpointFileSuffix
	path := filepath.Join(dir, name)
	if err := filesystem.WriteFileWithDir(path, payload, 0644); err != nil {
		return fmt.Errorf("writing checkpoint %q: %w", cp.ID, err)
	}
	return nil
}

func readCheckpointFile(path string) (Checkpoint, error) {
	raw, err := filesystem.ReadFileBytes(path)
	if err != nil {
		return Checkpoint{}, err
	}
	var cp Checkpoint
	if err := json.Unmarshal(raw, &cp); err != nil {
		return Checkpoint{}, fmt.Errorf("decoding checkpoint %s: %w", path, err)
	}
	return cp, nil
}

func sortCheckpoints(cps []Checkpoint, newestFirst bool) {
	sort.SliceStable(cps, func(i, j int) bool {
		a, b := cps[i], cps[j]
		if !a.Timestamp.Equal(b.Timestamp) {
			if newestFirst {
				return a.Timestamp.After(b.Timestamp)
			}
			return a.Timestamp.Before(b.Timestamp)
		}
		if newestFirst {
			return a.ID > b.ID
		}
		return a.ID < b.ID
	})
}

// SafeCheckpointFilename reduces a checkpoint ID to a filesystem-safe base
// name: path separators and traversal sequences are replaced so an ID can
// never escape the checkpoint store directory.
func SafeCheckpointFilename(id string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", "..", "_", "\x00", "_")
	name := replacer.Replace(strings.TrimSpace(id))
	if name == "" {
		name = "checkpoint"
	}
	return name
}

func revisionFileList(group *RevisionGroup) []string {
	seen := make(map[string]struct{})
	var files []string
	for i := range group.Changes {
		change := &group.Changes[i]
		if change.Filename == "" {
			continue
		}
		if _, ok := seen[change.Filename]; ok {
			continue
		}
		seen[change.Filename] = struct{}{}
		files = append(files, change.Filename)
	}
	sort.Strings(files)
	return files
}

func newCheckpointID(origin CheckpointOrigin, revisionID string, ts time.Time) string {
	var b strings.Builder
	b.WriteString(string(origin))
	b.WriteString("-")
	if revisionID != "" {
		b.WriteString(revisionID)
	} else {
		b.WriteString("no-revision")
	}
	b.WriteString("-")
	b.WriteString(ts.UTC().Format("20060102T150405.000000000Z"))
	return b.String()
}
