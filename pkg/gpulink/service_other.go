//go:build !darwin && !linux

package gpulink

import "errors"

var errNoService = errors.New("installing the GPU link as a login service is supported on macOS and Linux; run `sprout gpu start` instead")

// InstallService is unavailable on this OS.
func InstallService(name string) (string, error) { return "", errNoService }

// UninstallService is unavailable on this OS.
func UninstallService(slot string) error { return errNoService }

// LegacyServiceInstalled reports whether an older sprout's single service is
// still on disk; this OS has none.
func LegacyServiceInstalled() bool { return false }

// ServiceNotice is what to know about the service on this machine.
func ServiceNotice() string { return "" }

// ServiceState is unavailable on this OS.
func ServiceState(slot string) (state, logs string) { return "not supported here", "" }
