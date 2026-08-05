package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// The repo map and the embedding index describe the same files to the same
// agent: the map is what it reads to decide which code to open, the index is
// what semantic search retrieves. When they disagree about what a file
// contains, the agent is told a symbol does not exist and then finds it by
// search — or worse, never finds it.
//
// They had drifted. repo_map read ASTResult.Symbols (top-level children only)
// while the embedding extractors call ast.ExtractSymbols (nested scopes too),
// so every method inside a class was indexed but absent from the map.
//
// This pins the property rather than the implementation: every function or
// method the embedding index creates a record for must appear in the repo map
// at the same line. repo_map may legitimately report MORE (types, variables,
// consts) — it is a map, not an embedding corpus.
func TestRepoMapCoversEverySymbolTheIndexEmbeds(t *testing.T) {
	cases := []struct {
		name string
		file string
		src  string
	}{
		{
			name: "python methods inside classes",
			file: "sample.py",
			src: `class PaymentProcessor:
    def charge(self, amount):
        return amount * 2

    def refund(self, amount):
        return -amount

def standalone(x):
    return x
`,
		},
		{
			name: "typescript class methods",
			file: "sample.ts",
			src: `export class UserService {
  async findById(id: string): Promise<string> {
    return id;
  }
  async deleteUser(id: string): Promise<void> {
    return;
  }
}

export function topLevel(a: number): number {
  return a + 1;
}
`,
		},
		{
			name: "go functions and methods",
			file: "sample.go",
			src: `package sample

type Server struct{}

func (s *Server) Start(addr string) error {
	return nil
}

func (s *Server) Stop() error {
	return nil
}

func NewServer() *Server {
	return &Server{}
}
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, tc.file)
			if err := os.WriteFile(path, []byte(tc.src), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}

			units, err := embedding.ExtractFromFile(path, embedding.WithIncludeTests(true))
			if err != nil {
				t.Skipf("embedding extractor unavailable for %s: %v", tc.file, err)
			}
			if len(units) == 0 {
				t.Skipf("embedding extractor produced no units for %s", tc.file)
			}

			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			entries, err := extractSymbolsForFile(path, filepath.Ext(tc.file), content)
			if err != nil {
				t.Fatalf("repo_map extraction: %v", err)
			}

			mapLines := make(map[int][]string)
			for _, e := range entries {
				mapLines[e.Line] = append(mapLines[e.Line], e.Name)
			}

			for _, u := range units {
				names, ok := mapLines[u.StartLine]
				if !ok {
					t.Errorf("embedding indexes %q at line %d, but the repo map has no symbol on that line.\n"+
						"repo map produced: %v",
						u.Name, u.StartLine, entries)
					continue
				}
				// The bare identifier should be recognisable in the map entry,
				// allowing for scope qualification and kind prefixes.
				bare := u.Name
				if i := strings.LastIndex(bare, "."); i >= 0 {
					bare = bare[i+1:]
				}
				bare = strings.TrimSuffix(strings.TrimPrefix(bare, "("), ")")
				found := false
				for _, n := range names {
					if strings.Contains(n, bare) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("embedding indexes %q at line %d; repo map has %v on that line but none names it",
						u.Name, u.StartLine, names)
				}
			}
		})
	}
}
