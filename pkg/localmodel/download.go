package localmodel

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// DownloadStatusPayload is the JSON-facing snapshot of a download job's
// state. Values are copied out under the job's lock, so holders can't
// observe torn state.
type DownloadStatusPayload struct {
	ModelID string `json:"model_id"`
	Status  string `json:"status"` // "downloading" | "completed" | "failed" | "canceled"
	Bytes   int64  `json:"bytes_downloaded"`
	Total   int64  `json:"total_bytes,omitempty"` // 0 = unknown (see pollDownloadProgress)
	Error   string `json:"error,omitempty"`
}

// downloadJob is one in-process model download. Downloads run as
// goroutines inside sprout's process (the same EnsureModel path the CLI
// uses — no external llm_download binary), so directory resolution,
// HFInclude handling, and tokenizer patching can't drift between entry
// points. Progress is polled from disk (the hf subprocess exposes no
// total).
type downloadJob struct {
	mu      sync.Mutex
	status  string // "downloading" | "completed" | "failed" | "canceled"
	bytes   int64
	total   int64
	errStr  string
	started time.Time
	ended   time.Time
	cancel  context.CancelFunc
}

func (j *downloadJob) snapshot() DownloadStatusPayload {
	j.mu.Lock()
	defer j.mu.Unlock()
	return DownloadStatusPayload{
		Status: j.status,
		Bytes:  j.bytes,
		Total:  j.total,
		Error:  j.errStr,
	}
}

func (j *downloadJob) running() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status == "downloading"
}

// downloadRegistry is the process-wide set of download jobs, keyed by
// catalog model name. One download per model at a time; stale
// terminal-status entries are replaced by a new StartDownload.
type downloadRegistry struct {
	mu   sync.Mutex
	jobs map[string]*downloadJob
}

var downloads = &downloadRegistry{jobs: map[string]*downloadJob{}}

// DownloadModelsDir exposes the models root for API responses (the webui
// shows users where weights live).
func DownloadModelsDir() string { return DefaultModelsDir }

// StartDownload begins downloading a catalog model by ID (catalog Name or
// directory basename — ResolveModelID's contract) and returns its initial
// status. Errors when the model is unknown, already installed, or already
// downloading. RAM-tier policy is NOT enforced here: an explicit user
// selection is a warned-but-allowed choice (UIs surface the tier),
// matching the CLI's SPROUT_ALLOW_OVERWEIGHT behavior for
// explicitly-selected models.
func StartDownload(modelID string) (DownloadStatusPayload, error) {
	var zero DownloadStatusPayload
	status, err := ResolveModelID(modelID)
	if err != nil {
		return zero, fmt.Errorf("unknown model %q — see the catalog list for valid IDs", modelID)
	}
	if status.Installed {
		return zero, fmt.Errorf("%s is already installed", status.Name)
	}

	ctx, cancel := context.WithCancel(context.Background())
	job := &downloadJob{
		status:  "downloading",
		started: time.Now(),
		cancel:  cancel,
	}

	downloads.mu.Lock()
	if existing := downloads.jobs[status.Name]; existing != nil && existing.running() {
		downloads.mu.Unlock()
		cancel()
		return zero, fmt.Errorf("%s is already downloading", status.Name)
	}
	downloads.jobs[status.Name] = job
	downloads.mu.Unlock()

	go func() {
		_, dlErr := EnsureModel(ctx, *status, func(downloaded, total int64) {
			job.mu.Lock()
			job.bytes = downloaded
			if total > 0 {
				job.total = total
			}
			job.mu.Unlock()
		})
		job.mu.Lock()
		job.ended = time.Now()
		switch {
		case dlErr == nil:
			job.status = "completed"
		case ctx.Err() != nil:
			job.status = "canceled"
		default:
			job.status = "failed"
			job.errStr = dlErr.Error()
		}
		job.mu.Unlock()
	}()

	return job.snapshot(), nil
}

// CancelDownload cancels a running download for the given model.
func CancelDownload(modelID string) error {
	name := modelID
	if status, err := ResolveModelID(modelID); err == nil {
		name = status.Name
	}
	downloads.mu.Lock()
	job := downloads.jobs[name]
	downloads.mu.Unlock()
	if job == nil || !job.running() {
		return fmt.Errorf("no active download for %s", name)
	}
	job.cancel()
	return nil
}

// DownloadStatus returns the current status for a model, or nil when no
// job exists for it.
func DownloadStatus(modelID string) *DownloadStatusPayload {
	name := modelID
	if status, err := ResolveModelID(modelID); err == nil {
		name = status.Name
	}
	downloads.mu.Lock()
	job := downloads.jobs[name]
	downloads.mu.Unlock()
	if job == nil {
		return nil
	}
	snap := job.snapshot()
	return &snap
}

// ActiveDownloads snapshots all running jobs.
func ActiveDownloads() []DownloadStatusPayload {
	downloads.mu.Lock()
	jobs := make([]*downloadJob, 0, len(downloads.jobs))
	for _, job := range downloads.jobs {
		if job.running() {
			jobs = append(jobs, job)
		}
	}
	downloads.mu.Unlock()

	out := make([]DownloadStatusPayload, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, job.snapshot())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModelID < out[j].ModelID })
	return out
}
