package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// A tool call whose context is cancelled (user stop, turn stall, per-call
// timeout) has already been reported to the model as a failure. The handler
// goroutine keeps running, so without a guard the write lands anyway and the
// model — told the edit failed — applies it again, duplicating content.

func TestEditFile_CancelledContextDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dialect.go")
	original := "package store\n\nfunc a() {\n\treturn\n}\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(filesystem.WithWorkspaceRoot(context.Background(), dir))
	cancel()

	_, err := EditFile(ctx, path, "func a() {\n\treturn\n}", "func b() {\n\treturn\n}")
	if err == nil {
		t.Fatal("expected the cancelled call to fail")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if !strings.Contains(err.Error(), "unchanged") {
		t.Errorf("error must tell the model the file is unchanged, got %q", err)
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Fatalf("cancelled edit must not write; file is now:\n%s", got)
	}
}

func TestEditFile_DeadlineExceededDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dialect.go")
	original := "package store\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(filesystem.WithWorkspaceRoot(context.Background(), dir), 0)
	defer cancel()
	<-ctx.Done()

	_, err := EditFile(ctx, path, "package store", "package other")
	if err == nil {
		t.Fatal("expected the timed-out call to fail")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Fatalf("timed-out edit must not write; file is now:\n%s", got)
	}
}

func TestWriteFile_CancelledContextDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dialect.go")
	original := "package store\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(filesystem.WithWorkspaceRoot(context.Background(), dir))
	cancel()

	_, err := WriteFile(ctx, path, "package other\n")
	if err == nil {
		t.Fatal("expected the cancelled call to fail")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Fatalf("cancelled write must not run; file is now:\n%s", got)
	}
}

// The guard at the write itself: a cancellation that arrives after the
// initial checks must still stop the write, because the caller (seed's tool
// registry) may already have reported a timeout to the model.
func TestWriteFileWithPermissions_CancelledContextDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dialect.go")
	original := "package store\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := writeFileWithPermissions(ctx, path, []byte("package other\n"), 0o644); err == nil {
		t.Fatal("expected the write-site guard to refuse a cancelled call")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Fatalf("write-site guard must not write; file is now:\n%s", got)
	}
}

// A live context still writes — the guard must not change the success path.
func TestEditFile_LiveContextWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dialect.go")
	if err := os.WriteFile(path, []byte("package store\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := filesystem.WithWorkspaceRoot(context.Background(), dir)
	if _, err := EditFile(ctx, path, "package store", "package other"); err != nil {
		t.Fatalf("live-context edit failed: %v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "package other\n" {
		t.Fatalf("edit did not apply; file is:\n%s", got)
	}
}
