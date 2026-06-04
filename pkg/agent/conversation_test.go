package agent

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestMakeAllowedToolSetFromConversation(t *testing.T) {
	t.Run("empty slice returns empty map", func(t *testing.T) {
		got := makeAllowedToolSet(nil)
		if len(got) != 0 {
			t.Errorf("got map with %d entries, want 0", len(got))
		}

		got = makeAllowedToolSet([]string{})
		if len(got) != 0 {
			t.Errorf("got map with %d entries, want 0", len(got))
		}
	})

	t.Run("names are added to map", func(t *testing.T) {
		got := makeAllowedToolSet([]string{"read_file", "write_file", "shell_command"})
		if len(got) != 3 {
			t.Errorf("got %d entries, want 3", len(got))
		}
		for _, name := range []string{"read_file", "write_file", "shell_command"} {
			if _, ok := got[name]; !ok {
				t.Errorf("missing %q in map", name)
			}
		}
	})

	t.Run("whitespace entries are filtered out", func(t *testing.T) {
		got := makeAllowedToolSet([]string{"read_file", "  ", "", "   ", "write_file"})
		if len(got) != 2 {
			t.Errorf("got %d entries, want 2 (whitespace entries should be filtered)", len(got))
		}
		if _, ok := got["read_file"]; !ok {
			t.Error("missing read_file")
		}
		if _, ok := got["write_file"]; !ok {
			t.Error("missing write_file")
		}
	})

	t.Run("duplicates are deduped", func(t *testing.T) {
		got := makeAllowedToolSet([]string{"read_file", "read_file", "write_file"})
		if len(got) != 2 {
			t.Errorf("got %d entries, want 2", len(got))
		}
	})

	t.Run("whitespace is trimmed from names", func(t *testing.T) {
		got := makeAllowedToolSet([]string{"  read_file  "})
		if len(got) != 1 {
			t.Fatalf("got %d entries, want 1", len(got))
		}
		if _, ok := got["read_file"]; !ok {
			t.Errorf("key should be 'read_file'")
		}
	})
}

func makeTool(name string) api.Tool {
	var t api.Tool
	t.Function.Name = name
	return t
}

func TestFilterToolsByNameFromConversation(t *testing.T) {
	tools := []api.Tool{
		makeTool("read_file"),
		makeTool("write_file"),
		makeTool("shell_command"),
		makeTool("analyze_image_content"),
	}

	t.Run("empty allowed returns empty result", func(t *testing.T) {
		got := filterToolsByName(tools, map[string]struct{}{})
		if len(got) != 0 {
			t.Errorf("got %d tools, want 0", len(got))
		}
	})

	t.Run("single allowed tool", func(t *testing.T) {
		allowed := map[string]struct{}{"read_file": {}}
		got := filterToolsByName(tools, allowed)
		if len(got) != 1 {
			t.Fatalf("got %d tools, want 1", len(got))
		}
		if got[0].Function.Name != "read_file" {
			t.Errorf("got %q, want %q", got[0].Function.Name, "read_file")
		}
	})

	t.Run("multiple allowed tools", func(t *testing.T) {
		allowed := map[string]struct{}{"read_file": {}, "shell_command": {}}
		got := filterToolsByName(tools, allowed)
		if len(got) != 2 {
			t.Fatalf("got %d tools, want 2", len(got))
		}
		for _, tool := range got {
			if tool.Function.Name != "read_file" && tool.Function.Name != "shell_command" {
				t.Errorf("unexpected tool %q", tool.Function.Name)
			}
		}
	})

	t.Run("all allowed returns all", func(t *testing.T) {
		allowed := makeAllowedToolSet([]string{"read_file", "write_file", "shell_command", "analyze_image_content"})
		got := filterToolsByName(tools, allowed)
		if len(got) != 4 {
			t.Fatalf("got %d tools, want 4", len(got))
		}
	})

	t.Run("none matching returns empty", func(t *testing.T) {
		allowed := map[string]struct{}{"nonexistent": {}}
		got := filterToolsByName(tools, allowed)
		if len(got) != 0 {
			t.Errorf("got %d tools, want 0", len(got))
		}
	})

	t.Run("empty tools list returns empty", func(t *testing.T) {
		allowed := map[string]struct{}{"read_file": {}}
		got := filterToolsByName([]api.Tool{}, allowed)
		if len(got) != 0 {
			t.Errorf("got %d tools, want 0", len(got))
		}
	})
}

