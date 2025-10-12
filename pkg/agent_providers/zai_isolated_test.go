package providers

import (
	"fmt"
	"os"
	"testing"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// TestZAIProviderIsolated tests the ZAI provider in isolation
func TestZAIProviderIsolated(t *testing.T) {
	if os.Getenv("ZAI_API_KEY") == "" {
		t.Skip("ZAI_API_KEY not set, skipping ZAI isolation test")
	}

	provider, err := NewZAIProvider()
	if err != nil {
		t.Fatalf("Failed to create ZAI provider: %v", err)
	}

	provider.SetDebug(true)

	// Test 1: Check connection
	t.Run("Connection", func(t *testing.T) {
		start := time.Now()
		err := provider.CheckConnection()
		if err != nil {
			t.Fatalf("Connection test failed: %v", err)
		}
		t.Logf("✅ Connection test passed in %v", time.Since(start))
	})

	// Test 2: Simple non-streaming request
	t.Run("NonStreaming", func(t *testing.T) {
		messages := []api.Message{
			{Role: "user", Content: "What is 2+2? Answer with just the number."},
		}

		start := time.Now()
		resp, err := provider.SendChatRequest(messages, nil, "")
		if err != nil {
			t.Fatalf("Non-streaming request failed: %v", err)
		}
		t.Logf("✅ Non-streaming request completed in %v", time.Since(start))
		t.Logf("📝 Response: %s", resp.Choices[0].Message.Content)
	})

	// Test 3: Simple streaming request
	t.Run("Streaming", func(t *testing.T) {
		messages := []api.Message{
			{Role: "user", Content: "Say hello in exactly one word."},
		}

		start := time.Now()
		var streamContent string
		_, err := provider.SendChatRequestStream(messages, nil, "", func(chunk string) {
			streamContent += chunk
			t.Logf("📦 Chunk: %q", chunk)
		})
		if err != nil {
			t.Fatalf("Streaming request failed: %v", err)
		}
		t.Logf("✅ Streaming request completed in %v", time.Since(start))
		t.Logf("📝 Stream response: %s", streamContent)
	})
}

// TestZAIProviderDirect is a standalone test that can be run manually
func TestZAIProviderDirect(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping ZAI direct test in short mode")
	}

	if os.Getenv("ZAI_API_KEY") == "" {
		t.Skip("ZAI_API_KEY not set, skipping ZAI direct test")
	}

	fmt.Println("🔍 Testing ZAI provider directly...")

	provider, err := NewZAIProvider()
	if err != nil {
		t.Fatalf("❌ Failed to create ZAI provider: %v", err)
	}

	provider.SetDebug(true)

	// Test with a very simple request
	messages := []api.Message{
		{Role: "user", Content: "1+1=?"},
	}

	fmt.Println("📡 Sending simple request...")
	start := time.Now()

	// Test non-streaming first
	resp, err := provider.SendChatRequest(messages, nil, "")
	if err != nil {
		t.Fatalf("❌ Non-streaming failed: %v", err)
	}

	duration := time.Since(start)
	fmt.Printf("✅ Non-streaming completed in %v\n", duration)
	fmt.Printf("📝 Response: %q\n", resp.Choices[0].Message.Content)

	// Test streaming
	fmt.Println("🌊 Testing streaming...")
	start = time.Now()
	var streamContent string
	resp, err = provider.SendChatRequestStream(messages, nil, "", func(chunk string) {
		streamContent += chunk
		fmt.Printf("💭 %s", chunk)
	})
	if err != nil {
		t.Fatalf("❌ Streaming failed: %v", err)
	}

	duration = time.Since(start)
	fmt.Printf("\n✅ Streaming completed in %v\n", duration)
	fmt.Printf("📝 Stream response: %q\n", streamContent)
}
