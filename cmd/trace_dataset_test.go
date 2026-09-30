//go:build !js

package cmd

import "testing"

func TestGetTraceDatasetDir_EmptyFlag(t *testing.T) {
	t.Setenv("SPROUT_TRACE_DATASET_DIR", "")

	got := getTraceDatasetDir("")
	if got != "" {
		t.Errorf("getTraceDatasetDir(\"\") = %q, want empty", got)
	}
}

func TestGetTraceDatasetDir_NonEmptyFlag(t *testing.T) {
	t.Setenv("SPROUT_TRACE_DATASET_DIR", "")

	got := getTraceDatasetDir("/tmp/traces")
	if got != "/tmp/traces" {
		t.Errorf("getTraceDatasetDir(\"/tmp/traces\") = %q, want \"/tmp/traces\"", got)
	}
}

func TestGetTraceDatasetDir_EnvVar(t *testing.T) {
	t.Setenv("SPROUT_TRACE_DATASET_DIR", "/env/traces")

	got := getTraceDatasetDir("")
	if got != "/env/traces" {
		t.Errorf("getTraceDatasetDir(\"\") with env set = %q, want \"/env/traces\"", got)
	}
}

func TestGetTraceDatasetDir_EnvVarEmpty(t *testing.T) {
	t.Setenv("SPROUT_TRACE_DATASET_DIR", "")

	got := getTraceDatasetDir("")
	if got != "" {
		t.Errorf("getTraceDatasetDir(\"\") with env empty = %q, want empty", got)
	}
}

func TestGetTraceDatasetDir_FlagTakesPriority(t *testing.T) {
	t.Setenv("SPROUT_TRACE_DATASET_DIR", "/env/traces")

	got := getTraceDatasetDir("/flag/traces")
	if got != "/flag/traces" {
		t.Errorf("getTraceDatasetDir(\"/flag/traces\") with env set = %q, want \"/flag/traces\"", got)
	}
}
