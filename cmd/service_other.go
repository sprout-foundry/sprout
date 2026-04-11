//go:build !linux && !darwin

package cmd

import "fmt"

// platformServiceManager is a stub for platforms without service management support.
type platformServiceManager struct{}

func (p *platformServiceManager) Install() error {
	return fmt.Errorf("service management is not supported on this platform")
}

func (p *platformServiceManager) Uninstall() error {
	return fmt.Errorf("service management is not supported on this platform")
}

func (p *platformServiceManager) Start() error {
	return fmt.Errorf("service management is not supported on this platform")
}

func (p *platformServiceManager) Stop() error {
	return fmt.Errorf("service management is not supported on this platform")
}

func (p *platformServiceManager) Status() (bool, error) {
	return false, fmt.Errorf("service management is not supported on this platform")
}

func init() {
	newServiceManager = func() serviceManager { return &platformServiceManager{} }
}
