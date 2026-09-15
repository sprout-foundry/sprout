package design

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// frameNameRe enforces the package slug rule on frame names; frame
// names feed device-frame-aware sizing and wireframe frame matching, so
// a malformed name must fail at parse time.
var frameNameRe = regexp.MustCompile(SlugPattern)

// Frame is a device frame declared in the manifest's frames block.
type Frame struct {
	Name   string
	Width  int
	Height int
}

// ParseFrames extracts device frames from manifest content. The frames
// block starts at a non-indented `frames:` line and continues through
// indented `name: WxH` entries until the first non-indented, non-blank
// line (or end of input). Blank lines inside the block are ignored, and
// any indentation depth counts as an entry line. Frames are returned in
// declaration order.
//
// A manifest without a frames block yields an empty slice and no error;
// frame presence is validated separately. A frames block with no
// entries, duplicate names (checked across all blocks in the content),
// a frame name not matching the slug rule, a non-integer width or
// height, a non-lowercase-x separator, or non-positive dimensions is an
// error carrying the 1-based line number.
func ParseFrames(content string) ([]Frame, error) {
	var (
		frames     []Frame
		blockStart = -1
		blockLen   int
	)

	endBlock := func() error {
		if blockStart >= 0 {
			// Compare against the count captured at block start: an
			// entry-less block must error even when earlier blocks
			// already filled the cumulative slice.
			if len(frames) == blockLen {
				return fmt.Errorf("line %d: frames block has no entries", blockStart)
			}
			blockStart = -1
		}
		return nil
	}

	for i, raw := range strings.Split(content, "\n") {
		lineNo := i + 1
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		indented := raw[0] == ' ' || raw[0] == '\t'

		if blockStart >= 0 {
			if !indented {
				if err := endBlock(); err != nil {
					return nil, err
				}
			} else {
				frame, err := parseFrameEntry(trimmed, lineNo)
				if err != nil {
					return nil, err
				}
				if err := appendFrame(&frames, frame, lineNo); err != nil {
					return nil, err
				}
				continue
			}
		}

		if !indented && trimmed == "frames:" {
			blockStart = lineNo
			blockLen = len(frames)
		}
	}

	if err := endBlock(); err != nil {
		return nil, err
	}
	if frames == nil {
		frames = []Frame{}
	}
	return frames, nil
}

func appendFrame(frames *[]Frame, frame Frame, lineNo int) error {
	for _, existing := range *frames {
		if existing.Name == frame.Name {
			return fmt.Errorf("line %d: duplicate frame name %q", lineNo, frame.Name)
		}
	}
	*frames = append(*frames, frame)
	return nil
}

func parseFrameEntry(trimmed string, lineNo int) (Frame, error) {
	name, value, found := strings.Cut(trimmed, ":")
	if !found {
		return Frame{}, fmt.Errorf("line %d: frame entry %q is missing ':' after the name", lineNo, trimmed)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Frame{}, fmt.Errorf("line %d: frame entry %q has an empty name", lineNo, trimmed)
	}
	if !frameNameRe.MatchString(name) {
		return Frame{}, fmt.Errorf("line %d: frame name %q must match %s", lineNo, name, SlugPattern)
	}
	width, height, err := parseDimensions(strings.TrimSpace(value), lineNo, name)
	if err != nil {
		return Frame{}, err
	}
	return Frame{Name: name, Width: width, Height: height}, nil
}

// parsePlainInt accepts only unsigned decimal digits; strconv.Atoi
// alone would let a leading "+" or "-" through, which is not the
// manifest's plain-integer shape.
func parsePlainInt(s string) (int, error) {
	if s == "" || s[0] < '0' || s[0] > '9' {
		return 0, strconv.ErrSyntax
	}
	return strconv.Atoi(s)
}

func parseDimensions(value string, lineNo int, name string) (int, int, error) {
	parts := strings.Split(value, "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf(
			"line %d: frame %q has invalid dimensions %q: expected \"<width>x<height>\" with a lowercase \"x\" separator",
			lineNo, name, value)
	}
	width, err := parsePlainInt(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("line %d: frame %q has an invalid width %q: expected a plain integer", lineNo, name, parts[0])
	}
	height, err := parsePlainInt(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("line %d: frame %q has an invalid height %q: expected a plain integer", lineNo, name, parts[1])
	}
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("line %d: frame %q has non-positive dimensions %dx%d", lineNo, name, width, height)
	}
	return width, height, nil
}
