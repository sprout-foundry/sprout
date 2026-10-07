package health

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/ast"
)

// DuplicateFinder finds pairs of near-identical code units in a project. It is
// the seam that lets the command use whatever similarity index the codebase
// offers while a test injects a fixture: the health check never embeds a
// parallel index of its own, it consumes a finder and turns what it returns
// into findings.
type DuplicateFinder interface {
	// FindDuplicates returns the near-duplicate pairs under root whose
	// similarity is at least threshold (0..1), most similar first. It is a
	// candidate list for review, not a verdict.
	FindDuplicates(ctx context.Context, root string, threshold float64) ([]DuplicateMatch, error)
}

// DuplicateMatch is one near-duplicate pair: two code units and the similarity
// between them. File/Name/Line identify each side; the pair is unordered.
type DuplicateMatch struct {
	// A is the first code unit in the pair.
	A CodeLocation `json:"a"`
	// B is the second code unit in the pair.
	B CodeLocation `json:"b"`
	// Similarity is the 0..1 similarity between the two units.
	Similarity float64 `json:"similarity"`
}

// CodeLocation identifies one extracted code unit.
type CodeLocation struct {
	// File is the workspace-relative path.
	File string `json:"file"`
	// Name is the function or method name.
	Name string `json:"name"`
	// Line is the 1-based start line.
	Line int `json:"line"`
}

// DefaultDuplicateThreshold is the similarity at which two functions are
// reported as near-duplicates. It is deliberately high: a health check that
// flags every superficially similar function is noise, and the finding asks a
// human to look, so only near-identical bodies are worth surfacing.
const DefaultDuplicateThreshold = 0.85

// DefaultMaxUnits bounds how many code units the default finder compares, so a
// health scan on a large tree stays bounded. Units are visited in lexical
// (file, line) order, so the bound is deterministic.
const DefaultMaxUnits = 1500

// minDuplicateTokens is the token count below which a function is too small to
// judge: trivial getters and one-line wrappers are structurally similar to
// many things without being meaningful duplicates.
const minDuplicateTokens = 30

// TextDuplicateFinder is the default DuplicateFinder. It reuses the repo's
// code extraction (the same tree-sitter path the size/complexity scan uses)
// and compares the token-frequency vectors of sibling functions with cosine
// similarity — the same similarity measure, over the existing extraction
// machinery, rather than a new embedded index.
type TextDuplicateFinder struct {
	// MaxUnits bounds the number of compared units. Zero uses DefaultMaxUnits.
	MaxUnits int
}

// FindDuplicates extracts the functions under root and reports every pair whose
// token-frequency cosine similarity is at least threshold. Functions are
// compared across the whole tree, so a duplicated block moved into a second
// file is still found. A single unreadable file is skipped, not fatal.
func (f TextDuplicateFinder) FindDuplicates(ctx context.Context, root string, threshold float64) ([]DuplicateMatch, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("health: duplicate scan root is required")
	}
	if threshold <= 0 {
		threshold = DefaultDuplicateThreshold
	}
	maxUnits := f.MaxUnits
	if maxUnits <= 0 {
		maxUnits = DefaultMaxUnits
	}

	units, err := extractFunctionVectors(root, maxUnits)
	if err != nil {
		return nil, err
	}

	var out []DuplicateMatch
	for i := 0; i < len(units); i++ {
		for j := i + 1; j < len(units); j++ {
			if err := ctx.Err(); err != nil {
				return out, err
			}
			if units[i].file == units[j].file && units[i].name == units[j].name {
				continue
			}
			sim := cosineSimilarity(units[i].vector, units[j].vector)
			if sim < threshold {
				continue
			}
			out = append(out, DuplicateMatch{
				A:          units[i].location(),
				B:          units[j].location(),
				Similarity: sim,
			})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Similarity != out[j].Similarity {
			return out[i].Similarity > out[j].Similarity
		}
		if out[i].A.File != out[j].A.File {
			return out[i].A.File < out[j].A.File
		}
		return out[i].A.Line < out[j].A.Line
	})
	return out, nil
}

// unitVector is one extracted function with its token-frequency vector.
type unitVector struct {
	file   string
	name   string
	line   int
	vector map[string]float64
	norm   float64
}

func (u unitVector) location() CodeLocation {
	return CodeLocation{File: u.file, Name: u.name, Line: u.line}
}

