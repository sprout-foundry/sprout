//go:build !js

package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/sprout-foundry/sprout/pkg/service"
)

// loadServiceEnvForDaemon loads service.env into the environment of a
// service-managed process (SPROUT_SERVICE=1) before configuration and
// providers are resolved. launchd cannot reference an env file, so on macOS
// this is how captured API keys reach the daemon; under systemd the unit's
// EnvironmentFile= has already set them and existing variables win. Only a
// count and failures are reported — never names or values.
func loadServiceEnvForDaemon(stderr io.Writer) {
	if os.Getenv("SPROUT_SERVICE") != "1" {
		return
	}
	applied, err := service.ApplyServiceEnvFile()
	if err != nil {
		fmt.Fprintf(stderr, "sprout service: could not load service.env: %v\n", err)
		return
	}
	if len(applied) > 0 {
		fmt.Fprintf(stderr, "sprout service: loaded %d variable(s) from service.env\n", len(applied))
	}
}
