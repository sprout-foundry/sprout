package configuration

import (
	"fmt"
	"time"
)

// ConfigConflictError is returned from Config.Save when the on-disk
// config file has been modified since the in-memory Config was last
// loaded. Indicates another writer (another agent process, another
// webui tab, a hand-edit) changed the file out from under us.
//
// SP-034-4b. Callers should surface this to the user with a "Settings
// changed on disk" prompt and offer to reload before retrying.
type ConfigConflictError struct {
	// Path is the absolute path of the config file.
	Path string
	// LoadedModTime / LoadedSize is what the in-memory Config remembers
	// from when it was last loaded.
	LoadedModTime time.Time
	LoadedSize    int64
	// CurrentModTime / CurrentSize is what's on disk right now.
	CurrentModTime time.Time
	CurrentSize    int64
}

// Error satisfies the error interface. Format is stable and parsed by
// the webui's `config_conflict` error mapper — don't reorganize fields
// without coordinating with `pkg/webui/websocket_message_handlers.go`.
func (e *ConfigConflictError) Error() string {
	return fmt.Sprintf(
		"config file changed on disk since load: %s (loaded mtime=%s size=%d; current mtime=%s size=%d)",
		e.Path,
		e.LoadedModTime.UTC().Format(time.RFC3339Nano),
		e.LoadedSize,
		e.CurrentModTime.UTC().Format(time.RFC3339Nano),
		e.CurrentSize,
	)
}

// IsConfigConflict is a convenience predicate so callers don't have to
// type-assert by hand. Returns false for nil errors.
func IsConfigConflict(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(*ConfigConflictError)
	return ok
}
