//go:build !linux && !darwin

package embedding

func init() {
	// No portable free-memory reader on this platform; the check is disabled.
	memAvailableFn = func() (uint64, bool) { return 0, false }
}
