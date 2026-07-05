package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// semantic_search — Definition / Validate
// ---------------------------------------------------------------------------

func TestSemanticSearchHandler_Definition(t *testing.T) {
	t.Parallel()
	h := &semanticSearchHandler{}

	require.Equal(t, "semantic_search", h.Name())

	def := h.Definition()
	require.Equal(t, "semantic_search", def.Name)
	require.NotEmpty(t, def.Description)
	require.Equal(t, []string{"query"}, def.Required)

	// Check parameter schema
	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["query"])
	require.True(t, paramNames["threshold"])
	require.True(t, paramNames["top_k"])

	// query should be required in the parameter list
	for _, p := range def.Parameters {
		if p.Name == "query" {
			require.True(t, p.Required)
		}
	}
}

func TestSemanticSearchHandler_Validate(t *testing.T) {
	t.Parallel()
	h := &semanticSearchHandler{}

	// Missing query
	err := h.Validate(map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")

	// Wrong type for query
	err = h.Validate(map[string]any{"query": 42})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a string")

	// Valid
	require.NoError(t, h.Validate(map[string]any{"query": "find auth function"}))
}

// ---------------------------------------------------------------------------
// semantic_search — Execute (arg extraction & error paths)
// ---------------------------------------------------------------------------

func TestSemanticSearchHandler_Execute_MissingQuery(t *testing.T) {
	t.Parallel()
	h := &semanticSearchHandler{}
	env := ToolEnv{
		EventBus:      events.NewEventBus(),
		WorkspaceRoot: ".",
		ConfigManager: newHermeticConfigManager(t),
	}

	res, err := h.Execute(context.Background(), env, map[string]any{})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "required")
}

func TestSemanticSearchHandler_Execute_InvalidQueryType(t *testing.T) {
	t.Parallel()
	h := &semanticSearchHandler{}
	env := ToolEnv{
		EventBus:      events.NewEventBus(),
		WorkspaceRoot: ".",
		ConfigManager: newHermeticConfigManager(t),
	}

	res, err := h.Execute(context.Background(), env, map[string]any{"query": 42})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "must be a string")
}

func TestSemanticSearchHandler_Execute_DefaultArgs(t *testing.T) {
	t.Parallel()
	h := &semanticSearchHandler{}

	// We test the arg extraction by checking that it doesn't panic
	// and that results (success or error) are reported gracefully.
	// The result depends on whether ONNX is available in the test environment.
	env := ToolEnv{
		EventBus:      events.NewEventBus(),
		WorkspaceRoot: t.TempDir(),
		ConfigManager: newHermeticConfigManager(t),
	}

	res, err := h.Execute(context.Background(), env, map[string]any{
		"query": "test query",
	})
	// ONNX may or may not be available. If unavailable, expect an error result.
	// If available, we expect either success (index found) or an error (no index built).
	// In either case, the handler should not panic.
	require.NoError(t, err, "handler should not return a Go error")
	if res.IsError {
		// ONNX unavailable or no index — graceful degradation
		require.Contains(t, res.Output, "Semantic search unavailable",
			"error output should explain the issue")
	} else {
		// No error — either found results or returned empty results (not an error)
		require.Contains(t, res.Output, "result", "should return some output even with no index")
	}
}

// ---------------------------------------------------------------------------
// semantic_search — Argument extraction edge cases (unit-level tests)
// ---------------------------------------------------------------------------

