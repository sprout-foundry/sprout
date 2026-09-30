//go:build !js

package cmd

import (
	"github.com/spf13/pflag"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// aliasMode says how an alternate flag spelling behaves.
type aliasMode int

const (
	// aliasDeprecated keeps the old spelling working, hides it from help,
	// and prints a one-line warning pointing at the canonical flag.
	aliasDeprecated aliasMode = iota
	// aliasSilent keeps the old spelling working and hidden with no notice.
	// Reserved for spellings external automation depends on (the foundry
	// task runner invokes `agent --skip-prompt --output-json --output-path`),
	// where a stderr notice would land in captured logs.
	aliasSilent
)

// deprecationAnnotation marks a hidden alias whose use prints a notice.
const deprecationAnnotation = "sprout_deprecated_alias_of"

// deprecatedValue wraps an alias flag's value so using the alias prints one
// warning in the CLI's glyph style. Warning from Set (rather than pflag's
// MarkDeprecated, which prints an unstyled line) works regardless of which
// PersistentPreRun hooks a subcommand installs.
type deprecatedValue struct {
	pflag.Value
	alias, canonical string
}

func (d *deprecatedValue) Set(v string) error {
	console.GlyphWarning.Printf("--%s is deprecated; use --%s", d.alias, d.canonical)
	return d.Value.Set(v)
}

func (d *deprecatedValue) IsBoolFlag() bool {
	b, ok := d.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

func markAlias(fs *pflag.FlagSet, alias, canonical string, mode aliasMode) {
	_ = fs.MarkHidden(alias)
	if mode != aliasDeprecated {
		return
	}
	f := fs.Lookup(alias)
	f.Value = &deprecatedValue{Value: f.Value, alias: alias, canonical: canonical}
	_ = fs.SetAnnotation(alias, deprecationAnnotation, []string{canonical})
}

// The alias must be registered after the canonical flag: pflag writes a
// flag's default into the bound variable at definition, so the alias reuses
// the variable's current value rather than resetting it.
func boolFlagAlias(fs *pflag.FlagSet, p *bool, alias, canonical string, mode aliasMode) {
	boolFlagAliasP(fs, p, alias, "", canonical, mode)
}

func boolFlagAliasP(fs *pflag.FlagSet, p *bool, alias, shorthand, canonical string, mode aliasMode) {
	fs.BoolVarP(p, alias, shorthand, *p, "alias for --"+canonical)
	markAlias(fs, alias, canonical, mode)
}

func stringFlagAlias(fs *pflag.FlagSet, p *string, alias, canonical string, mode aliasMode) {
	fs.StringVar(p, alias, *p, "alias for --"+canonical)
	markAlias(fs, alias, canonical, mode)
}
