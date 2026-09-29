package configuration

import (
	"log"
	"strings"
)

// logNoEnvKeys notes that no provider key came from the environment. It is
// only emitted with SPROUT_DEBUG set: keys usually live in the credential
// store, so the message fired on every launch and, in the interactive CLI,
// printed into the startup screen.
func logNoEnvKeys() {
	switch strings.ToLower(strings.TrimSpace(GetEnvSimple("DEBUG"))) {
	case "", "0", "false", "no", "off":
		return
	}
	log.Printf("[debug] no API keys found in environment variables")
}