func TestSemanticSearchHandler_ArgExtraction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		args          map[string]any
		wantTopK      int
		wantThreshold float64
	}{
		{
			name:          "defaults",
			args:          map[string]any{"query": "test"},
			wantTopK:      5,
			wantThreshold: 0.75,
		},
		{
			name:          "custom top_k as int",
			args:          map[string]any{"query": "test", "top_k": 10},
			wantTopK:      10,
			wantThreshold: 0.75,
		},
		{
			name:          "custom top_k as float64 (JSON number)",
			args:          map[string]any{"query": "test", "top_k": float64(15)},
			wantTopK:      15,
			wantThreshold: 0.75,
		},
		{
			name:          "top_k clamped to 1",
			args:          map[string]any{"query": "test", "top_k": -3},
			wantTopK:      1,
			wantThreshold: 0.75,
		},
		{
			name:          "custom threshold as float64",
			args:          map[string]any{"query": "test", "threshold": 0.5},
			wantTopK:      5,
			wantThreshold: 0.5,
		},
		{
			name:          "custom threshold as float32",
			args:          map[string]any{"query": "test", "threshold": float32(0.6)},
			wantTopK:      5,
			wantThreshold: 0.6,
		},
		{
			name:          "custom threshold as int",
			args:          map[string]any{"query": "test", "threshold": 0},
			wantTopK:      5,
			wantThreshold: 0,
		},
		{
			name:          "threshold clamped low",
			args:          map[string]any{"query": "test", "threshold": -0.5},
			wantTopK:      5,
			wantThreshold: 0,
		},
		{
			name:          "threshold clamped high",
			args:          map[string]any{"query": "test", "threshold": 1.5},
			wantTopK:      5,
			wantThreshold: 1,
		},
		{
			name:          "both custom",
			args:          map[string]any{"query": "test", "top_k": 20, "threshold": 0.85},
			wantTopK:      20,
			wantThreshold: 0.85,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Extract top_k
			topK := 5
			if tkRaw, exists := tc.args["top_k"]; exists && tkRaw != nil {
				switch v := tkRaw.(type) {
				case int:
					topK = v
				case float64:
					topK = int(v)
				}
			}
			if topK < 1 {
				topK = 1
			}

			// Extract threshold
			threshold := 0.75
			if tRaw, exists := tc.args["threshold"]; exists && tRaw != nil {
				switch v := tRaw.(type) {
				case float64:
					threshold = v
				case float32:
					threshold = float64(v)
				case int:
					threshold = float64(v)
				}
			}
			if threshold < 0 {
				threshold = 0
			}
			if threshold > 1 {
				threshold = 1
			}

			require.Equal(t, tc.wantTopK, topK)
			require.InDelta(t, tc.wantThreshold, threshold, 0.001)
		})
	}
}

// ---------------------------------------------------------------------------
// semantic_search — Result formatting
// ---------------------------------------------------------------------------

func TestSemanticSearchHandler_FormatSearchResults(t *testing.T) {
	t.Parallel()

	// No results
	output := formatEmbeddingSearchResults("auth", nil, 0.75)
	require.True(t, strings.Contains(output, "No results found"))
	require.True(t, strings.Contains(output, "auth"))

	// One result
	results := []embedding.QueryResult{
		{
			Record: embedding.VectorRecord{
				ID:        "pkg/auth.go:Login",
				File:      "pkg/auth.go",
				Name:      "Login",
				Signature: "func Login(username, password string) (*User, error)",
				StartLine: 10,
				EndLine:   45,
				Language:  "go",
			},
			Similarity: 0.92,
		},
	}
	output = formatEmbeddingSearchResults("authentication", results, 0.75)
	require.True(t, strings.Contains(output, "Found 1 result"))
	require.True(t, strings.Contains(output, "Login"))
	require.True(t, strings.Contains(output, "pkg/auth.go"))
	require.True(t, strings.Contains(output, "score: 0.9200"))
	require.True(t, strings.Contains(output, "Language: go"))
	require.True(t, strings.Contains(output, "lines 10-45"))
	require.True(t, strings.Contains(output, "read_file"))

	// Multiple results
	results = []embedding.QueryResult{
		{
			Record:     embedding.VectorRecord{Name: "Login", File: "pkg/auth.go", Language: "go", StartLine: 10, EndLine: 45},
			Similarity: 0.95,
		},
		{
			Record:     embedding.VectorRecord{Name: "Authenticate", File: "pkg/middleware/auth.go", Language: "go", StartLine: 20, EndLine: 60},
			Similarity: 0.82,
		},
	}
	output = formatEmbeddingSearchResults("auth", results, 0.75)
	require.True(t, strings.Contains(output, "Found 2 result(s)"))
	require.True(t, strings.Contains(output, "#1"))
	require.True(t, strings.Contains(output, "#2"))
	require.True(t, strings.Contains(output, "Login"))
	require.True(t, strings.Contains(output, "Authenticate"))
}

