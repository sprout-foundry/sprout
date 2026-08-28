// ETH-2 "transactional escalation": the container-side execution surface of
// a three-phase transaction the platform drives against a workspace
// container — push deltas (browser→container), run a command, pull deltas
// (container→browser). The browser keeps a WASM editing plane; this package
// is the executor the daemon exposes over HTTP and the CLI wraps.
//
// The JSON shapes below are PINNED CONTRACTS — field names, nesting and
// zero-values must not change without coordinating with the platform parser
// (see docs/txn-protocol.md, the cross-repo contract doc).
//
// Everything here is intentionally dependency-free: it shells out to git,
// touches no configuration, no LLM and no agent state, so the same entry
// points serve the CLI (`sprout txn-status` …) and the daemon (running
// in-container as root against /work) with identical output.
package txn

import "time"

// Caps and defaults of the contract. They bound both directions: a push
// manifest may not exceed them, and a pull manifest reports over-cap entries
// in "skipped" instead of inlining them.
//
// The three manifest caps are vars (not consts) for the same reason
// pkg/git.SyncGitTimeout is: tests tighten them rather than allocating a
// real 100 MiB fixture. Their DEFAULT values below are the contract —
// TestContractCapsIsPinned guards them against drift.
var (
	// MaxFileBytes is the per-file decoded-content cap (5 MiB).
	MaxFileBytes = 5 << 20
	// MaxFileCount is the per-manifest file cap.
	MaxFileCount = 2000
	// MaxTotalBytes is the per-manifest total decoded cap (100 MiB).
	MaxTotalBytes = 100 << 20
)

// Remaining caps and defaults. Consts: nothing needs to vary them in tests.
const (
	// MaxRequestBytes is the HTTP request-body cap the daemon enforces on
	// /api/txn/push — the transport-level mirror of MaxTotalBytes.
	MaxRequestBytes = 100 << 20
	// MaxOutputBytes is the rolling per-stream (stdout, stderr) cap kept
	// from a run: the LAST 256 KiB of each stream.
	MaxOutputBytes = 256 << 10
	// DefaultTimeoutSeconds is used when a run request omits or zeroes the
	// timeout.
	DefaultTimeoutSeconds = 600
	// MaxTimeoutSeconds is the hard ceiling on a requested timeout.
	MaxTimeoutSeconds = 900
	// TimeoutExitCode is the exit_code reported for a timed-out run (the
	// GNU timeout convention).
	TimeoutExitCode = 124
	// StartFailureExitCode is reported when /bin/sh could not even be
	// started (missing workdir, exec failure) — POSIX 126, "found but not
	// executable"-adjacent.
	StartFailureExitCode = 126
	// DefaultFileMode and DefaultDirMode are used when a manifest omits
	// "mode".
	DefaultFileMode = 0o644
	DefaultDirMode  = 0o755
)

// Skip reasons. "reason" is a free-form string in the wire contract, but the
// values below are the pinned vocabulary (docs/txn-protocol.md); the
// platform matches on them to explain a partial apply/pull in the UI.
const (
	SkipReasonEmptyPath        = "empty_path"
	SkipReasonNulInPath        = "nul_in_path"
	SkipReasonAbsolutePath     = "absolute_path"
	SkipReasonPathTraversal    = "path_traversal"
	SkipReasonGitPath          = "git_path"
	SkipReasonInvalidPath      = "invalid_path"
	SkipReasonInvalidBase64    = "invalid_base64"
	SkipReasonInvalidMode      = "invalid_mode"
	SkipReasonExceedsPerFile   = "exceeds_per_file_cap"
	SkipReasonExceedsFileCount = "exceeds_file_count_cap"
	SkipReasonExceedsTotal     = "exceeds_total_cap"
	SkipReasonSymlinkEscape    = "symlink_escape"
	SkipReasonWriteFailed      = "write_failed"
	SkipReasonDeleteFailed     = "delete_failed"
	SkipReasonDeleteMissing    = "delete_missing"
	SkipReasonNotAFile         = "not_a_file"
	SkipReasonSymlink          = "symlink"
	SkipReasonReadFailed       = "read_failed"
)

