//go:build !darwin && !linux

package runner

import "errors"

var errNoService = errors.New("installing the runner as a login service is supported on macOS and Linux; run `sprout runner start` instead")

// InstallService is unavailable on this OS.
func InstallService() (string, error) { return "", errNoService }

// UninstallService is unavailable on this OS.
func UninstallService() error { return errNoService }
