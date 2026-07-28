// Persistence operations for change tracking
package history

// RedactedContentMarker is the canonical marker used when file content is
// redacted because the file is outside the workspace root (to avoid leaking
// sensitive data). It is defined here, in the lower-level history package, so
// both pkg/history and pkg/agent reference a single source of truth instead of
// maintaining duplicate copies that can silently drift. pkg/agent references
// this via history.RedactedContentMarker.
const RedactedContentMarker = "[REDACTED - external file]"
