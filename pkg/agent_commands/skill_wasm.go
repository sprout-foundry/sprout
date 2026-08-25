//go:build js

package commands

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// SkillCommand is unavailable in WASM builds.
//
// The real implementation (skill.go) is //go:build !js because skill
// install/update/remove operate on the local filesystem and shell out — neither
// exists in the browser. But commands.go registers &SkillCommand{}
// unconditionally, so without this stub `GOOS=js go build ./...` fails with
// "undefined: SkillCommand".
//
// That break was invisible to CI: the WASM smoke test builds the tool roster,
// not this package, so nothing exercised a full js build of ./...
//
// Registering a stub rather than skipping registration keeps /skill listed and
// gives a clear reason when invoked, matching how the other WASM-excluded
// surfaces behave (see all_browse_url_wasm.go, all_codegraph_wasm.go).
type SkillCommand struct{}

func (c *SkillCommand) Name() string { return "skill" }

func (c *SkillCommand) SafeDuringSteer() bool { return true }

func (c *SkillCommand) Description() string {
	return "Install, update, remove, list, enable, or disable skills (unavailable in the browser)"
}

func (c *SkillCommand) Usage() string {
	return "Skill management is not available in WASM builds — it requires local filesystem access."
}

func (c *SkillCommand) Execute(args []string, chatAgent *agent.Agent) error {
	return errSkillUnsupportedWASM
}

func (c *SkillCommand) ExecuteWithJSONOutput(args []string, chatAgent *agent.Agent, ctx *CommandContext) error {
	return errSkillUnsupportedWASM
}

func (c *SkillCommand) Complete(args []string, chatAgent *agent.Agent) []string { return nil }

var errSkillUnsupportedWASM = fmt.Errorf(
	"skill management is not available in WASM builds: it requires local filesystem access")
