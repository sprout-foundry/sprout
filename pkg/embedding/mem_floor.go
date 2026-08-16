package embedding

import "fmt"

// memAvailableFn reports currently available system memory in bytes and
// whether the platform could determine it. Platform files assign the real
// reader in init(); tests override it to simulate low-memory builds.
var memAvailableFn func() (uint64, bool)

// errMemFloor is returned when available memory is below the build floor.
var errMemFloor = fmt.Errorf("index: build halted: available memory below %d MiB floor", memFloorBytes>>20)

// checkMemFloor returns errMemFloor when available memory is below the build
// floor, or nil when memory is fine or the platform cannot report it.
func checkMemFloor() error {
	if memAvailableFn == nil {
		return nil
	}
	available, ok := memAvailableFn()
	if !ok {
		return nil
	}
	if available < memFloorBytes {
		return errMemFloor
	}
	return nil
}
