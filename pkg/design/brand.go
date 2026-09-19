package design

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Rule ids for the brand validator, SP-140-1 §1d.
const (
	// ruleBrandRawHex fires (warn) when brand.md carries a raw hex color
	// instead of a reference into the token files.
	ruleBrandRawHex = "brand_raw_hex"

	// ruleBrandNoTokenRefs is advisory: brand.md references no design tokens.
	ruleBrandNoTokenRefs = "brand_no_token_refs"
)

// brandHexRunRe matches a maximal run of hex digits following '#'. The run is
// then length-checked (a raw CSS hex color is 3, 4, 6, or 8 digits); common
// markdown headers such as "#Brand" or "#Voice" never match because their
// first character is not a hex digit (SP-140-1 §1d).
var brandHexRunRe = regexp.MustCompile(`#[0-9a-fA-F]+`)

// brandHexLengths are the digit counts of a valid CSS hex color (#RGB, #RGBA,
// #RRGGBB, #RRGGBBAA).
var brandHexLengths = map[int]bool{3: true, 4: true, 6: true, 8: true}

// brandTokenRefRe matches a design-token reference in the {group.token}
// dot-path form.
var brandTokenRefRe = regexp.MustCompile(`\{[A-Za-z][A-Za-z0-9]*(?:\.[A-Za-z0-9_-]+)+\}`)

// ValidateBrandDir validates design/brand/brand.md under root, SP-140-1 §1d.
// A missing brand directory or brand.md yields no findings (not an error) —
// a whole-tree run must not fail on workspaces without a brand file.
func ValidateBrandDir(root string) []Finding {
	data, err := os.ReadFile(filepath.Join(root, DirName, "brand", "brand.md"))
	if err != nil {
		return []Finding{}
	}
	return ValidateBrandFile(filepath.Join(DirName, "brand", "brand.md"), data)
}

// ValidateBrandFile validates one brand.md file. A raw hex color is a warn
// finding (reference a token instead); a brand file with no token references
// is advisory (info). The result is never nil.
func ValidateBrandFile(relPath string, content []byte) []Finding {
	text := string(content)
	findings := []Finding{}

	for _, m := range brandHexRunRe.FindAllString(text, -1) {
		digits := m[1:]
		if !brandHexLengths[len(digits)] {
			continue
		}
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleBrandRawHex,
			Severity: SeverityWarn,
			Message:  fmt.Sprintf("raw hex color %s in brand.md; reference a design token instead (e.g. {color.brand.primary})", m),
		})
	}

	if len(brandTokenRefRe.FindAllString(text, -1)) == 0 {
		findings = append(findings, Finding{
			File:     relPath,
			Rule:     ruleBrandNoTokenRefs,
			Severity: SeverityInfo,
			Message:  "brand.md does not reference any design tokens ({group.token}); the palette should reference tokens, not raw values",
		})
	}

	sortFindings(findings)
	return findings
}
