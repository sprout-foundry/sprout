//go:build !js

package cmd

import "testing"

// A new top-level command must be placed in a help group (or declared
// plumbing), or root help grows an "Additional Commands" junk drawer again.
func TestEveryVisibleTopLevelCommandHasAGroup(t *testing.T) {
	applyCommandGroups(rootCmd)
	for _, c := range rootCmd.Commands() {
		if c.Hidden || c.Deprecated != "" {
			continue
		}
		if c.GroupID == "" {
			t.Errorf("top-level command %q has no help group — add it to commandGroups (or plumbingCommands)", c.Name())
		}
	}
	for _, name := range plumbingCommands {
		c, _, err := rootCmd.Find([]string{name})
		if err != nil || c.Name() != name {
			t.Errorf("plumbing command %q is not registered", name)
			continue
		}
		if !c.Hidden {
			t.Errorf("plumbing command %q should be hidden from help", name)
		}
	}
}
