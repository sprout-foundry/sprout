package design

// inventory_listing.go — manifest-listing enrichment, split out of
// inventory.go. enrichAssetRows back-fills each asset row's Status and
// Summary from the design/README.md listing; parseManifestListings and
// splitDashSegments parse the "- `name` — status — summary" bullets that
// carry those values.
import (
	"os"
	"path/filepath"
	"strings"
)

// enrichAssetRows fills the Status and Summary fields on each asset row from
// the manifest listing (`- \`login\` — ready — sign-in entry point`). It is
// best-effort enrichment: a missing or unparsable manifest leaves the fields
// as they are.
func enrichAssetRows(rows []AssetRow, root string) {
	data, err := os.ReadFile(filepath.Join(root, DirName, ManifestName))
	if err != nil {
		return
	}
	statuses, summaries := parseManifestListings(string(data))
	for i := range rows {
		if s, ok := statuses[rows[i].Name]; ok {
			rows[i].Status = s
		}
		if s, ok := summaries[rows[i].Name]; ok {
			rows[i].Summary = s
		}
	}
}

// manifestListingRe captures a manifest listing bullet: a backticked name,
// optionally followed by an em/en-dash-separated status and summary.
var listingDash = "—" // em dash (U+2014), the separator the manifest template uses

// parseManifestListings extracts, per backticked asset name, its status marker
// and free-text summary from manifest listing lines of the form:
//
//   - `login` — ready — sign-in entry point
//   - `sign-up` — draft
//
// The middle segment is treated as a status only when it is exactly one of
// draft/review/ready; otherwise the whole tail is a summary. Names are
// returned in lowercase-keyed maps.
func parseManifestListings(text string) (statuses, summaries map[string]string) {
	statuses = map[string]string{}
	summaries = map[string]string{}
	known := map[string]bool{"draft": true, "review": true, "ready": true}

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		trimmed = strings.TrimSpace(trimmed[2:])
		start := strings.IndexByte(trimmed, '`')
		if start < 0 {
			continue
		}
		rest := trimmed[start+1:]
		end := strings.IndexByte(rest, '`')
		if end < 0 {
			continue
		}
		name := strings.TrimSpace(rest[:end])
		tail := strings.TrimSpace(rest[end+1:])
		if name == "" {
			continue
		}

		// Split the tail on the dash separator, handling the em dash and the
		// ASCII/typographic variants the template and humans may use.
		segments := splitDashSegments(tail)
		switch {
		case len(segments) == 0:
			// no listing text.
		case len(segments) == 1:
			if known[strings.ToLower(segments[0])] {
				statuses[name] = strings.ToLower(segments[0])
			} else {
				summaries[name] = segments[0]
			}
		default:
			if known[strings.ToLower(segments[0])] {
				statuses[name] = strings.ToLower(segments[0])
				if s := strings.TrimSpace(strings.Join(segments[1:], " "+listingDash+" ")); s != "" {
					summaries[name] = s
				}
			} else {
				summaries[name] = strings.Join(segments, " "+listingDash+" ")
			}
		}
	}
	return statuses, summaries
}

// splitDashSegments splits a manifest listing tail on dash separators,
// trimming each segment. The template writes the em dash with surrounding
// spaces (`- \`login\` — draft — summary`); a plain hyphen surrounded by spaces
// is accepted too so human-authored manifests enrich. Because the caller
// trims the tail, a leading separator surfaces as `- ` / `— ` with no leading
// space, which is normalized before splitting. Empty segments are dropped.
func splitDashSegments(tail string) []string {
	tail = strings.TrimSpace(tail)
	if tail == "" {
		return nil
	}
	// Normalize a leading/trailing separator to the internal form so one set
	// of rules covers every shape.
	for _, d := range []string{listingDash, "-"} {
		tail = strings.TrimPrefix(tail, d+" ")
		tail = strings.TrimSuffix(tail, " "+d)
	}
	normalized := strings.ReplaceAll(tail, " "+listingDash+" ", "\x00")
	normalized = strings.ReplaceAll(normalized, " - ", "\x00")
	parts := strings.Split(normalized, "\x00")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
