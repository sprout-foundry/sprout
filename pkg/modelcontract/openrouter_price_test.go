package modelcontract

import "testing"

func TestParsePerTokenUSD(t *testing.T) {
	for in, want := range map[string]float64{"0": 0, "0.0000002": 0.2, "0.000015": 15} {
		got, ok := parsePerTokenUSD(in)
		if !ok || got < want-1e-9 || got > want+1e-9 {
			t.Errorf("parsePerTokenUSD(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	// "-1" is OpenRouter's variable-price marker for router models: it must
	// leave the model unpriced, never become a negative price.
	for _, in := range []string{"", "x", "-1", "-0.000001"} {
		if got, ok := parsePerTokenUSD(in); ok {
			t.Errorf("parsePerTokenUSD(%q) = %v; want unpriced", in, got)
		}
	}
}
