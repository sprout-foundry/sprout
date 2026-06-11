//go:build !js

package cmd

import (
	"os"
	"testing"
)

func TestStripMallocDebugEnv_Unsets(t *testing.T) {
	t.Setenv("SPROUT_KEEP_MALLOC_DEBUG", "")
	t.Setenv("MallocStackLogging", "1")
	t.Setenv("MallocStackLoggingNoCompact", "1")

	stripMallocDebugEnv()

	for _, k := range []string{"MallocStackLogging", "MallocStackLoggingNoCompact"} {
		if _, ok := os.LookupEnv(k); ok {
			t.Errorf("%s should have been unset", k)
		}
	}
}

func TestStripMallocDebugEnv_OptOut(t *testing.T) {
	t.Setenv("SPROUT_KEEP_MALLOC_DEBUG", "1")
	t.Setenv("MallocStackLogging", "1")

	stripMallocDebugEnv()

	if v, ok := os.LookupEnv("MallocStackLogging"); !ok || v != "1" {
		t.Error("MallocStackLogging must be preserved when SPROUT_KEEP_MALLOC_DEBUG=1")
	}
}
