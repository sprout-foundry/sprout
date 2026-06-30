//go:build !darwin && !linux

package computer_use

func init() {
	// getForegroundAppImpl already defaults to returning
	// ErrForegroundUnavailable, so no override is needed.
}
