package console

// CLI-D-3: register the Alt+T keybinding in the global keymap so the
// binding is discoverable via /help (KeymapHelpTable) and the dispatch
// path works without per-call site wiring.
//
// The handler reads the footer size via the global StatusFooter when
// available, falls back to a sane default, and toggles the tooltip.
//
// CLI-UX-12: register Alt+V → output_verbosity.toggle so power users
// can switch between default and verbose output with a single shortcut.
//
// Initialization is idempotent — multiple RegisterKeymapForFooter calls
// during startup are safe.

import (
	"fmt"
	"os"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

var (
	keymapOnce     sync.Once
	keymapDisabled bool
)

// RegisterKeymapForFooter wires Alt+T → footer-tooltip toggle and
// Alt+V → output-verbosity toggle into the global keymap. Call from
// your REPL bootstrap (or wherever the agent shell starts).
//
// The cfg parameter is optional (nil falls through to a no-op handler
// for the verbosity toggle). Idempotent — calling twice doesn't double-
// register because the keymap replaces by Action name.
func RegisterKeymapForFooter(footer *StatusFooter, cfg *configuration.Manager) {
	keymapOnce.Do(func() {
		// SP-115 Phase 4: set the initial hint visibility based on verbosity.
		// Compact verbosity hides the hint; default and verbose show it.
		if footer != nil && cfg != nil {
			current := cfg.GetConfig()
			if current != nil {
				footer.SetShowKeymapHint(current.OutputVerbosity != "compact")
			}
		}

		// Alt+T: footer tooltip toggle (CLI-D-3)
		GlobalKeymap().Register(KeymapEntry{
			Key:         "Alt+T",
			Action:      "footer.tooltip.toggle",
			Description: "Show / hide per-tool invocation breakdown above the status footer",
			Handler: func() {
				cols, rows := footerTooltipSize(footer)
				t := getOrInitTooltip()
				t.Toggle(cols, rows)
			},
		})

		// Alt+V: output verbosity toggle (CLI-UX-12)
		GlobalKeymap().Register(KeymapEntry{
			Key:         "Alt+V",
			Action:      "output_verbosity.toggle",
			Description: "Cycle output verbosity: default ↔ verbose (more tool detail)",
			Handler: func() {
				if cfg == nil {
					return
				}
				current := cfg.GetConfig()
				if current == nil {
					return
				}
				newValue := computeVerbosityToggle(current.OutputVerbosity)
				if err := cfg.UpdateConfigNoSave(func(c *configuration.Config) error {
					c.OutputVerbosity = newValue
					return nil
				}); err != nil {
					return
				}
				// SP-115: update hint visibility when verbosity changes.
				// Compact hides the hint; default and verbose show it.
				if footer != nil {
					footer.SetShowKeymapHint(newValue != "compact")
				}
				label := verbosityToggleLabel(newValue)
				fmt.Fprintln(os.Stderr, GlyphInfo.Prefix()+label)
			},
		})
	})
}

// computeVerbosityToggle returns the next verbosity value for an Alt+V
// press. The cycle is narrow: verbose ↔ default. Compact jumps to
// verbose (not default) so power users always get more detail.
func computeVerbosityToggle(current string) string {
	switch current {
	case configuration.OutputVerbosityVerbose:
		return configuration.OutputVerbosityDefault
	default:
		// "default", "", "compact", or anything else → verbose
		return configuration.OutputVerbosityVerbose
	}
}

// verbosityToggleLabel returns the one-line confirmation message for
// the given verbosity mode, matching the existing badge style.
func verbosityToggleLabel(verbosity string) string {
	switch verbosity {
	case configuration.OutputVerbosityVerbose:
		return "output verbosity: verbose (wider tool-arg previews · Alt+V to toggle)"
	default:
		return "output verbosity: default (Alt+V to toggle)"
	}
}

var (
	sharedTooltip     *FooterTooltip
	sharedTooltipOnce sync.Once
)

func getOrInitTooltip() *FooterTooltip {
	sharedTooltipOnce.Do(func() {
		sharedTooltip = NewFooterTooltip(os.Stderr)
	})
	return sharedTooltip
}

// footerTooltipSize resolves the size for the tooltip placement.
// Prefers the registered status footer's terminal size; falls back to
// /dev/tty probing; finally 80x24.
func footerTooltipSize(footer *StatusFooter) (cols, rows int) {
	if footer != nil {
		c, r := footer.TerminalSize()
		if c > 0 && r > 0 {
			return c, r
		}
	}
	if f, err := openDevTtyForSize(); err == nil {
		c, r := readTermSize(f)
		_ = f.Close()
		if c > 0 && r > 0 {
			return c, r
		}
	}
	return 80, 24
}
