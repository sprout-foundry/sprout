package console

// CLI-D-3: register the Alt+T keybinding in the global keymap so the
// binding is discoverable via /help (KeymapHelpTable) and the dispatch
// path works without per-call site wiring.
//
// The handler reads the footer size via the global StatusFooter when
// available, falls back to a sane default, and toggles the tooltip.
// Initialization is idempotent — multiple RegisterKeymapForFooter calls
// during startup are safe.

import (
	"os"
	"sync"
)

var (
	keymapOnce     sync.Once
	keymapDisabled bool
)

// RegisterKeymapForFooter wires Alt+T → footer-tooltip toggle into the
// global keymap. Call from your REPL bootstrap (or wherever the agent
// shell starts). Idempotent — calling twice doesn't double-register
// because the keymap replaces by Action name.
func RegisterKeymapForFooter(footer *StatusFooter) {
	keymapOnce.Do(func() {
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
	})
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