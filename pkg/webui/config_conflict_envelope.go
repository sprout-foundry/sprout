//go:build !js

package webui

import (
	"errors"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// configConflictErrorCode is the wire-format error code surfaced to the
// frontend when a save fails because the config file changed on disk
// since this Config was loaded. The frontend matches on `code` (not the
// message string) to show its "Settings changed on disk" toast — see
// SP-034-4d. Keep this stable: changing it is a wire break.
const configConflictErrorCode = "config_conflict"

// configConflictEnvelope builds the WS error message body for a
// ConfigConflictError. Includes `current_summary` with the canonical
// fields the frontend needs to decide what to show in the reload toast.
//
// Returns (envelope, true) when the error is a ConfigConflictError and
// the caller should send the envelope. Returns (nil, false) for any
// other error; the caller should fall through to its existing error
// path.
func configConflictEnvelope(err error, cm *configuration.Manager) (map[string]interface{}, bool) {
	if err == nil {
		return nil, false
	}
	var ccErr *configuration.ConfigConflictError
	if !errors.As(err, &ccErr) {
		return nil, false
	}

	summary := map[string]interface{}{}
	if cm != nil {
		// Re-read the on-disk config so the frontend can show what the
		// user will get if they accept the reload. Failures are
		// non-fatal — we still want to surface the conflict; the
		// summary is just a UX nicety.
		if reloaded, loadErr := configuration.Load(); loadErr == nil && reloaded != nil {
			summary["provider"] = reloaded.LastUsedProvider
			if reloaded.ProviderModels != nil {
				summary["model"] = reloaded.ProviderModels[reloaded.LastUsedProvider]
			}
		}
	}

	return map[string]interface{}{
		"type": "error",
		"data": map[string]interface{}{
			"code":            configConflictErrorCode,
			"message":         ccErr.Error(),
			"current_summary": summary,
			"path":            ccErr.Path,
		},
	}, true
}
