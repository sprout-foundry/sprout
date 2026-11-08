//go:build !js

package appcheck

// probe_scripts.go (continued): the shared vocabulary the in-page scripts
// are built from, and how a script is bound to a form.

import (
	"fmt"
	"strings"
)

// probeTextPattern matches the values the probe types, so the check can tell
// whether they were kept after a fresh load.
var probeTextPattern = `Probe (` + strings.Join(probeWords, "|") + `)`

// Never pressed: anything that destroys, leaves or dismisses.
const destructivePattern = `delete|remove|cancel|archive|clear|reset|log ?out|sign ?out|close|back|discard`

// Fields that collect several values, each added with Enter.
const multiValuePattern = `\b(tags?|labels?|keywords?|skills?|ingredients?|topics?|categories|chips?)\b`

// Multi-step forms advance with one of these.
const nextStepPattern = `^\s*(next|continue)\b`

// Action buttons outside forms the probe may press.
const actionPattern = `done|tick|check|complete|mark|toggle|today|\+|add|new|save|start|finish`

// formScript binds a script to the n-th form on the page (0-based) and fills
// its remaining placeholders with the given strings. The form is looked up
// inside the script rather than passed in by handle, so nothing is held
// across a navigation.
func formScript(template string, formIndex int, args ...string) string {
	values := make([]any, 0, len(args)+1)
	values = append(values, formIndex)
	for _, a := range args {
		values = append(values, a)
	}
	return fmt.Sprintf(template, values...)
}

// Note: template consts use fmt placeholders (%s, %q, %d), so they are bound
// with formScript (below) or fmt.Sprintf in the driver.
