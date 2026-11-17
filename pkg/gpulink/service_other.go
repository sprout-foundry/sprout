//go:build !darwin && !linux

package gpulink

import "errors"

var errNoService = errors.New("installing the GPU link as a login service is supported on macOS and Linux; run `sprout gpu start` instead")

// InstallService is unavailable on this OS.
func InstallService() (string, error) { return "", errNoService }

// UninstallService is unavailable on this OS.
func UninstallService() error { return errNoService }

// ServiceNotice is what to know about the service on this machine.
func ServiceNotice() string { return "" }

// ServiceState is unavailable on this OS.
func ServiceState() (state, logs string) { return "not supported here", "" }
