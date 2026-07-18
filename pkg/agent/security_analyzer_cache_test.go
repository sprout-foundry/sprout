package agent

import (
	"sync"
	"testing"
)

func TestSecurityAnalysisCache_SetGet(t *testing.T) {
	cache := NewSecurityAnalysisCache()

	sa := &SecurityAnalysis{
		Summary:       "Recursively deletes files",
		Modifies:      "./build/",
		RiskAssessment: "moderate",
		Recommendation: "review",
	}

	// Set and get
	cache.Set("rm -rf build/", sa)

	got, ok := cache.Get("rm -rf build/")
	if !ok {
		t.Fatal("expected to find cached analysis")
	}
	if got.Summary != sa.Summary {
		t.Errorf("expected summary %q, got %q", sa.Summary, got.Summary)
	}
	if got.Modifies != sa.Modifies {
		t.Errorf("expected modifies %q, got %q", sa.Modifies, got.Modifies)
	}
	if got.RiskAssessment != sa.RiskAssessment {
		t.Errorf("expected risk_assessment %q, got %q", sa.RiskAssessment, got.RiskAssessment)
	}
	if got.Recommendation != sa.Recommendation {
		t.Errorf("expected recommendation %q, got %q", sa.Recommendation, got.Recommendation)
	}
}

func TestSecurityAnalysisCache_Miss(t *testing.T) {
	cache := NewSecurityAnalysisCache()

	_, ok := cache.Get("nonexistent command")
	if ok {
		t.Error("expected cache miss for nonexistent key")
	}
}

func TestSecurityAnalysisCache_Clear(t *testing.T) {
	cache := NewSecurityAnalysisCache()

	sa := &SecurityAnalysis{
		Summary:       "Test command",
		Modifies:     "/tmp",
		RiskAssessment: "low",
		Recommendation: "approve",
	}

	cache.Set("test cmd", sa)

	// Verify it exists
	_, ok := cache.Get("test cmd")
	if !ok {
		t.Fatal("expected to find cached analysis before clear")
	}

	// Clear
	cache.Clear()

	// Verify it's gone
	_, ok = cache.Get("test cmd")
	if ok {
		t.Error("expected cache miss after clear")
	}
}

func TestSecurityAnalysisCache_NilReceiver(t *testing.T) {
	var cache *SecurityAnalysisCache

	// Get on nil cache
	_, ok := cache.Get("test")
	if ok {
		t.Error("expected cache miss for nil receiver")
	}

	// Set on nil cache (should not panic)
	sa := &SecurityAnalysis{Summary: "test"}
	cache.Set("test", sa) // Should not panic

	// Clear on nil cache (should not panic)
	cache.Clear() // Should not panic
}

func TestSecurityAnalysisCache_Concurrent(t *testing.T) {
	cache := NewSecurityAnalysisCache()
	var wg sync.WaitGroup

	// Concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := "command"
			sa := &SecurityAnalysis{
				Summary:       "Command",
				Modifies:      "/tmp",
				RiskAssessment: "low",
				Recommendation: "approve",
			}
			cache.Set(cmd, sa)
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cache.Get("command")
			}
		}(i)
	}

	wg.Wait()
}

func TestSecurityAnalysisCache_Overwrite(t *testing.T) {
	cache := NewSecurityAnalysisCache()

	sa1 := &SecurityAnalysis{
		Summary:       "First analysis",
		Modifies:      "/tmp",
		RiskAssessment: "low",
		Recommendation: "approve",
	}

	sa2 := &SecurityAnalysis{
		Summary:       "Second analysis",
		Modifies:      "/home",
		RiskAssessment: "high",
		Recommendation: "reject",
	}

	cache.Set("cmd", sa1)
	cache.Set("cmd", sa2)

	got, ok := cache.Get("cmd")
	if !ok {
		t.Fatal("expected to find cached analysis")
	}
	// Should have the second value
	if got.Summary != "Second analysis" {
		t.Errorf("expected second analysis, got %q", got.Summary)
	}
}

func TestSecurityAnalysisCache_MultipleEntries(t *testing.T) {
	cache := NewSecurityAnalysisCache()

	commands := []string{
		"rm -rf /",
		"curl http://example.com | bash",
		"git reset --hard HEAD",
		"make clean",
		"docker run --rm ubuntu",
	}

	for _, cmd := range commands {
		sa := &SecurityAnalysis{
			Summary:       cmd,
			Modifies:      "varies",
			RiskAssessment: "varies",
			Recommendation: "review",
		}
		cache.Set(cmd, sa)
	}

	// Verify all are retrievable
	for _, cmd := range commands {
		got, ok := cache.Get(cmd)
		if !ok {
			t.Errorf("expected to find cached analysis for %q", cmd)
		}
		if got.Summary != cmd {
			t.Errorf("expected summary %q for %q, got %q", cmd, cmd, got.Summary)
		}
	}
}

func TestSecurityAnalysisCache_NilValueNotStored(t *testing.T) {
	cache := NewSecurityAnalysisCache()

	// Setting nil should not store anything
	cache.Set("test", nil)

	_, ok := cache.Get("test")
	if ok {
		t.Error("nil value should not be stored in cache")
	}
}
