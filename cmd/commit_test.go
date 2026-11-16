package cmd

import "testing"

func TestCommitModelOverride(t *testing.T) {
	cases := []struct{ provider, model, wantP, wantM string }{
		{"deepinfra", "deepseek-ai/DeepSeek-V4.1-Flash-1017", "deepinfra", "deepseek-ai/DeepSeek-V4.1-Flash-1017"},
		{"", "deepinfra:deepseek-ai/DeepSeek-V4.1-Flash-1017", "deepinfra", "deepseek-ai/DeepSeek-V4.1-Flash-1017"},
		{"", "qwen3-coder:30b", "", "qwen3-coder:30b"}, // not a provider prefix
		{"", "", "", ""},
	}
	for _, tc := range cases {
		p, m := commitModelOverride(tc.provider, tc.model)
		if p != tc.wantP || m != tc.wantM {
			t.Errorf("commitModelOverride(%q, %q) = %q, %q; want %q, %q", tc.provider, tc.model, p, m, tc.wantP, tc.wantM)
		}
	}
}