// extractFunctionVectors walks the source tree and returns one unitVector per
// function with enough tokens to compare, in lexical (file, line) order,
// capped at maxUnits.
func extractFunctionVectors(root string, maxUnits int) ([]unitVector, error) {
	paths, err := collectSourceFiles(root, DefaultMaxFiles)
	if err != nil {
		return nil, err
	}

	var units []unitVector
	for _, path := range paths {
		if len(units) >= maxUnits {
			break
		}
		info, statErr := os.Stat(path)
		if statErr != nil || info.IsDir() || info.Size() > maxScanFileBytes {
			continue
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		rel := path
		if r, relErr := filepath.Rel(root, path); relErr == nil {
			rel = filepath.ToSlash(r)
		}
		units = append(units, fileFunctionVectors(rel, path, content)...)
	}

	if len(units) > maxUnits {
		units = units[:maxUnits]
	}
	return units, nil
}

// fileFunctionVectors parses one file and returns a unitVector per function
// whose body carries at least minDuplicateTokens tokens.
func fileFunctionVectors(rel, path string, content []byte) []unitVector {
	result, err := ast.ParseFile(path, content)
	if err != nil {
		return nil
	}
	defer result.Release()

	var out []unitVector
	for _, sym := range ast.ExtractSymbolsWithMaxDepth(result.Root, result.Bound, result.Language, 3) {
		if sym.Kind != "function" && sym.Kind != "method" {
			continue
		}
		body := sym.Body
		if body == "" && sym.StartLine > 0 && sym.EndLine >= sym.StartLine {
			body = linesFrom(content, sym.StartLine, sym.EndLine)
		}
		vec := tokenVector(body, result.Language)
		if len(vec) == 0 {
			continue
		}
		if countTokens(vec) < minDuplicateTokens {
			continue
		}
		out = append(out, unitVector{
			file:   rel,
			name:   sym.Name,
			line:   sym.StartLine,
			vector: vec,
			norm:   vectorNorm(vec),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].line < out[j].line })
	return out
}

// tokenVector returns the term-frequency vector of a function body: each
// identifier/keyword token mapped to its count. Comments and string literals
// are stripped first (reusing the complexity proxy's lexical machinery) so
// prose does not create spurious matches.
func tokenVector(body, lang string) map[string]float64 {
	tokens := tokenize(stripCommentsAndStrings(body, lang))
	vec := make(map[string]float64)
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		vec[tok]++
	}
	return vec
}

// countTokens returns the total token count (the sum of the term frequencies).
func countTokens(vec map[string]float64) int {
	total := 0
	for _, c := range vec {
		total += int(c)
	}
	return total
}

// vectorNorm returns the Euclidean norm of a term-frequency vector.
func vectorNorm(vec map[string]float64) float64 {
	var sum float64
	for _, c := range vec {
		sum += c * c
	}
	return math.Sqrt(sum)
}

// cosineSimilarity returns the cosine similarity of two term-frequency
// vectors, in [0, 1] for non-negative counts. A zero-magnitude vector (an
// empty body) is treated as no similarity.
func cosineSimilarity(a, b map[string]float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	// Iterate the smaller map for the dot product.
	if len(a) > len(b) {
		a, b = b, a
	}
	var dot float64
	for tok, ca := range a {
		if cb, ok := b[tok]; ok {
			dot += ca * cb
		}
	}
	if dot == 0 {
		return 0
	}
	na, nb := vectorNorm(a), vectorNorm(b)
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (na * nb)
}

// duplicateFindings reports every near-duplicate pair with a proposed
// extraction of a shared helper — the fix a human approves for one pair
// without touching any other.
func duplicateFindings(matches []DuplicateMatch) []Finding {
	var out []Finding
	for _, m := range matches {
		target := JoinTarget(m.A.File, m.A.Name)
		message := fmt.Sprintf("%s:%s and %s:%s are %.0f%% similar (%s:%d, %s:%d)",
			m.A.File, m.A.Name, m.B.File, m.B.Name, m.Similarity*100,
			m.A.File, m.A.Line, m.B.File, m.B.Line)
		out = append(out, Finding{
			Kind:     KindDuplication,
			Severity: SeverityWarn,
			Target:   target,
			Message:  message,
			Fix: &Fix{
				Summary: fmt.Sprintf("extract the shared block from %s:%s and %s:%s into one helper",
					m.A.File, m.A.Name, m.B.File, m.B.Name),
				Detail: "The two bodies are near-identical. Decide which is canonical, move the common logic into a single helper (in the package both can import), and have each call site delegate to it. If they are genuinely different, add a comment saying why the duplication is intentional.",
			},
		})
	}
	return out
}