func TestShouldUseDirectMultimodalImageReasoning(t *testing.T) {
	t.Run("nil agent with nil client returns false", func(t *testing.T) {
		a := &Agent{}
		got := a.shouldUseDirectMultimodalImageReasoning([]api.Message{})
		if got {
			t.Error("expected false for nil client")
		}
	})

	t.Run("non-vision client returns false", func(t *testing.T) {
		a := &Agent{client: &visionSupportingClient{supportsVision: false}}
		got := a.shouldUseDirectMultimodalImageReasoning([]api.Message{
			{Role: "user", Content: "hello", Images: []api.ImageData{{Base64: "abc", Type: "image/png"}}},
		})
		if got {
			t.Error("expected false for non-vision client")
		}
	})

	t.Run("vision client with user image returns true", func(t *testing.T) {
		a := &Agent{client: &visionSupportingClient{supportsVision: true}}
		got := a.shouldUseDirectMultimodalImageReasoning([]api.Message{
			{Role: "user", Content: "hello", Images: []api.ImageData{{Base64: "abc", Type: "image/png"}}},
		})
		if !got {
			t.Error("expected true for vision client with user image")
		}
	})

	t.Run("vision client with no images returns false", func(t *testing.T) {
		a := &Agent{client: &visionSupportingClient{supportsVision: true}}
		got := a.shouldUseDirectMultimodalImageReasoning([]api.Message{
			{Role: "user", Content: "hello"},
		})
		if got {
			t.Error("expected false when no images")
		}
	})

	t.Run("assistant message with images returns false", func(t *testing.T) {
		a := &Agent{client: &visionSupportingClient{supportsVision: true}}
		got := a.shouldUseDirectMultimodalImageReasoning([]api.Message{
			{Role: "assistant", Content: "reply", Images: []api.ImageData{{Base64: "abc", Type: "image/png"}}},
		})
		if got {
			t.Error("expected false for assistant message with images")
		}
	})

	t.Run("empty messages returns false", func(t *testing.T) {
		a := &Agent{client: &visionSupportingClient{supportsVision: true}}
		got := a.shouldUseDirectMultimodalImageReasoning([]api.Message{})
		if got {
			t.Error("expected false for empty messages")
		}
	})

	t.Run("user message with empty images returns false", func(t *testing.T) {
		a := &Agent{client: &visionSupportingClient{supportsVision: true}}
		got := a.shouldUseDirectMultimodalImageReasoning([]api.Message{
			{Role: "user", Content: "hello", Images: []api.ImageData{}},
		})
		if got {
			t.Error("expected false for empty images slice")
		}
	})
}

func TestClearConversationHistory(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	a.AddMessage(api.Message{Role: "user", Content: "hello"})
	a.state.SetCurrentIteration(5)
	a.state.SetPreviousSummary("some summary")

	a.ClearConversationHistory()

	msgs := a.GetMessages()
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}

	if a.state.GetCurrentIteration() != 0 {
		t.Errorf("expected iteration 0, got %d", a.state.GetCurrentIteration())
	}

	if a.state.GetPreviousSummary() != "" {
		t.Errorf("expected empty previous summary, got %q", a.state.GetPreviousSummary())
	}
}

func TestSetConversationOptimization(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	// Should not panic even if optimizer is nil
	a.state.SetOptimizer(nil)
	a.SetConversationOptimization(true)

	// With real optimizer
	a2 := newTestAgent(t)
	defer a2.Shutdown()
	a2.SetConversationOptimization(true)
	a2.SetConversationOptimization(false)
}

