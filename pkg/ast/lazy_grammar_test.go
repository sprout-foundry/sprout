package ast

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Package init must not decode grammars: each one stays resident once
// loaded, and decoding all of them cost ~435 MB at every process start.
// Measured in a fresh process so other tests' parses don't count.
func TestPackageInitDoesNotLoadGrammars(t *testing.T) {
	if os.Getenv("AST_INIT_HEAP_CHILD") == "1" {
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		fmt.Printf("HEAP_IN_USE=%d\n", m.HeapInuse)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestPackageInitDoesNotLoadGrammars$") //nolint:gosec // G204: re-runs this test binary
	cmd.Env = append(os.Environ(), "AST_INIT_HEAP_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	_, after, ok := strings.Cut(string(out), "HEAP_IN_USE=")
	if !ok {
		t.Fatalf("no heap report:\n%s", out)
	}
	fields := strings.Fields(after)
	if len(fields) == 0 {
		t.Fatalf("empty heap report:\n%s", out)
	}
	heap, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		t.Fatalf("parse heap report %q: %v", fields[0], err)
	}
	if heap > 64<<20 {
		t.Errorf("heap after package init is %d MB; grammars appear to be loaded eagerly", heap>>20)
	}
}
