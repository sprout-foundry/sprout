package config

import (
	"testing"
)

func TestBatchSizeDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.setDefaultValues()

	// Test that batch size defaults are set correctly
	if cfg.FileBatchSize != 30 {
		t.Errorf("Expected FileBatchSize to be 30, got %d", cfg.FileBatchSize)
	}

	if cfg.EmbeddingBatchSize != 30 {
		t.Errorf("Expected EmbeddingBatchSize to be 30, got %d", cfg.EmbeddingBatchSize)
	}

	if cfg.MaxConcurrentRequests != 3 {
		t.Errorf("Expected MaxConcurrentRequests to be 3, got %d", cfg.MaxConcurrentRequests)
	}

	if cfg.RequestDelayMs != 100 {
		t.Errorf("Expected RequestDelayMs to be 100, got %d", cfg.RequestDelayMs)
	}
}

func TestBatchSizeOverrides(t *testing.T) {
	cfg := &Config{
		FileBatchSize:         20,
		EmbeddingBatchSize:    10,
		MaxConcurrentRequests: 5,
		RequestDelayMs:        200,
	}
	cfg.setDefaultValues()

	// Test that custom values are preserved
	if cfg.FileBatchSize != 20 {
		t.Errorf("Expected FileBatchSize to be 20, got %d", cfg.FileBatchSize)
	}

	if cfg.EmbeddingBatchSize != 10 {
		t.Errorf("Expected EmbeddingBatchSize to be 10, got %d", cfg.EmbeddingBatchSize)
	}

	if cfg.MaxConcurrentRequests != 5 {
		t.Errorf("Expected MaxConcurrentRequests to be 5, got %d", cfg.MaxConcurrentRequests)
	}

	if cfg.RequestDelayMs != 200 {
		t.Errorf("Expected RequestDelayMs to be 200, got %d", cfg.RequestDelayMs)
	}
}
