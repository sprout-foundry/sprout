package webui

import "testing"

// The readiness probe is matched against real PTY bytes, which differ per
// shell: the printed line carries the shell's prompt prefix, and the echoed
// command line carries the token too. These are the shapes observed from the
// shells the test and production paths run under.
func TestProbeAnswered(t *testing.T) {
	const token = "SPROUT-READY-1792783368277166099"

	cases := []struct {
		name string
		seen string
		want bool
	}{
		{
			name: "dash prompt prefixes the printed line",
			// ubuntu /bin/sh: prompt is "$ ", and the echo of the command
			// appears first with the token inside the printf string.
			seen: "printf '" + token + "\\n'\r\n$ " + token + "\r\n$ ",
			want: true,
		},
		{
			name: "bash prompt prefixes the printed line",
			seen: "printf '" + token + "\\n'\r\n$ " + token + "\r\n$ ",
			want: true,
		},
		{
			name: "zsh percent prompt",
			seen: "printf '" + token + "\\n'\r\n% " + token + "\r\n% ",
			want: true,
		},
		{
			name: "no prompt at all",
			seen: "printf '" + token + "\\n'\r\n" + token + "\r\n",
			want: true,
		},
		{
			name: "printed line still arriving",
			seen: "printf '" + token + "\\n'\r\n$ " + token,
			want: true,
		},
		{
			name: "only the echoed command line so far",
			seen: "printf '" + token + "\\n'\r\n",
			want: false,
		},
		{
			name: "echoed command line with no trailing newline yet",
			seen: "printf '" + token + "\\n'",
			want: false,
		},
		{
			name: "echo truncated right after the token mid-line",
			// The buffer ends inside the echoed printf string, before the \n
			// escape arrives. This must not be mistaken for the printed line.
			seen: "printf '" + token,
			want: false,
		},
		{
			name: "unrelated output",
			seen: "welcome to the shell\r\n$ ",
			want: false,
		},
		{
			name: "empty",
			seen: "",
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := probeAnswered([]byte(tc.seen), []byte(token)); got != tc.want {
				t.Errorf("probeAnswered(%q) = %v, want %v", tc.seen, got, tc.want)
			}
		})
	}
}
