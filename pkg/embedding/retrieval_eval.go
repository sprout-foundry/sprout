//go:build ignore

// Retrieval quality evaluation against a focused subset of the codebase.
package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
)

func cosineSim(a, b []float32) float32 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

type queryTest struct {
	name     string
	query    string
	expected []string // substrings to look for in ID/Name/Signature
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Build index ONLY on pkg/embedding (small, fast, no storybook junk)
	rootDir := "pkg/embedding"

	// Use a clean temp directory so old cached embeddings don't interfere
	dir, err := os.MkdirTemp("", "eval-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	evalDir := filepath.Join(dir, "embeddings")

	cfg := &configuration.EmbeddingIndexConfig{
		IndexDir: evalDir,
	}

	mgr := embedding.NewEmbeddingManager(cfg, rootDir)
	if err := mgr.Init(ctx); err != nil {
		log.Fatalf("Init: %v", err)
	}
	defer mgr.Close()

	fmt.Println("Building index on pkg/embedding ...")
	start := time.Now()
	stats, err := mgr.BuildIndex(ctx)
	if err != nil {
		log.Fatalf("Build: %v", err)
	}
	fmt.Printf("Done: %d files, %d units in %s\n\n",
		stats.FilesProcessed, stats.UnitsEmbedded, time.Since(start))

	tests := []queryTest{
		{
			name:     "cosine similarity function",
			query:    "compute cosine similarity between two vectors",
			expected: []string{"cosine", "similarity", "TopK", "similarity.go"},
		},
		{
			name:     "embedding provider interface",
			query:    "interface for producing vector embeddings from text",
			expected: []string{"provider", "embed", "EmbeddingProvider"},
		},
		{
			name:     "SHA-256 model hash",
			query:    "compute SHA-256 hash of embedded model data for change detection",
			expected: []string{"ModelHash", "sha256", "modelHash"},
		},
		{
			name:     "incremental index from git diff",
			query:    "incrementally update embedding index from git diff of changed files",
			expected: []string{"GitDiff", "git", "diff", "incremental", "UpdateFromGit"},
		},
		{
			name:     "file deletion handler",
			query:    "remove embedding records when a source file is deleted",
			expected: []string{"DeleteByFile", "delete", "remove"},
		},
		{
			name:     "HNSW vector store persistence",
			query:    "thread-safe vector store backed by HNSW graph index",
			expected: []string{"HNSWStore", "hnsw", "store"},
		},
		{
			name:     "WordPiece tokenizer",
			query:    "greedy longest-prefix matching tokenizer with subword prefix",
			expected: []string{"tokenize", "token", "WordPiece", "StaticTokenizer"},
		},
		{
			name:     "code extraction from files",
			query:    "extract code units like functions and methods from source files",
			expected: []string{"ExtractFromFile", "CodeUnit", "extract", "function"},
		},
	}

	fmt.Println("========================================")
	fmt.Println("  RETRIEVAL QUALITY EVALUATION")
	fmt.Println("========================================\n")

	hitRate := 0
	for i, test := range tests {
		fmt.Printf("[%d] %s\n", i+1, test.name)
		fmt.Printf("    Query: %s\n", test.query)

		results, err := mgr.QuerySimilar(ctx, test.query, 10, 0.0)
		if err != nil {
			fmt.Printf("    ERROR: %v\n\n", err)
			continue
		}

		if len(results) == 0 {
			fmt.Printf("    ⚠️  No results\n\n")
			continue
		}

		fmt.Printf("    Top 5:\n")
		hit := false
		for j, r := range results {
			if j >= 5 {
				break
			}
			marker := "  "
			recordText := r.Record.ID + " " + r.Record.Name + " " + r.Record.Signature + " " + r.Record.File
			for _, exp := range test.expected {
				if contains(recordText, exp) {
					marker = "▶ "
					hit = true
					break
				}
			}
			fmt.Printf("      %d. %ssim=%.4f  %s\n", j+1, marker, r.Similarity, r.Record.ID)
		}

		if hit {
			fmt.Printf("    ✅ HIT\n")
			hitRate++
		} else {
			fmt.Printf("    ❌ MISS\n")
		}
		fmt.Println()
	}

	pct := float64(hitRate) / float64(len(tests)) * 100
	fmt.Printf("========================================\n")
	fmt.Printf("  Hit Rate: %d/%d (%.0f%%)\n", hitRate, len(tests), pct)
	fmt.Printf("========================================\n")
}

func contains(haystack, needle string) bool {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			c1, c2 := haystack[i+j], needle[j]
			if c1 >= 'A' && c1 <= 'Z' {
				c1 += 'a' - 'A'
			}
			if c2 >= 'A' && c2 <= 'Z' {
				c2 += 'a' - 'A'
			}
			if c1 != c2 {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
