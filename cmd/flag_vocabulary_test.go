//go:build !js

package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// nonCanonicalFlagNames maps retired spellings to the canonical flag. They
// may exist only as hidden aliases; a visible one means a command drifted
// back to its own vocabulary.
var nonCanonicalFlagNames = map[string]string{
	"force":       "yes",
	"skip-prompt": "yes",
	"output-json": "json",
	"output-path": "output",
	"out":         "output",
	"cwd":         "dir",
	"workspace":   "dir",
}

func walkCommands(c *cobra.Command, visit func(*cobra.Command)) {
	visit(c)
	for _, sub := range c.Commands() {
		walkCommands(sub, visit)
	}
}

func TestFlagVocabularyIsCanonical(t *testing.T) {
	walkCommands(rootCmd, func(c *cobra.Command) {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			canonical, retired := nonCanonicalFlagNames[f.Name]
			if retired && !f.Hidden {
				t.Errorf("%s: --%s is visible; use --%s (keep --%s only as a hidden alias)", c.CommandPath(), f.Name, canonical, f.Name)
			}
		})
	})
}

func TestFlagAliasesBindTheCanonicalVariable(t *testing.T) {
	cases := []struct {
		cmd       *cobra.Command
		alias     string
		value     string
		canonical string
	}{
		{agentCmd, "skip-prompt", "true", "yes"},
		{agentCmd, "output-json", "true", "json"},
		{agentCmd, "output-path", "/tmp/r.json", "output"},
		{commitCmd, "skip-prompt", "true", "yes"},
		{prCmd, "skip-prompt", "true", "yes"},
		{reviewStagedCmd, "skip-prompt", "true", "yes"},
		{auditClearCmd, "force", "true", "yes"},
		{historyClearCmd, "force", "true", "yes"},
		{historyClearCmd, "workspace", "/tmp/ws", "dir"},
		{searchCmd, "cwd", "/tmp/ws", "dir"},
		{txnPullCmd, "out", "/tmp/m.json", "output"},
	}
	for _, tc := range cases {
		t.Run(tc.cmd.Name()+"/"+tc.alias, func(t *testing.T) {
			fs := tc.cmd.Flags()
			canonical := fs.Lookup(tc.canonical)
			if canonical == nil {
				t.Fatalf("--%s not registered", tc.canonical)
			}
			orig := canonical.Value.String()
			t.Cleanup(func() { _ = canonical.Value.Set(orig) })
			if err := fs.Set(tc.alias, tc.value); err != nil {
				t.Fatalf("setting --%s: %v", tc.alias, err)
			}
			if got := canonical.Value.String(); got != tc.value {
				t.Errorf("--%s=%s left --%s at %q", tc.alias, tc.value, tc.canonical, got)
			}
		})
	}
}

// The foundry task runner passes these spellings; a deprecation notice would
// land in its captured stderr.
func TestContractAliasesStaySilent(t *testing.T) {
	for _, tc := range []struct {
		cmd  *cobra.Command
		flag string
	}{
		{agentCmd, "skip-prompt"},
		{agentCmd, "output-json"},
		{agentCmd, "output-path"},
		{commitCmd, "skip-prompt"},
	} {
		f := tc.cmd.Flags().Lookup(tc.flag)
		if f == nil {
			t.Fatalf("%s --%s not registered", tc.cmd.Name(), tc.flag)
		}
		if _, warns := f.Annotations[deprecationAnnotation]; warns || f.Deprecated != "" {
			t.Errorf("%s --%s must not print a deprecation notice", tc.cmd.Name(), tc.flag)
		}
		if !f.Hidden {
			t.Errorf("%s --%s should be hidden from help", tc.cmd.Name(), tc.flag)
		}
	}
}

func TestTxnPullOutputKeepsStdoutDefault(t *testing.T) {
	if got := txnPullCmd.Flags().Lookup("output").DefValue; got != "-" {
		t.Errorf("txn-pull --output default = %q, want \"-\"", got)
	}
	if txnPullOut != "-" {
		t.Errorf("txnPullOut = %q after init, want \"-\"", txnPullOut)
	}
}

func TestDeprecatedAliasesWarn(t *testing.T) {
	for _, tc := range []struct {
		cmd  *cobra.Command
		flag string
	}{
		{prCmd, "skip-prompt"},
		{reviewStagedCmd, "skip-prompt"},
		{auditClearCmd, "force"},
		{historyClearCmd, "force"},
		{historyClearCmd, "workspace"},
		{searchCmd, "cwd"},
	} {
		f := tc.cmd.Flags().Lookup(tc.flag)
		if f == nil {
			t.Fatalf("%s --%s not registered", tc.cmd.Name(), tc.flag)
		}
		if _, ok := f.Value.(*deprecatedValue); !ok {
			t.Errorf("%s --%s should warn when used", tc.cmd.Name(), tc.flag)
		}
	}
	if f := auditClearCmd.Flags().ShorthandLookup("f"); f == nil || f.Name != "force" {
		t.Error("audit clear -f should still resolve to the --force alias")
	}
}
