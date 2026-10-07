// errclass_annotation.go — the bridge from the build/runtime error
// classifier (pkg/errclass) to the turn-end repair reports. A failing
// build/test (or format/lint) check carries a bounded output excerpt; this
// helper classifies that excerpt and renders a short annotation the report
// attaches to the check's bullet. The annotation states what kind of
// failure it is and what to look at; the raw excerpt stays in the report
// unchanged, so the explanation accompanies the raw output rather than
// replacing it. An unknown classification renders nothing — the existing
// report shape is left untouched.

package agent

import "github.com/sprout-foundry/sprout/pkg/errclass"

// errclassAnnotation returns the short classification line for one failed
// check's output, or "" when the output cannot be classified (an unknown
// failure falls through to the existing report behavior). The line names
// the category and its explanation template; it never embeds the raw
// output, which the report keeps as its own excerpt block.
func errclassAnnotation(output string) string {
	if output == "" {
		return ""
	}
	res := errclass.Classify(output)
	if !res.Category.Known() {
		return ""
	}
	return string(res.Category) + ": " + res.Explanation
}
