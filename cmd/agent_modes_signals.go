//go:build !js

package cmd

// startSignalHandler installs the SIGINT/SIGTERM/SIGHUP handler
// goroutine — SIGHUP in daemon mode reloads on-disk config, a second
// interrupt within 2s of an in-progress query force-quits, and a graceful
// interrupt cancels the context, closes the global browser, and starts the
// 5s force-quit timer — and returns the shutdown channel that is closed
// once a graceful shutdown has started. Split out of RunAgent
// (agent_modes.go).
import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/webcontent"
)

func startSignalHandler(ctx context.Context, cancel context.CancelFunc, chatAgent *agent.Agent, isInteractive bool) chan struct{} {
	// Setup signal handling with buffered channel for multiple signals
	// Note: We intentionally do NOT capture SIGTSTP (Ctrl+Z) to allow process suspension
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// Handle shutdown gracefully
	shutdown := make(chan struct{})
	go func() {
		var lastInterruptAt int64
		for {
			select {
			case sig := <-sigCh:
				// SIGHUP in daemon mode = reload on-disk config (Unix
				// daemon convention). In interactive mode SIGHUP means
				// the controlling terminal closed — the kernel sends it
				// to the foreground process group when the tty hangs
				// up. Treating that as "reload" leaves an orphaned
				// sprout running with PPID=1 forever (it keeps
				// heartbeating to instances.json against new sessions).
				// Fall through to the shutdown path so terminal close
				// cleans up the process.
				if sig == syscall.SIGHUP && daemonMode {
					fmt.Println()
					console.GlyphAction.Printf("Received SIGHUP, reloading configuration...")
					if chatAgent != nil {
						if mgr := chatAgent.GetConfigManager(); mgr != nil {
							if err := mgr.Reload(); err != nil {
								console.GlyphError.Fprintf(os.Stdout, "Reload failed: %v", err)
							} else {
								console.GlyphSuccess.Print("Configuration reloaded successfully.")
							}
						}
					}
					continue
				}

				if isInteractive && (isQueryInProgress() || (chatAgent != nil && chatAgent.IsQueryInProgress())) {
					nowUnix := time.Now().UnixNano()
					prev := atomic.LoadInt64(&lastInterruptAt)
					if prev > 0 && time.Duration(nowUnix-prev) < 2*time.Second {
						console.StopGlobalStatusFooter()
						fmt.Println()
						console.GlyphStopped.Printf("Force quitting immediately...")
						if chatAgent != nil {
							chatAgent.ForceSaveAndExit(1)
						}
						os.Exit(1)
					}

					atomic.StoreInt64(&lastInterruptAt, nowUnix)
					fmt.Println()
					console.GlyphPaused.Printf("Received signal %v, interrupting active task...", sig)
					console.GlyphDim.Printf("  (Press Ctrl+C again quickly to force quit)")
					if chatAgent != nil {
						chatAgent.TriggerInterrupt()
					}
					// SP-056-6d: Resolve any active reasoning fold on interrupt.
					if fold := currentReasoningFold; fold != nil && fold.IsActive() {
						fold.Interrupt()
					}
					continue
				}

				fmt.Println()
				console.GlyphStopped.Printf("Received signal %v, shutting down gracefully...", sig)
				console.GlyphDim.Printf("  (Press Ctrl+C again to force quit)")

				// Cancel the context which will stop all operations
				cancel()

				// Close the global browser renderer to release Chromium resources
				webcontent.CloseGlobalBrowser()

				// Signal that shutdown has started
				close(shutdown)

				// Start a timeout goroutine for force quit
				go func() {
					time.Sleep(5 * time.Second)
					console.StopGlobalStatusFooter()
					fmt.Println()
					console.GlyphStopped.Printf("Force quitting...")
					if chatAgent != nil {
						chatAgent.ForceSaveAndExit(1)
					}
					os.Exit(1)
				}()

				// Any subsequent signal after shutdown starts should force quit.
				for {
					select {
					case <-sigCh:
						fmt.Println()
						console.GlyphStopped.Printf("Force quitting immediately...")
						if chatAgent != nil {
							chatAgent.ForceSaveAndExit(1)
						}
						os.Exit(1)
					case <-ctx.Done():
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return shutdown
}
