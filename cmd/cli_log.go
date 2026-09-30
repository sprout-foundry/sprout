//go:build !js

package cmd

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	"gopkg.in/natefinch/lumberjack.v2"
)

var cliLogOnce sync.Once

// routeGoLogForTerminal sends package-level log.Printf output to
// <state>/logs/cli.log when a human is watching stderr. Internals log
// operational noise (keyring fallbacks, watcher startup) that would otherwise
// print timestamped lines ahead of the command's real output. Piped and CI
// runs keep stderr logging — there it is the only diagnostic channel.
// SPROUT_DEBUG=1 opts back into stderr.
func routeGoLogForTerminal() {
	cliLogOnce.Do(func() {
		if !console.StderrIsTerminal() || debugEnvEnabled() {
			return
		}
		stateDir, err := envutil.StateDir()
		if err != nil {
			return
		}
		logDir := filepath.Join(stateDir, "logs")
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			return
		}
		log.SetOutput(&lumberjack.Logger{
			Filename:   filepath.Join(logDir, "cli.log"),
			MaxSize:    daemonLogMaxSize,
			MaxBackups: daemonLogMaxBackups,
			Compress:   daemonLogCompress,
		})
	})
}

func debugEnvEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SPROUT_DEBUG"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