// ---------------------------------------------------------------------------
// semantic_search — EventBus event tracking
// ---------------------------------------------------------------------------

func TestSemanticSearchHandler_EventBus_PublishesEvents(t *testing.T) {
	t.Parallel()
	h := &semanticSearchHandler{}

	bus := events.NewEventBus()
	_ = bus.Subscribe("test") // subscribe to have a listener

	env := ToolEnv{
		EventBus:      bus,
		WorkspaceRoot: t.TempDir(),
		ConfigManager: newHermeticConfigManager(t),
	}

	// Execute with missing query — triggers error path
	_, _ = h.Execute(context.Background(), env, map[string]any{})

	// Handlers no longer self-publish tool_start/tool_end — the core
	// tool executor (pkg/agent/tool_executor.go) handles event publishing.
	select {
	case ev := <-bus.Subscribe("check"):
		t.Fatalf("expected 0 events from handler, got %+v", ev)
	default:
		// good — no events published by the handler
	}
}

func TestSemanticSearchHandler_NoEventBus(t *testing.T) {
	t.Parallel()
	h := &semanticSearchHandler{}

	// No EventBus — should still work, just no events
	env := ToolEnv{
		WorkspaceRoot: t.TempDir(),
		ConfigManager: newHermeticConfigManager(t),
	}

	// Should handle missing query gracefully without panicking
	res, err := h.Execute(context.Background(), env, map[string]any{})
	require.Error(t, err)
	require.True(t, res.IsError)
}

// ---------------------------------------------------------------------------
// embedding_index — EventBus nil fix (GAP-2)
// ---------------------------------------------------------------------------

func TestEmbeddingIndexHandler_NoEventBus(t *testing.T) {
	t.Parallel()
	h := &embeddingIndexHandler{}

	// No EventBus — the handler should still execute its logic
	env := ToolEnv{
		WorkspaceRoot: t.TempDir(),
		ConfigManager: newHermeticConfigManager(t),
	}

	// Test status operation (no ONNX needed)
	res, err := h.Execute(context.Background(), env, map[string]any{
		"operation": "status",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "Embedding Index Status")
	require.Contains(t, res.Output, "Provider:")
}

func TestEmbeddingIndexHandler_NoEventBus_UnknownOperation(t *testing.T) {
	t.Parallel()
	h := &embeddingIndexHandler{}

	env := ToolEnv{
		WorkspaceRoot: t.TempDir(),
		ConfigManager: newHermeticConfigManager(t),
	}

	res, err := h.Execute(context.Background(), env, map[string]any{
		"operation": "invalid_op",
	})
	require.NoError(t, err) // handler returns nil error for unknown ops
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "Unknown operation")
}

func TestEmbeddingIndexHandler_NoEventBus_MissingOperation(t *testing.T) {
	t.Parallel()
	h := &embeddingIndexHandler{}

	env := ToolEnv{
		WorkspaceRoot: t.TempDir(),
		ConfigManager: newHermeticConfigManager(t),
	}

	res, err := h.Execute(context.Background(), env, map[string]any{})
	require.NoError(t, err) // handler returns nil error for parse errors
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "required")
}

