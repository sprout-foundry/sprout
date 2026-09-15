package design

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFramesValid(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []Frame
	}{
		{
			name: "spec example block",
			content: "# Manifest\n\n" +
				"frames:\n" +
				"  desktop: 1440x900\n" +
				"  mobile: 390x844\n",
			want: []Frame{
				{Name: "desktop", Width: 1440, Height: 900},
				{Name: "mobile", Width: 390, Height: 844},
			},
		},
		{
			name: "tab indentation",
			content: "frames:\n" +
				"\tdesktop: 1440x900\n" +
				"\tmobile: 390x844\n",
			want: []Frame{
				{Name: "desktop", Width: 1440, Height: 900},
				{Name: "mobile", Width: 390, Height: 844},
			},
		},
		{
			name: "deeper indentation",
			content: "frames:\n" +
				"      desktop: 1440x900\n" +
				"\t\t  mobile: 390x844\n",
			want: []Frame{
				{Name: "desktop", Width: 1440, Height: 900},
				{Name: "mobile", Width: 390, Height: 844},
			},
		},
		{
			name: "trailing whitespace on entries",
			content: "frames:\n" +
				"  desktop: 1440x900   \n" +
				"  mobile: 390x844\t\n",
			want: []Frame{
				{Name: "desktop", Width: 1440, Height: 900},
				{Name: "mobile", Width: 390, Height: 844},
			},
		},
		{
			name:    "CRLF line endings",
			content: "frames:\r\n  desktop: 1440x900\r\n  mobile: 390x844\r\n",
			want: []Frame{
				{Name: "desktop", Width: 1440, Height: 900},
				{Name: "mobile", Width: 390, Height: 844},
			},
		},
		{
			name: "mid-document block with prose before and after",
			content: "# Title\n\nSome prose.\n\n" +
				"frames:\n" +
				"  desktop: 1440x900\n" +
				"  mobile: 390x844\n" +
				"\n" +
				"## Screens\n\nMore prose.\n",
			want: []Frame{
				{Name: "desktop", Width: 1440, Height: 900},
				{Name: "mobile", Width: 390, Height: 844},
			},
		},
		{
			name: "blank lines inside the block",
			content: "frames:\n" +
				"  desktop: 1440x900\n" +
				"\n" +
				"   \n" +
				"  mobile: 390x844\n" +
				"\n",
			want: []Frame{
				{Name: "desktop", Width: 1440, Height: 900},
				{Name: "mobile", Width: 390, Height: 844},
			},
		},
		{
			name:    "single frame",
			content: "frames:\n  desktop: 1440x900\n",
			want: []Frame{
				{Name: "desktop", Width: 1440, Height: 900},
			},
		},
		{
			name: "two separate blocks accumulate in order",
			content: "frames:\n" +
				"  desktop: 1440x900\n" +
				"\n" +
				"## Other section\n\n" +
				"frames:\n" +
				"  mobile: 390x844\n",
			want: []Frame{
				{Name: "desktop", Width: 1440, Height: 900},
				{Name: "mobile", Width: 390, Height: 844},
			},
		},
		{
			name: "block ended by non-indented line keeps only prior entries",
			content: "frames:\n" +
				"  desktop: 1440x900\n" +
				"  mobile: 390x844\n" +
				"## Screens\n" +
				"  tablet: 768x1024\n",
			want: []Frame{
				{Name: "desktop", Width: 1440, Height: 900},
				{Name: "mobile", Width: 390, Height: 844},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frames, err := ParseFrames(tt.content)
			require.NoError(t, err)
			assert.Equal(t, tt.want, frames)
		})
	}
}

