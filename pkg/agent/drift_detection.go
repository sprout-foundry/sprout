package agent

import (
	"sync"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// DefaultDriftThreshold is the default cosine similarity threshold below which
// a conversation is considered to have drifted from its original intent.
const DefaultDriftThreshold = 0.60

// DefaultDriftCheckInterval is the default number of turns between drift checks.
const DefaultDriftCheckInterval = 5

// MaxDriftRejections is the number of rejections after which drift detection
// is suppressed for the remainder of the session.
const MaxDriftRejections = 3

// DriftDetector tracks conversational drift by comparing the current turn's
// embedding against the session's original intent embedding.
type DriftDetector struct {
	mu             sync.Mutex
	threshold      float64 // cosine similarity threshold (default: 0.60)
	checkInterval  int     // check every N turns (default: 5)
	driftCount     int     // number of drift detections in this session
	rejectionCount int     // number of times user rejected drift suggestion
	suppressed     bool    // true after MaxDriftRejections rejections
}

// NewDriftDetector creates a new DriftDetector with the given threshold and
// check interval. Zero values are replaced with sensible defaults.
func NewDriftDetector(threshold float64, checkInterval int) *DriftDetector {
	if threshold <= 0 {
		threshold = DefaultDriftThreshold
	}
	if checkInterval <= 0 {
		checkInterval = DefaultDriftCheckInterval
	}
	return &DriftDetector{
		threshold:     threshold,
		checkInterval: checkInterval,
	}
}

// ShouldCheck returns true if drift should be checked on the given turn number.
// Checks occur every checkInterval turns (turn 5, 10, 15, ...).
// Returns false if the detector is suppressed.
func (d *DriftDetector) ShouldCheck(turnNumber int) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.suppressed {
		return false
	}
	return turnNumber > 0 && turnNumber%d.checkInterval == 0
}

// CheckDrift computes the cosine similarity between the session's original
// intent embedding and the current turn's embedding. Returns true if the
// similarity is below the threshold, indicating drift.
//
// If sessionIntent is nil or empty, returns false, 0 as a graceful no-op.
func (d *DriftDetector) CheckDrift(sessionIntent []float32, currentEmbedding []float32) (isDrift bool, similarity float64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Graceful no-op if no session intent is available
	if sessionIntent == nil || len(sessionIntent) == 0 {
		return false, 0
	}

	similarity = float64(embedding.CosineSimilarity(sessionIntent, currentEmbedding))
	return similarity < d.threshold, similarity
}

// RecordDrift increments the drift detection counter for this session.
func (d *DriftDetector) RecordDrift() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.driftCount++
}

// RecordRejection increments the rejection counter and suppresses drift
// detection if the user has rejected MaxDriftRejections times.
func (d *DriftDetector) RecordRejection() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rejectionCount++
	if d.rejectionCount >= MaxDriftRejections {
		d.suppressed = true
	}
}

// IsSuppressed returns true if drift detection has been suppressed for this
// session due to too many rejections.
func (d *DriftDetector) IsSuppressed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.suppressed
}

// DriftCount returns the number of drift detections in this session.
func (d *DriftDetector) DriftCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.driftCount
}

// RejectionCount returns the number of drift rejections in this session.
func (d *DriftDetector) RejectionCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.rejectionCount
}