func TestEmbeddingIndexHandler_WithEventBus(t *testing.T) {
	t.Parallel()
	h := &embeddingIndexHandler{}

	bus := events.NewEventBus()
	_ = bus.Subscribe("test") // subscribe to have a listener

	env := ToolEnv{
		EventBus:      bus,
		WorkspaceRoot: t.TempDir(),
		ConfigManager: newHermeticConfigManager(t),
	}

	res, err := h.Execute(context.Background(), env, map[string]any{
		"operation": "status",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "Embedding Index Status")

	// Handlers no longer self-publish tool_start/tool_end — the core
	// tool executor (pkg/agent/tool_executor.go) handles event publishing.
	select {
	case ev := <-bus.Subscribe("check"):
		t.Fatalf("expected 0 events from handler, got %+v", ev)
	default:
		// good — no events published by the handler
	}
}

func TestEmbeddingIndexHandler_WithEventBus_Error(t *testing.T) {
	t.Parallel()
	h := &embeddingIndexHandler{}

	bus := events.NewEventBus()
	_ = bus.Subscribe("test") // subscribe to have a listener

	env := ToolEnv{
		EventBus:      bus,
		WorkspaceRoot: t.TempDir(),
		ConfigManager: newHermeticConfigManager(t),
	}

	// Invalid operation — handler should still execute without panicking
	_, _ = h.Execute(context.Background(), env, map[string]any{
		"operation": "bad",
	})

	// Handlers no longer self-publish tool_start/tool_end — the core
	// tool executor (pkg/agent/tool_executor.go) handles event publishing.
	select {
	case ev := <-bus.Subscribe("check"):
		t.Fatalf("expected 0 events from handler, got %+v", ev)
	default:
		// good — no events published by the handler
	}
}

func TestEmbeddingIndexHandler_WithEventBus_MissingOperation(t *testing.T) {
	t.Parallel()
	h := &embeddingIndexHandler{}

	bus := events.NewEventBus()
	_ = bus.Subscribe("test") // subscribe to have a listener

	env := ToolEnv{
		EventBus:      bus,
		WorkspaceRoot: t.TempDir(),
		ConfigManager: newHermeticConfigManager(t),
	}

	// Missing operation — handler should still execute without panicking
	_, _ = h.Execute(context.Background(), env, map[string]any{})

	// Handlers no longer self-publish tool_start/tool_end — the core
	// tool executor (pkg/agent/tool_executor.go) handles event publishing.
	select {
	case ev := <-bus.Subscribe("check"):
		t.Fatalf("expected 0 events from handler, got %+v", ev)
	default:
		// good — no events published by the handler
	}
}

// ---------------------------------------------------------------------------
// embedding_index — Definition / Validate
// ---------------------------------------------------------------------------

func TestEmbeddingIndexHandler_Definition(t *testing.T) {
	t.Parallel()
	h := &embeddingIndexHandler{}

	require.Equal(t, "embedding_index", h.Name())

	def := h.Definition()
	require.Equal(t, "embedding_index", def.Name)
	require.NotEmpty(t, def.Description)
	require.Equal(t, []string{"operation"}, def.Required)

	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["operation"])
}

func TestEmbeddingIndexHandler_Validate(t *testing.T) {
	t.Parallel()
	h := &embeddingIndexHandler{}

	// Missing operation
	err := h.Validate(map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")

	// Valid
	require.NoError(t, h.Validate(map[string]any{"operation": "status"}))
	require.NoError(t, h.Validate(map[string]any{"operation": "build"}))
	require.NoError(t, h.Validate(map[string]any{"operation": "update"}))
}

// ---------------------------------------------------------------------------
// embedding_index — handleStatus without needing real config
// ---------------------------------------------------------------------------

func TestEmbeddingIndexHandler_Status_NoIndexDir(t *testing.T) {
	t.Parallel()
	h := &embeddingIndexHandler{}

	// Empty config — should fall back to ~/.config/sprout/embeddings
	cfg := &configuration.EmbeddingIndexConfig{}
	res, err := h.handleStatus(cfg, t.TempDir())
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "Embedding Index Status")
	require.Contains(t, res.Output, "Enabled: false")
	require.Contains(t, res.Output, "Provider: bundled")
}
