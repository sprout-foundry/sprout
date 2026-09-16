package design

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Rule ids for the icon validator, SP-140-1 §1f.
const (
	// ruleIconWellformed fires when an icon SVG is not well-formed.
	ruleIconWellformed = "icon_wellformed"

	// ruleIconSelfContainment fires on <script> or external/local resource
	// references (icons follow the wireframe self-containment rules).
	ruleIconSelfContainment = "icon_self_containment"

	// ruleIconSlugName fires when an icon file stem breaks the slug rule.
	ruleIconSlugName = "icon_slug_name"

	// ruleIconSpriteSymbol fires when a sprite.svg <symbol> id is not a slug
	// (or the sprite has no symbols).
	ruleIconSpriteSymbol = "icon_sprite_symbol"
)

// iconSpriteName is the designated sprite sheet filename under design/icons/.
// It is a fixed name, exempt from the slug rule that applies to other icons.
const iconSpriteName = "sprite"

// ValidateIconsDir validates every design/icons/*.svg under root, SP-140-1
// §1f: self-containment (no <script>, no external/local refs), the slug name
// rule, and — for the optional sprite.svg — <symbol> entries whose ids follow
// the slug rule. A missing or empty icons directory yields no findings, not
// an error. Findings are sorted by file, line, rule, message.
func ValidateIconsDir(root string) ([]Finding, error) {
	pattern := filepath.Join(root, DirName, "icons", "*.svg")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing %s: %w", pattern, err)
	}
	findings := []Finding{}
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", match, err)
		}
		rel, err := filepath.Rel(root, match)
		if err != nil {
			return nil, fmt.Errorf("resolving %s relative to %s: %w", match, root, err)
		}
		rel = filepath.ToSlash(rel)
		isSprite := strings.TrimSuffix(path.Base(rel), ".svg") == iconSpriteName
		findings = append(findings, validateIconSVG(rel, data, isSprite)...)
	}
	sortFindings(findings)
	return findings, nil
}

// validateIconSVG validates one icon SVG per SP-140-1 §1f. It reuses the SVG
// walk for well-formedness and self-containment, applies the slug name rule
// (except to the fixed sprite.svg), and — for sprite.svg — checks the
// <symbol> entries. The result is never nil.
func validateIconSVG(relPath string, content []byte, isSprite bool) []Finding {
	findings := []Finding{}
	walk := walkSVG(content)

	if walk.error != nil {
		findings = append(findings, Finding{
			File:     relPath,
			Line:     walk.errorLine,
			Rule:     ruleIconWellformed,
			Severity: SeverityError,
			Message:  fmt.Sprintf("not well-formed XML: %v", walk.error),
		})
		return finalizeIconFindings(findings)
	}
	if !walk.sawRoot {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleIconWellformed,
			Severity: SeverityError,
			Message:  "missing root element; an icon must be a single <svg> document",
		})
		return finalizeIconFindings(findings)
	}

	// self-containment (hard).
	scriptOff := 0
	for range walk.scripts {
		off := findTagOffset(content, "script", scriptOff)
		findings = append(findings, Finding{
			File:     relPath,
			Line:     lineOfOffset(content, off),
			Rule:     ruleIconSelfContainment,
			Severity: SeverityError,
			Message:  "icons must be self-contained: remove <script>",
		})
		if off >= 0 {
			scriptOff = off + 1
		}
	}
	for _, r := range walk.resourceRefs {
		verdict := classifyResourceRef(r.value)
		if verdict == "" {
			continue
		}
		off := findAttrValueOffset(content, r.attr, r.value, 0)
		findings = append(findings, Finding{
			File:     relPath,
			Line:     lineOfOffset(content, off),
			Rule:     ruleIconSelfContainment,
			Severity: SeverityError,
			Message:  fmt.Sprintf("self-containment: %s reference %q in <%s> breaks self-containment (embed a data: URI or reference from brand/)", verdict, r.value, r.tag),
		})
	}

	// slug name rule (hard) — the fixed sprite.svg name is exempt.
	if !isSprite {
		stem := strings.TrimSuffix(path.Base(filepath.ToSlash(relPath)), ".svg")
		if !frameNameRe.MatchString(stem) {
			findings = append(findings, Finding{
				File:     relPath,
				Rule:     ruleIconSlugName,
				Severity: SeverityError,
				Message:  fmt.Sprintf("icon stem %q must match the slug rule %s", stem, SlugPattern),
			})
		}
	}

	// sprite symbols (hard on bad ids, advisory on an empty sprite).
	if isSprite {
		if len(walk.symbols) == 0 {
			findings = append(findings, Finding{
				File:     relPath,
				Rule:     ruleIconSpriteSymbol,
				Severity: SeverityInfo,
				Message:  "sprite.svg has no <symbol> entries",
			})
		}
		for _, s := range walk.symbols {
			if !frameNameRe.MatchString(s.id) {
				findings = append(findings, Finding{
					File:     relPath,
					Rule:     ruleIconSpriteSymbol,
					Severity: SeverityError,
					Message:  fmt.Sprintf("sprite <symbol id=%q> must follow the slug rule %s", s.id, SlugPattern),
				})
			}
		}
	}

	return finalizeIconFindings(findings)
}

// finalizeIconFindings normalizes an icon finding slice: never nil and sorted
// deterministically (file, line, rule, message).
func finalizeIconFindings(findings []Finding) []Finding {
	if findings == nil {
		findings = []Finding{}
	}
	sortFindings(findings)
	return findings
}
