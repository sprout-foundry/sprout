package design

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFramesEntryLessBlockAtEOF(t *testing.T) {
	frames, err := ParseFrames("# Manifest\n\nSome prose.\n\nframes:\n")

	require.Error(t, err, "an entry-less frames block must error even when ended by EOF")
	assert.Nil(t, frames)
	assert.Contains(t, err.Error(), "line 5")
	assert.Contains(t, err.Error(), "frames block has no entries")
}

func TestParseFramesTrailingSpacesOnBlockStart(t *testing.T) {
	frames, err := ParseFrames("frames:  \n  desktop: 1440x900\n  mobile: 390x844\n")

	require.NoError(t, err)
	assert.Equal(t, []Frame{
		{Name: "desktop", Width: 1440, Height: 900},
		{Name: "mobile", Width: 390, Height: 844},
	}, frames, "trailing whitespace on the frames: line must not hide the block")
}

func TestParseFramesEntryShapedLineEndsBlock(t *testing.T) {
	frames, err := ParseFrames(
		"frames:\n" +
			"  desktop: 1440x900\n" +
			"tablet: 768x1024\n")

	require.NoError(t, err, "a non-indented entry-shaped line ends the block rather than erroring")
	assert.Equal(t, []Frame{{Name: "desktop", Width: 1440, Height: 900}}, frames,
		"only the entries before the non-indented line belong to the block")
}

func TestParseFramesValueTrailingContentIsRejected(t *testing.T) {
	frames, err := ParseFrames("frames:\n  desktop: 1440x900 extra: stuff\n")

	require.Error(t, err, "any trailing content after the WxH dimensions must not parse as 1440x900")
	assert.Nil(t, frames)
	assert.Contains(t, err.Error(), "line 2")
	assert.Contains(t, err.Error(), "invalid dimensions")
}

func TestParseFramesHugeDimensionsRejected(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "width overflows int", value: "99999999999999999999x900"},
		{name: "height overflows int", value: "1440x99999999999999999999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frames, err := ParseFrames("frames:\n  desktop: " + tt.value + "\n")

			require.Error(t, err, "dimensions beyond int range must error, not wrap around")
			assert.Nil(t, frames)
			assert.Contains(t, err.Error(), "expected a plain integer")
		})
	}
}

func TestManifestTemplateDirectoryContract(t *testing.T) {
	template, err := ManifestTemplate()
	require.NoError(t, err)
	body := string(template)

	for _, sub := range Subdirs {
		assert.Contains(t, body, "`"+sub+"/`",
			"template layout table must cover contract subdirectory %q", sub)
	}

	statuses := []string{"draft", "review", "ready"}
	for _, status := range statuses {
		assert.Contains(t, body, "`"+status+"`",
			"template must document the %q status marker", status)
	}

	assert.Contains(t, body, SlugPattern,
		"template must state the slug rule verbatim")
	assert.Contains(t, body, "## Screens",
		"template must have a screens purpose section")
	assert.Contains(t, body, "## Flows",
		"template must have a flows purpose section")
	assert.Contains(t, body, "## Links",
		"template must have a links section")

	links := regexp.MustCompile(`\]\(([^)]+)\)`).FindAllStringSubmatch(body, -1)
	require.NotEmpty(t, links, "template links section must contain at least one relative link")
	for _, link := range links {
		_, ok := SubdirByName(strings.SplitN(link[1], "/", 2)[0])
		assert.True(t, ok,
			"template link target %q must be relative inside design/, never absolute", link[1])
	}
}
