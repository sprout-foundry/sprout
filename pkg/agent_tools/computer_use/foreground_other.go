//go:build !darwin && !linux && !android

package computer_use

func init() {
	// getForegroundAppImpl already defaults to returning
	// ErrForegroundUnavailable, so no override is needed.
}
