// D1 migration application for the Cloudflare Workers adapter.
//
// A project's D1 schema lives in numbered .sql files under its migrations
// directory (drizzle/migrations for the reference starter). The adapter
// applies the pending ones through the D1 query API before the script goes
// live, so the deployed database has the schema the code expects.
//
// The applied set is tracked in a D1 table (the same pattern wrangler's
// migration runner uses), so a re-run applies only what is new and is
// idempotent. If a migration cannot be applied the deploy fails rather than
// going live against a database missing its tables.

package deploy

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// d1MigrationsTable is the D1 table that records which migrations have been
// applied, matching the bookkeeping wrangler's own runner keeps.
const d1MigrationsTable = "d1_migrations"

// d1Migration is one migration file: its name (the recorded identity) and its
// SQL body.
type d1Migration struct {
	Name string
	SQL  string
}

// readD1Migrations reads the *.sql migration files under dir, sorted by name
// so they apply in order. An absent or empty directory yields no migrations
// (a project may have none), which is not an error.
func readD1Migrations(dir string) ([]d1Migration, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cloudflare: read migrations directory %s: %w", dir, err)
	}
	var migrations []d1Migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".sql") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("cloudflare: read migration %s: %w", e.Name(), err)
		}
		migrations = append(migrations, d1Migration{Name: e.Name(), SQL: string(content)})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Name < migrations[j].Name })
	return migrations, nil
}

// d1QueryResult is one entry of a D1 query response. The API's `result` is an
// array of these (one per statement); the adapter reads the first.
type d1QueryResult struct {
	Results []map[string]any `json:"results"`
}

// applyD1Migrations applies every migration under dir that the database has
// not already recorded, in name order, and returns how many were applied.
//
// The applied set is tracked in the d1_migrations table: the table is created
// if absent, the recorded names are read, and only the unrecorded files run.
// Each migration's SQL and its bookkeeping row are sent in one request, so a
// migration that is applied is recorded in the same round trip and an
// interrupted run re-applies only what was never recorded. A statement the
// database rejects is a hard error naming the migration — the deploy fails
// rather than going live with a partial schema.
//
// A migration file is sent as a single SQL string; the D1 query endpoint runs
// the statements it contains. A file with statements the endpoint rejects is
// reported as a failure naming the migration rather than silently skipped.
func (w *CloudflareWorkers) applyD1Migrations(ctx context.Context, databaseID, dir string) (int, error) {
	migrations, err := readD1Migrations(dir)
	if err != nil {
		return 0, err
	}
	if len(migrations) == 0 {
		return 0, nil
	}

	if err := w.d1Exec(ctx, databaseID, "CREATE TABLE IF NOT EXISTS "+d1MigrationsTable+" (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)"); err != nil {
		return 0, fmt.Errorf("cloudflare: prepare D1 migrations table: %w", err)
	}

	applied, err := w.d1AppliedMigrations(ctx, databaseID)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, m := range migrations {
		if applied[m.Name] {
			continue
		}
		// The migration and its bookkeeping row go in one request, so a
		// migration cannot be applied without being recorded (the D1 query
		// endpoint runs the statements of one request together). A statement
		// the database rejects fails the whole request, so a partial apply
		// cannot be recorded as done.
		record := fmt.Sprintf("INSERT INTO %s (name, applied_at) VALUES (%s, datetime('now'))",
			d1MigrationsTable, sqlStringLiteral(m.Name))
		if err := w.d1Exec(ctx, databaseID, m.SQL+"\n"+record); err != nil {
			return count, fmt.Errorf("cloudflare: apply D1 migration %s: %w", m.Name, err)
		}
		count++
	}
	return count, nil
}

// d1AppliedMigrations returns the set of migration names already recorded in
// the database's d1_migrations table.
func (w *CloudflareWorkers) d1AppliedMigrations(ctx context.Context, databaseID string) (map[string]bool, error) {
	rows, err := w.d1Query(ctx, databaseID, "SELECT name FROM "+d1MigrationsTable)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: read applied D1 migrations: %w", err)
	}
	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		if name, ok := row["name"].(string); ok {
			out[name] = true
		}
	}
	return out, nil
}

// d1Exec runs a statement against the database through the D1 query API.
func (w *CloudflareWorkers) d1Exec(ctx context.Context, databaseID, sql string) error {
	_, err := w.d1Query(ctx, databaseID, sql)
	return err
}

// d1Query posts one SQL statement to the D1 query API and returns the rows it
// produced. The token is passed explicitly to the shared request helper and
// appears only in the Authorization header.
//
// The API's `result` is an array of per-statement result objects; the adapter
// posts one statement, so it reads the first entry. A response with no result
// entries yields no rows.
func (w *CloudflareWorkers) d1Query(ctx context.Context, databaseID, sql string) ([]map[string]any, error) {
	path := fmt.Sprintf("/accounts/%s/d1/database/%s/query",
		url.PathEscape(strings.TrimSpace(w.t.cfg.AccountID)), url.PathEscape(strings.TrimSpace(databaseID)))
	data, err := w.t.doRequest(ctx, w.t.cred.Value(), http.MethodPost, path, map[string]any{"sql": sql})
	if err != nil {
		return nil, err
	}
	var res []d1QueryResult
	if err := decodeCloudflareResult("d1 query", data, &res); err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, nil
	}
	return res[0].Results, nil
}

// sqlStringLiteral quotes s as a SQL string literal (single quotes doubled),
// so a migration file name with a quote cannot break the bookkeeping insert.
func sqlStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