func TestParseFramesErrors(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantLine     string
		wantFragment string
	}{
		{
			name:         "non-integer width",
			content:      "frames:\n  desktop: 14a0x900\n",
			wantLine:     "line 2",
			wantFragment: "invalid width",
		},
		{
			name:         "non-integer height",
			content:      "frames:\n  desktop: 1440x9a0\n",
			wantLine:     "line 2",
			wantFragment: "invalid height",
		},
		{
			name:         "missing colon",
			content:      "frames:\n  desktop 1440x900\n",
			wantLine:     "line 2",
			wantFragment: "missing ':'",
		},
		{
			name:         "uppercase X separator",
			content:      "frames:\n  desktop: 1440X900\n",
			wantLine:     "line 2",
			wantFragment: "invalid dimensions",
		},
		{
			name:         "asterisk separator",
			content:      "frames:\n  desktop: 1440*900\n",
			wantLine:     "line 2",
			wantFragment: "invalid dimensions",
		},
		{
			name:         "missing height",
			content:      "frames:\n  desktop: 1440\n",
			wantLine:     "line 2",
			wantFragment: "invalid dimensions",
		},
		{
			name:         "empty name",
			content:      "frames:\n  : 1440x900\n",
			wantLine:     "line 2",
			wantFragment: "empty name",
		},
		{
			name:         "name with a space is not a slug",
			content:      "frames:\n  my frame: 100x100\n",
			wantLine:     "line 2",
			wantFragment: "must match",
		},
		{
			name:         "uppercase name is not a slug",
			content:      "frames:\n  Desktop: 100x100\n",
			wantLine:     "line 2",
			wantFragment: "must match",
		},
		{
			name:         "underscore name is not a slug",
			content:      "frames:\n  my_frame: 100x100\n",
			wantLine:     "line 2",
			wantFragment: "must match",
		},
		{
			name:         "empty frames block",
			content:      "frames:\n\n## Next section\n",
			wantLine:     "line 1",
			wantFragment: "frames block has no entries",
		},
		{
			name: "entry-less second block after a valid block",
			content: "frames:\n" +
				"  desktop: 1440x900\n" +
				"\n" +
				"## Other\n\n" +
				"frames:\n",
			wantLine:     "line 6",
			wantFragment: "frames block has no entries",
		},
		{
			name: "entry-less middle block between valid blocks",
			content: "frames:\n" +
				"  desktop: 1440x900\n" +
				"\n" +
				"frames:\n" +
				"\n" +
				"frames:\n" +
				"  mobile: 390x844\n",
			wantLine:     "line 4",
			wantFragment: "frames block has no entries",
		},
		{
			name:         "duplicate name within a block",
			content:      "frames:\n  desktop: 1440x900\n  desktop: 1024x768\n",
			wantLine:     "line 3",
			wantFragment: "duplicate frame name",
		},
		{
			name:         "duplicate name across blocks",
			content:      "frames:\n  desktop: 1440x900\n\n## Other\n\nframes:\n  desktop: 1024x768\n",
			wantLine:     "line 7",
			wantFragment: "duplicate frame name",
		},
		{
			name:         "zero width",
			content:      "frames:\n  desktop: 0x100\n",
			wantLine:     "line 2",
			wantFragment: "non-positive dimensions",
		},
		{
			name:         "negative width is not a plain integer",
			content:      "frames:\n  desktop: -5x100\n",
			wantLine:     "line 2",
			wantFragment: "invalid width",
		},
		{
			name:         "plus-signed width is not a plain integer",
			content:      "frames:\n  desktop: +5x100\n",
			wantLine:     "line 2",
			wantFragment: "invalid width",
		},
		{
			name:         "mid-document block reports entry line",
			content:      "# Title\n\nProse.\n\nframes:\n  desktop: 1440\n\nMore prose.\n",
			wantLine:     "line 6",
			wantFragment: "invalid dimensions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frames, err := ParseFrames(tt.content)
			require.Error(t, err, "expected an error for content:\n%s", tt.content)
			assert.Nil(t, frames, "no frames should be returned on error")
			assert.Contains(t, err.Error(), tt.wantLine,
				"error must carry the 1-based line number of the offending entry")
			assert.Contains(t, err.Error(), tt.wantFragment)
		})
	}
}

func TestParseFramesNoBlock(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "empty content", content: ""},
		{name: "prose only", content: "# Manifest\n\nSome prose.\n"},
		{name: "colon but no frames keyword", content: "screens: none\n"},
		{name: "indented frames line is not a block start", content: "  frames:\n  desktop: 1440x900\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frames, err := ParseFrames(tt.content)
			require.NoError(t, err)
			assert.Empty(t, frames)
			assert.NotNil(t, frames, "result must be an empty non-nil slice")
		})
	}
}

func TestParseFramesEmbeddedManifestTemplate(t *testing.T) {
	template, err := ManifestTemplate()
	require.NoError(t, err)

	frames, err := ParseFrames(string(template))
	require.NoError(t, err, "the embedded manifest template must parse")
	assert.Equal(t, []Frame{
		{Name: "desktop", Width: 1440, Height: 900},
		{Name: "mobile", Width: 390, Height: 844},
		{Name: "tablet", Width: 768, Height: 1024},
	}, frames)
	assert.True(t, strings.Contains(string(template), "frames:"),
		"template must declare a frames block")
}
