//go:build !js

package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/codegraph"
)

// codeIndexRefreshEvery is how long after one index run a checkout may be
// re-indexed (changed files only) when StartCodeIndex is called again. A
// refresh only stats the indexed files and re-parses the changed ones.
const codeIndexRefreshEvery = 30 * time.Second

// codeIndexTimeout bounds one index run. A first build of a large repository
// takes a few minutes; one that has not finished by then is dropped.
const codeIndexTimeout = 10 * time.Minute

// codeIndexStaleLock is how old another process's build lock must be before
// it is taken as abandoned (the process died mid-build).
const codeIndexStaleLock = codeIndexTimeout + 5*time.Minute

var (
	codeIndexMu   sync.Mutex
	codeIndexRuns = map[string]*codeIndexRun{} // by checkout root
)

type codeIndexRun struct {
	running  bool
	finished time.Time
}

// StartCodeIndex builds or refreshes, in the background, the code index of
// the git checkout holding dir, so repo_map and the code graph tools answer
// from it instead of parsing files. A first build indexes every source file;
// later runs only the files changed since. It returns at once and does
// nothing outside a git checkout, under go test, while a run for the same
// checkout is in progress here or in another process, or within
// codeIndexRefreshEvery of the last one.
func StartCodeIndex(dir string) {
	if dir == "" || testing.Testing() {
		return
	}
	go func() {
		root, dbPath, err := codegraph.DBPathFor(dir)
		if err != nil {
			return
		}
		codeIndexMu.Lock()
		run := codeIndexRuns[root]
		if run == nil {
			run = &codeIndexRun{}
			codeIndexRuns[root] = run
		}
		if run.running || (!run.finished.IsZero() && time.Since(run.finished) < codeIndexRefreshEvery) {
			codeIndexMu.Unlock()
			return
		}
		run.running = true
		codeIndexMu.Unlock()
		defer func() {
			codeIndexMu.Lock()
			run.running = false
			run.finished = time.Now()
			codeIndexMu.Unlock()
		}()

		if err := indexCheckout(root, dbPath); err != nil {
			fmt.Fprintf(os.Stderr, "code index of %s: %v\n", root, err)
		}
	}()
}

// errCodeIndexBusy reports that another process is indexing the checkout.
var errCodeIndexBusy = errors.New("another process is indexing it")

// indexCheckout runs one index pass over the checkout at root, holding a lock
// file beside the database so two processes never write it at once (SQLite
// would refuse one of them, and a failed first build removes the database).
func indexCheckout(root, dbPath string) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}
	lock := dbPath + ".lock"
	if err := takeLock(lock); err != nil {
		if errors.Is(err, errCodeIndexBusy) {
			return nil
		}
		return err
	}
	defer func() { _ = os.Remove(lock) }()

	store, err := codegraph.CreateForDir(root)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), codeIndexTimeout)
	defer cancel()
	if store.Stats().FileCount > 0 {
		return store.IndexChangedFiles(ctx, codegraphFileParser)
	}
	if err := store.IndexAll(ctx, codegraphFileParser); err != nil {
		// A partial first build would read as a complete one; start over
		// next time instead.
		_ = store.Close()
		for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
			_ = os.Remove(p)
		}
		return err
	}
	return nil
}

// takeLock creates the lock file, or reports errCodeIndexBusy when another
// process holds it. A lock older than codeIndexStaleLock is abandoned and
// taken over.
func takeLock(path string) error {
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			return f.Close()
		}
		if !os.IsExist(err) {
			return err
		}
		info, statErr := os.Stat(path)
		if statErr != nil || time.Since(info.ModTime()) < codeIndexStaleLock {
			return errCodeIndexBusy
		}
		_ = os.Remove(path)
	}
	return errCodeIndexBusy
}
