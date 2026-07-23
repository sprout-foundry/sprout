package agent

import (
	"regexp"
	"strings"
	"testing"
)

func TestBytesIndexByte(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		b    []byte
		c    byte
		want int
	}{
		{
			name: "found at first position",
			b:    []byte("abc"),
			c:    'a',
			want: 0,
		},
		{
			name: "found at last position",
			b:    []byte("abc"),
			c:    'c',
			want: 2,
		},
		{
			name: "found at middle position",
			b:    []byte("abc"),
			c:    'b',
			want: 1,
		},
		{
			name: "not found returns -1",
			b:    []byte("abc"),
			c:    'z',
			want: -1,
		},
		{
			name: "empty slice returns -1",
			b:    []byte{},
			c:    'a',
			want: -1,
		},
		{
			name: "nil slice returns -1",
			b:    nil,
			c:    'a',
			want: -1,
		},
		{
			name: "first occurrence returned",
			b:    []byte("banana"),
			c:    'a',
			want: 1,
		},
		{
			name: "null byte found",
			b:    []byte{'h', 'e', 0, 'l', 'l'},
			c:    0,
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := bytesIndexByte(tt.b, tt.c); got != tt.want {
				t.Errorf("bytesIndexByte(%v, %q) = %d, want %d", tt.b, tt.c, got, tt.want)
			}
		})
	}
}

func TestSearchBufferLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		path          string
		content       string
		pattern       string
		caseSensitive bool
		useRegex      bool
		max           int
		maxBytes      int
		wantMatched   int
		wantCapped    bool
		wantOutput    string
	}{
		{
			name:          "regex match",
			path:          "file.go",
			content:       "func main() {}\nfunc foo() {}",
			pattern:       "func \\w+",
			caseSensitive: true,
			useRegex:      true,
			max:           50,
			maxBytes:      10000,
			wantMatched:   2,
			wantCapped:    false,
			wantOutput:    "file.go:1:func main() {}\nfile.go:2:func foo() {}\n",
		},
		{
			name:          "substring match",
			path:          "file.go",
			content:       "hello world\nfoo bar\nhello again",
			pattern:       "hello",
			caseSensitive: true,
			useRegex:      false,
			max:           50,
			maxBytes:      10000,
			wantMatched:   2,
			wantCapped:    false,
			wantOutput:    "file.go:1:hello world\nfile.go:3:hello again\n",
		},
		{
			name:          "case sensitive true no match",
			path:          "file.txt",
			content:       "Hello World",
			pattern:       "hello",
			caseSensitive: true,
			useRegex:      false,
			max:           50,
			maxBytes:      10000,
			wantMatched:   0,
			wantCapped:    false,
			wantOutput:    "",
		},
		{
			name:          "case sensitive false matches",
			path:          "file.txt",
			content:       "Hello World",
			pattern:       "hello",
			caseSensitive: false,
			useRegex:      false,
			max:           50,
			maxBytes:      10000,
			wantMatched:   1,
			wantCapped:    false,
			wantOutput:    "file.txt:1:Hello World\n",
		},
		{
			name:          "max results capping",
			path:          "file.txt",
			content:       "match\nmatch\nmatch\nmatch\nmatch",
			pattern:       "match",
			caseSensitive: true,
			useRegex:      false,
			max:           2,
			maxBytes:      10000,
			wantMatched:   2,
			wantCapped:    true,
		},
		{
			name:          "max bytes capping",
			path:          "file.txt",
			content:       "this is a very long line of content that should be found\nsecond match line here too with more content",
			pattern:       "match",
			caseSensitive: false,
			useRegex:      false,
			max:           50,
			maxBytes:      50,
			wantMatched:   1,
			wantCapped:    true,
		},
		{
			name:          "long lines truncated to 240 chars",
			path:          "file.txt",
			content:       strings.Repeat("x", 300),
			pattern:       "x",
			caseSensitive: true,
			useRegex:      false,
			max:           50,
			maxBytes:      10000,
			wantMatched:   1,
			wantCapped:    false,
		},
		{
			name:          "no matches returns false",
			path:          "file.txt",
			content:       "hello world\nfoo bar",
			pattern:       "xyz",
			caseSensitive: true,
			useRegex:      false,
			max:           50,
			maxBytes:      10000,
			wantMatched:   0,
			wantCapped:    false,
			wantOutput:    "",
		},
		{
			name:          "multiple matches with correct line numbers",
			path:          "src/app.go",
			content:       "line one\nfunc main() {}\nline three\nfunc helper() {}\nline five",
			pattern:       "func",
			caseSensitive: true,
			useRegex:      false,
			max:           50,
			maxBytes:      10000,
			wantMatched:   2,
			wantCapped:    false,
			wantOutput:    "src/app.go:2:func main() {}\nsrc/app.go:4:func helper() {}\n",
		},
		{
			name:          "empty content no match",
			path:          "empty.txt",
			content:       "",
			pattern:       "anything",
			caseSensitive: true,
			useRegex:      false,
			max:           50,
			maxBytes:      10000,
			wantMatched:   0,
			wantCapped:    false,
			wantOutput:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			var matched int
			var re *regexp.Regexp
			if tt.useRegex {
				re = regexp.MustCompile(tt.pattern) // panics on bad pattern in test
			} else {
				re = regexp.MustCompile("x") // dummy, not used when useRegex=false
			}
			capped := searchBufferLines(&b, tt.path, tt.content, re, tt.pattern, tt.caseSensitive, tt.useRegex, &matched, tt.max, tt.maxBytes)

			if matched != tt.wantMatched {
				t.Errorf("searchBufferLines matched = %d, want %d", matched, tt.wantMatched)
			}
			if capped != tt.wantCapped {
				t.Errorf("searchBufferLines capped = %v, want %v", capped, tt.wantCapped)
			}
			if tt.wantOutput != "" && b.String() != tt.wantOutput {
				t.Errorf("searchBufferLines output = %q, want %q", b.String(), tt.wantOutput)
			}

			// Special check: long line truncation
			if tt.name == "long lines truncated to 240 chars" {
				got := b.String()
				if !strings.Contains(got, "...") {
					t.Errorf("long line should be truncated with '...'")
				}
				// Extract the line content part (after "file.txt:1:")
				afterPrefix := strings.TrimPrefix(got, "file.txt:1:")
				contentLine := strings.TrimRight(afterPrefix, "\n")
				// Verify the content starts with 240 'x' characters
				if !strings.HasPrefix(contentLine, strings.Repeat("x", 240)) {
					t.Error("truncated line should start with 240 'x' characters")
				}
				if !strings.HasSuffix(contentLine, "...") {
					t.Error("truncated line should end with '...'")
				}
			}
		})
	}
}

func TestGetSearchMaxBytes(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     int
	}{
		{
			name:     "default value",
			envValue: "",
			want:     102400,
		},
		{
			name:     "custom value 5000",
			envValue: "5000",
			want:     5000,
		},
		{
			name:     "invalid value uses default",
			envValue: "not_a_number",
			want:     102400,
		},
		{
			name:     "zero uses default",
			envValue: "0",
			want:     102400,
		},
		{
			name:     "negative uses default",
			envValue: "-100",
			want:     102400,
		},
		{
			name:     "large value",
			envValue: "1000000",
			want:     1000000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// GetEnvSimple("SEARCH_MAX_BYTES") checks SPROUT_SEARCH_MAX_BYTES
			sproutKey := "SPROUT_SEARCH_MAX_BYTES"

			t.Setenv(sproutKey, "")
			if tt.envValue != "" {
				t.Setenv(sproutKey, tt.envValue)
			}

			if got := getSearchMaxBytes(); got != tt.want {
				t.Errorf("getSearchMaxBytes() = %d, want %d (env=%q)", got, tt.want, tt.envValue)
			}
		})
	}
}
