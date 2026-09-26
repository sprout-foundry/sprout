package design

// inventory_rules_naming.go — the screen / component naming + inventory
// helpers used by the consistency validators: the flow-node / README screen
// stem extraction (flowNodeStems, readmeScreenEntries), the README component
// entries (readmeComponentEntries), and the screen-slug / name-mismatch
// detection (screenSlugViolations, screenStemRecord, screenNameMismatches,
// nameMismatchesForStems). Split out of inventory_rules.go.

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// flowNodeStems returns the set of node ids referenced across every
// design/flows/*.mmd file. A node counts whether it is an edge endpoint or a
// declared node in the flow, matching the §1c id == stem contract: a flow that
// taps into a screen keeps a reference to it either way.
func flowNodeStems(root string) map[string]bool {
	out := map[string]bool{}
	matches, err := filepath.Glob(filepath.Join(root, DirName, "flows", "*.mmd"))
	if err != nil {
		return out
	}
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			continue
		}
		fc := ParseFlowchart(string(data))
		for _, id := range fc.NodeOrder {
			out[id] = true
		}
		for _, e := range fc.Edges {
			out[e.Source] = true
			out[e.Target] = true
		}
	}
	return out
}

// readmeScreenEntries returns the set of names named by the README manifest's
// Screens listing bullets, reusing the §4b parser so the inventory and the
// bidirectionality pack agree on what "listed in the README" means. A missing
// README yields an empty set (every wireframe then pairs with a flow or is an
// orphan).
func readmeScreenEntries(root string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(root, DirName, ManifestName))
	if err != nil {
		return out
	}
	for _, ref := range readmeScreenRefs(string(data)) {
		if ref.section == "Screens" {
			out[ref.name] = true
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Component inventory — orphan components
// ---------------------------------------------------------------------------

// readmeComponentEntries returns the set of names named by the README
// manifest's Components listing bullets, reusing the §4b parser so the
// inventory and the bidirectionality pack agree on what "listed in the
// README" means. A missing README yields an empty set (every component is
// then an orphan).
func readmeComponentEntries(root string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(root, DirName, ManifestName))
	if err != nil {
		return out
	}
	for _, ref := range readmeScreenRefs(string(data)) {
		if ref.section == "Components" {
			out[ref.name] = true
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Naming — screen names across wireframes/ and screens/
// ---------------------------------------------------------------------------

// screenSlugViolations reads every design/screens/*.html file and reports the
// stems that break the shared slug rule, as a warn. The stem is the screen
// name; a non-slug stem cannot be a wireframe counterpart and cannot appear in
// a flow or listing, so it is a naming inconsistency rather than the hard
// per-file rule (which validateScreen keeps as ruleScreenSlugName for a
// single-file run). A missing screens directory yields no findings.
func screenSlugViolations(root string) []Finding {
	matches, err := filepath.Glob(filepath.Join(root, DirName, "screens", "*.html"))
	if err != nil {
		return []Finding{}
	}
	sort.Strings(matches)
	var findings []Finding
	for _, match := range matches {
		stem := strings.TrimSuffix(path.Base(filepath.ToSlash(match)), ".html")
		if frameNameRe.MatchString(stem) {
			continue
		}
		findings = append(findings, Finding{
			File:     relAsset(root, match),
			Rule:     ruleScreenSlugName,
			Severity: SeverityWarn,
			Message:  fmt.Sprintf("screen stem %q must match the slug rule %s", stem, SlugPattern),
		})
	}
	return findings
}

// screenStemRecord is one delivered screen file paired with its stem, the
// unit the naming comparison groups.
type screenStemRecord struct {
	stem string
	file string
}

// screenNameMismatches compares the design/screens/ inventory against the
// design/wireframes/ inventory by stem, SP-140-4 §4b "Naming". Two mismatches
// are reported, both `warn`:
//
//   - a delivered screen whose stem has no wireframe counterpart — the screen
//     inventories disagree about which screens exist
//     (ruleConsistencyScreenNameMismatch);
//   - two delivered screen files sharing a stem — an ambiguous screen name
//     (ruleConsistencyScreenNameDuplicate).
//
// A wireframe with no delivered screen is the normal pre-code state and is
// never a mismatch (the orphan rule covers an unreferenced wireframe). Missing
// directories yield no findings; the result is sorted.
func screenNameMismatches(root string) []Finding {
	matches, err := filepath.Glob(filepath.Join(root, DirName, "screens", "*.html"))
	if err != nil || len(matches) == 0 {
		return []Finding{}
	}
	sort.Strings(matches)

	records := make([]screenStemRecord, 0, len(matches))
	for _, match := range matches {
		records = append(records, screenStemRecord{
			stem: strings.TrimSuffix(path.Base(filepath.ToSlash(match)), ".html"),
			file: relAsset(root, match),
		})
	}

	wireframeStems := map[string]bool{}
	for _, s := range assetStems(root, "wireframes", ".svg") {
		wireframeStems[s] = true
	}

	return nameMismatchesForStems(records, wireframeStems)
}

// nameMismatchesForStems is the naming comparison core: given the delivered
// screen records and the wireframe stem set, it returns the mismatch and
// duplicate findings sorted by file. It is split out so the duplicate-stem
// branch — unreachable through a real glob, since a filesystem cannot hold two
// identical file names — is testable directly.
func nameMismatchesForStems(records []screenStemRecord, wireframeStems map[string]bool) []Finding {
	byStem := map[string][]string{}
	for _, r := range records {
		byStem[r.stem] = append(byStem[r.stem], r.file)
	}

	var findings []Finding
	stems := make([]string, 0, len(byStem))
	for stem := range byStem {
		stems = append(stems, stem)
	}
	sort.Strings(stems)
	for _, stem := range stems {
		files := byStem[stem]
		if len(files) > 1 {
			sort.Strings(files)
			for _, f := range files {
				findings = append(findings, Finding{
					File:     f,
					Rule:     ruleConsistencyScreenNameDuplicate,
					Severity: SeverityWarn,
					Message:  fmt.Sprintf("duplicate screen name %q: %d files in %s/ share the stem (%s); a screen name must be unique", stem, len(files), path.Join(DirName, "screens"), strings.Join(files, ", ")),
				})
			}
			continue
		}
		if wireframeStems[stem] {
			continue
		}
		findings = append(findings, Finding{
			File:     files[0],
			Rule:     ruleConsistencyScreenNameMismatch,
			Severity: SeverityWarn,
			Message:  fmt.Sprintf("screen %q has no wireframe counterpart (expected %s); the screens/ and wireframes/ inventories disagree", stem, path.Join(DirName, "wireframes", stem+".svg")),
		})
	}
	return findings
}
