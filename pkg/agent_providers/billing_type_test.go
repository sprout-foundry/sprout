package providers

import "testing"

func TestBillingTypeResolved_Explicit(t *testing.T) {
	tests := []struct {
		name        string
		billingType string
		endpoint    string
		providerName string
		want        string
	}{
		{
			name:         "explicit pay_per_token",
			billingType:  BillingPayPerToken,
			endpoint:     "https://api.example.com",
			providerName: "example",
			want:         BillingPayPerToken,
		},
		{
			name:         "explicit subscription",
			billingType:  BillingSubscription,
			endpoint:     "https://api.example.com",
			providerName: "example",
			want:         BillingSubscription,
		},
		{
			name:         "explicit free",
			billingType:  BillingFree,
			endpoint:     "https://api.example.com",
			providerName: "example",
			want:         BillingFree,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ProviderConfig{
				Name:        tt.providerName,
				BillingType: tt.billingType,
				Endpoint:    tt.endpoint,
			}
			got := cfg.BillingTypeResolved()
			if got != tt.want {
				t.Errorf("BillingTypeResolved() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBillingTypeResolved_Heuristics(t *testing.T) {
	tests := []struct {
		name         string
		endpoint     string
		providerName string
		want         string
	}{
		{
			name:         "localhost endpoint → free",
			endpoint:     "http://localhost:8080/v1",
			providerName: "local",
			want:         BillingFree,
		},
		{
			name:         "127.0.0.1 endpoint → free",
			endpoint:     "http://127.0.0.1:5001",
			providerName: "local",
			want:         BillingFree,
		},
		{
			name:         "zai-coding name → subscription",
			endpoint:     "https://api.zai.com/v1",
			providerName: "zai-coding",
			want:         BillingSubscription,
		},
		{
			name:         "remote endpoint + non-zai → pay_per_token",
			endpoint:     "https://api.openai.com/v1",
			providerName: "openai",
			want:         BillingPayPerToken,
		},
		{
			name:         "remote endpoint + unknown provider → pay_per_token",
			endpoint:     "https://some-remote-api.example.com",
			providerName: "unknown-provider",
			want:         BillingPayPerToken,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ProviderConfig{
				Name:        tt.providerName,
				BillingType: "", // empty — force heuristic resolution
				Endpoint:    tt.endpoint,
			}
			got := cfg.BillingTypeResolved()
			if got != tt.want {
				t.Errorf("BillingTypeResolved() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBillingTypeResolved_ExplicitOverridesHeuristics(t *testing.T) {
	// Even with a localhost endpoint, an explicit billing_type wins.
	cfg := &ProviderConfig{
		Name:        "local",
		BillingType: BillingPayPerToken,
		Endpoint:    "http://localhost:8080/v1",
	}
	got := cfg.BillingTypeResolved()
	if got != BillingPayPerToken {
		t.Errorf("BillingTypeResolved() = %q, want %q (explicit should override localhost heuristic)", got, BillingPayPerToken)
	}

	// Even with zai-coding name, an explicit billing_type wins.
	cfg2 := &ProviderConfig{
		Name:        "zai-coding",
		BillingType: BillingPayPerToken,
		Endpoint:    "https://api.zai.com/v1",
	}
	got2 := cfg2.BillingTypeResolved()
	if got2 != BillingPayPerToken {
		t.Errorf("BillingTypeResolved() = %q, want %q (explicit should override zai-coding heuristic)", got2, BillingPayPerToken)
	}
}
