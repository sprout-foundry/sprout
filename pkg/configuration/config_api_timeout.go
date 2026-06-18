package configuration

// APITimeoutConfig represents timeout settings for API calls
type APITimeoutConfig struct {
	ConnectionTimeoutSec    int `json:"connection_timeout_sec,omitempty"`     // Time to establish connection (default: 300)
	FirstChunkTimeoutSec    int `json:"first_chunk_timeout_sec,omitempty"`    // Time to receive first response (default: 600)
	ChunkTimeoutSec         int `json:"chunk_timeout_sec,omitempty"`          // Max time between streaming chunks (default: 600)
	OverallTimeoutSec       int `json:"overall_timeout_sec,omitempty"`        // Total request timeout (default: 1800)
	CommitMessageTimeoutSec int `json:"commit_message_timeout_sec,omitempty"` // Timeout for commit message generation (default: 300)
}
