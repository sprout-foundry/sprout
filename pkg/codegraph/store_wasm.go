//go:build js

package codegraph

import (
	"context"
	"fmt"
)

// WASM stubs — SQLite is not available in the browser/WASM environment.

type Symbol struct {
	ID            int64
	QualifiedName string
	DisplayName   string
	FilePath      string
	Line          int
	Kind          string
	Language      string
	FileMTime     string
}

type Edge struct {
	SourceQualifiedName string
	TargetQualifiedName string
	EdgeType            string
	Line                int
}

type GraphStats struct {
	NodeCount int
	EdgeCount int
	FileCount int
}

type Store interface {
	IndexFile(ctx context.Context, path string, symbols []Symbol, edges []Edge) error
	QueryCallers(ctx context.Context, qualifiedName string) ([]Symbol, error)
	QueryCallees(ctx context.Context, qualifiedName string) ([]Symbol, error)
	FindDeadCode(ctx context.Context) ([]Symbol, error)
	GetStaleFiles(ctx context.Context) ([]string, error)
	Stats() GraphStats
	Close() error
	BaseDir() string
}

type SQLiteStore struct{}

func NewStore(dbPath string) (*SQLiteStore, error) {
	return nil, fmt.Errorf("codegraph: SQLite store not available on WASM")
}

func DefaultDBPath() (string, error) {
	return "", fmt.Errorf("codegraph: not available on WASM")
}

func errWASM() error { return fmt.Errorf("codegraph: not available on WASM") }

func (s *SQLiteStore) IndexFile(ctx context.Context, path string, symbols []Symbol, edges []Edge) error {
	return errWASM()
}
func (s *SQLiteStore) QueryCallers(ctx context.Context, qualifiedName string) ([]Symbol, error) {
	return nil, errWASM()
}
func (s *SQLiteStore) QueryCallees(ctx context.Context, qualifiedName string) ([]Symbol, error) {
	return nil, errWASM()
}
func (s *SQLiteStore) FindDeadCode(ctx context.Context) ([]Symbol, error) {
	return nil, errWASM()
}
func (s *SQLiteStore) GetStaleFiles(ctx context.Context) ([]string, error) {
	return nil, errWASM()
}
func (s *SQLiteStore) QueryAllNodes(ctx context.Context) ([]Symbol, error) {
	return nil, errWASM()
}
func (s *SQLiteStore) Stats() GraphStats { return GraphStats{} }
func (s *SQLiteStore) Close() error      { return nil }
func (s *SQLiteStore) BaseDir() string   { return "" }

// FileParser is a function that parses a source file at the given path and
// returns extracted symbols and call edges.
type FileParser func(path string, content []byte) ([]Symbol, []Edge, error)

func (s *SQLiteStore) IndexAll(ctx context.Context, parseFile FileParser) error {
	return errWASM()
}

func (s *SQLiteStore) IndexChangedFiles(ctx context.Context, parseFile FileParser) error {
	return errWASM()
}
