//go:build !darwin && !linux

package gpulink

import "errors"

var errNoService = errors.New("installing the GPU link as a login service is supported on macOS and Linux; run `sprout gpu start` instead")

// InstallService is unavailable on this OS.
func InstallService() (string, error) { return "", errNoService }

// UninstallService is unavailable on this OS.
func UninstallService() error { return errNoService }

// ServiceNote is what to know about the login service on this OS.
const ServiceNote = ""