func TestGetOptimizationStats(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	stats := a.GetOptimizationStats()
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
}

func TestGetOptimizationStatsNilOptimizer(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()
	a.state.SetOptimizer(nil)

	stats := a.GetOptimizationStats()
	if stats == nil {
		t.Fatal("expected non-nil stats even with nil optimizer")
	}
	if stats["enabled"] != false {
		t.Errorf("expected enabled=false, got %v", stats["enabled"])
	}
}

func TestExtractPastedImagePaths(t *testing.T) {
	t.Run("no matches returns nil", func(t *testing.T) {
		got := extractPastedImagePaths("just a normal query")
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("single match", func(t *testing.T) {
		got := extractPastedImagePaths("Pasted image saved to disk: /tmp/image.png\nDescribe it")
		if len(got) != 1 {
			t.Fatalf("got %d paths, want 1", len(got))
		}
		if got[0] != "/tmp/image.png" {
			t.Errorf("got %q, want %q", got[0], "/tmp/image.png")
		}
	})

	t.Run("multiple unique paths", func(t *testing.T) {
		got := extractPastedImagePaths(
			"Pasted image saved to disk: /tmp/a.png\n" +
				"Look at this. Pasted image saved to disk: /tmp/b.jpg\n" +
				"Describe both.")
		if len(got) != 2 {
			t.Fatalf("got %d paths, want 2", len(got))
		}
		if got[0] != "/tmp/a.png" || got[1] != "/tmp/b.jpg" {
			t.Errorf("got %v, want [/tmp/a.png /tmp/b.jpg]", got)
		}
	})

	t.Run("duplicate paths are deduped", func(t *testing.T) {
		got := extractPastedImagePaths(
			"Pasted image saved to disk: /tmp/a.png\n" +
				"Again: Pasted image saved to disk: /tmp/a.png")
		if len(got) != 1 {
			t.Fatalf("got %d paths, want 1 (deduped)", len(got))
		}
	})

	t.Run("path with relative path", func(t *testing.T) {
		got := extractPastedImagePaths("Pasted image saved to disk: ./.sprout/pasted-images/img.png")
		if len(got) != 1 {
			t.Fatalf("got %d paths, want 1", len(got))
		}
		if got[0] != "./.sprout/pasted-images/img.png" {
			t.Errorf("got %q, want %q", got[0], "./.sprout/pasted-images/img.png")
		}
	})
}

func TestAlwaysIncludedTools(t *testing.T) {
	expected := []string{
		"list_skills", "activate_skill", "manage_memory", "TodoWrite", "TodoRead",
	}
	for _, name := range expected {
		found := false
		for _, t := range alwaysIncludedTools {
			if t == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("alwaysIncludedTools missing %q", name)
		}
	}
}

func TestGetCurrentCustomProvider(t *testing.T) {
	t.Run("nil configManager returns false", func(t *testing.T) {
		a := &Agent{}
		_, ok := a.getCurrentCustomProvider()
		if ok {
			t.Error("expected false for nil configManager")
		}
	})

	t.Run("no custom providers returns false", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()
		_, ok := a.getCurrentCustomProvider()
		if ok {
			t.Error("expected false when no custom providers configured")
		}
	})
}

func TestBuildNonVisionImageToolPrompt(t *testing.T) {
	a := &Agent{}
	prompt := a.buildNonVisionImageToolPrompt("what is this?", []string{"/tmp/img.png", "/tmp/img2.jpg"})

	if !strings.Contains(prompt, "OCR Trigger Policy") {
		t.Error("expected OCR trigger policy in prompt")
	}
	if !strings.Contains(prompt, "/tmp/img.png") || !strings.Contains(prompt, "/tmp/img2.jpg") {
		t.Error("expected image paths in prompt")
	}
	if !strings.Contains(prompt, "what is this?") {
		t.Error("expected original query in prompt")
	}
}