// Apply/pull statuses.
const (
	// StatusOK: every entry in the request was applied (push) or the
	// manifest describes the whole tree (pull).
	StatusOK = "ok"
	// StatusPartial: at least one entry was skipped.
	StatusPartial = "partial"
)

// ApplyStatus values.
const (
	TxnClientWASM      = "wasm"
	TxnClientContainer = "container"
)

// DeltaBase identifies what a manifest was computed against. On a push it is
// the browser's editing base; on a pull it is the container's HEAD.
type DeltaBase struct {
	GitSha string `json:"git_sha"`
	Client string `json:"client"`
}

// DeltaFile is one file entry of a manifest. Size is the decoded byte count
// (advisory on push: the container decodes and measures for itself).
type DeltaFile struct {
	Path          string `json:"path"`
	ContentBase64 string `json:"content_base64"`
	Size          int    `json:"size"`
	Mode          string `json:"mode"`
}

// SkippedEntry records one manifest entry that was not transferred, and why.
type SkippedEntry struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// DeltaManifest is the pinned shape #1: the push request body and the pull
// response body.
//
//	{
//	  "base": {"git_sha": "", "client": "wasm"},
//	  "files": [{"path": "src/main.go", "content_base64": "...", "size": 123, "mode": "0644"}],
//	  "deletes": ["old.go"],
//	  "truncated": false,
//	  "skipped": [{"path": "x.bin", "reason": "exceeds_per_file_cap"}]
//	}
//
// Paths are repo-relative with forward slashes. Files/Deletes/Skipped are
// always arrays on the wire (never null).
type DeltaManifest struct {
	Base      DeltaBase      `json:"base"`
	Files     []DeltaFile    `json:"files"`
	Deletes   []string       `json:"deletes"`
	Truncated bool           `json:"truncated"`
	Skipped   []SkippedEntry `json:"skipped"`
}

// ApplyResult is the pinned POST /api/txn/push response.
//
//	{"applied": 3, "deleted": 1, "skipped": [...], "status": "ok"|"partial"}
type ApplyResult struct {
	Applied int            `json:"applied"`
	Deleted int            `json:"deleted"`
	Skipped []SkippedEntry `json:"skipped"`
	Status  string         `json:"status"`
}

// Status is the pinned shape #3 (GET /api/txn/status, `sprout txn-status`).
//
//	{
//	  "in_git_repo": true, "branch": "main",
//	  "dirty_files": ["a.go"], "untracked_files": ["b.out"], "deleted_files": ["c.go"],
//	  "total_changes": 3, "timestamp": "RFC3339"
//	}
type Status struct {
	InGitRepo      bool     `json:"in_git_repo"`
	Branch         string   `json:"branch"`
	DirtyFiles     []string `json:"dirty_files"`
	UntrackedFiles []string `json:"untracked_files"`
	DeletedFiles   []string `json:"deleted_files"`
	TotalChanges   int      `json:"total_changes"`
	Timestamp      string   `json:"timestamp"`
}

// RunRequest is the pinned POST /api/txn/run request body.
//
//	{"command": "go build ./...", "timeout_seconds": 600, "workdir": ""}
type RunRequest struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Workdir        string `json:"workdir"`
}

// RunResult is the pinned POST /api/txn/run response.
//
//	{"stdout": "...", "stderr": "...", "exit_code": 0, "duration_ms": 1234,
//	 "timed_out": false, "truncated": false}
type RunResult struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out"`
	Truncated  bool   `json:"truncated"`
}

// newManifest returns the zero manifest: empty (never null) lists.
func newManifest() DeltaManifest {
	return DeltaManifest{
		Files:   []DeltaFile{},
		Deletes: []string{},
		Skipped: []SkippedEntry{},
	}
}

// newApplyResult returns the zero apply result with an empty skipped list.
func newApplyResult() ApplyResult {
	return ApplyResult{Skipped: []SkippedEntry{}, Status: StatusOK}
}

// newStatus returns the zero status: empty lists, timestamp set.
func newStatus() Status {
	return Status{
		DirtyFiles:     []string{},
		UntrackedFiles: []string{},
		DeletedFiles:   []string{},
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
	}
}
