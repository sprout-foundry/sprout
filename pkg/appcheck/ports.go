//go:build !js && !windows

package appcheck

import (
	"fmt"
	"net"
)

// freePort returns a currently free TCP port on the loopback interface.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find a free port: %w", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		return 0, fmt.Errorf("release the probe port: %w", err)
	}
	return port, nil
}
