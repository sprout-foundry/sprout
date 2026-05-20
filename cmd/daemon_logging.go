// daemon_logging.go — Daemon log rotation via lumberjack.
//
// When sprout runs as a daemon (SPROUT_SERVICE=1), this module redirects
// os.Stdout and os.Stderr to lumberjack.Logger instances so that log files
// are automatically rotated.  This replaces the approach of letting launchd
// / systemd / nohup write to fixed files and provides uniform rotation on
// every platform.
package cmd

import (
	"io"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	daemonLogMaxSize    = 10 // MB
	daemonLogMaxBackups = 5
	daemonLogCompress   = true
)

// setupDaemonLogging redirects os.Stdout and os.Stderr to lumberjack.Logger
// writers when the process is running as a managed daemon
// (SPROUT_SERVICE=1).  Does nothing in non-daemon mode.
//
// It creates a pair of os.Pipe per stream: the write end replaces
// os.Stdout/os.Stderr (it is an *os.File), while a background goroutine
// copies from the read end into the rotating lumberjack.Logger.
func setupDaemonLogging(homeDir string) {
	if os.Getenv("SPROUT_SERVICE") != "1" {
		return
	}

	logDir := filepath.Join(homeDir, ".sprout", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		// Can't log anything meaningful here; just skip rotation.
		return
	}

	redirectToLumberjack := func(filename string) *os.File {
		writer := &lumberjack.Logger{
			Filename:   filepath.Join(logDir, filename),
			MaxSize:    daemonLogMaxSize,
			MaxBackups: daemonLogMaxBackups,
			Compress:   daemonLogCompress,
		}

		r, w, err := os.Pipe()
		if err != nil {
			return nil
		}

		go func() {
			io.Copy(writer, r)
			r.Close()
		}()

		return w
	}

	if w := redirectToLumberjack("daemon.stdout.log"); w != nil {
		os.Stdout = w
	}
	if w := redirectToLumberjack("daemon.stderr.log"); w != nil {
		os.Stderr = w
	}
}
