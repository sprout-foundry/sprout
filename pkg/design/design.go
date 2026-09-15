// Package design defines the design workspace directory contract: the
// design/ tree layout, the manifest scaffold, and parsers for the
// machine-readable manifest blocks. See roadmap/SP-140-1-formats.md.
package design

import "path/filepath"

const (
	// DirName is the design workspace root directory, relative to the
	// project root.
	DirName = "design"

	// ManifestName is the inventory manifest at the design root.
	ManifestName = "README.md"

	// SlugPattern is the name rule for screens, flows, and icons.
	SlugPattern = `^[a-z0-9]+(-[a-z0-9]+)*$`
)

// Subdirs are the canonical design workspace subdirectories, in contract
// order.
var Subdirs = []string{
	"tokens",
	"brand",
	"icons",
	"wireframes",
	"screens",
	"flows",
	"feedback",
}

// SubdirByName returns the canonical subdirectory path under DirName for
// the given name (for example "wireframes" yields "design/wireframes").
// The second return value is false for a name not in Subdirs.
func SubdirByName(name string) (string, bool) {
	for _, sub := range Subdirs {
		if sub == name {
			return filepath.Join(DirName, sub), true
		}
	}
	return "", false
}

// SubdirsJoined returns each canonical subdirectory joined under DirName
// (for example "design/tokens"), in contract order. The result is always
// non-nil, even when there are no canonical subdirectories.
func SubdirsJoined() []string {
	joined := make([]string, 0, len(Subdirs))
	for _, sub := range Subdirs {
		joined = append(joined, filepath.Join(DirName, sub))
	}
	return joined
}
