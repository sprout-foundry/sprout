package deploy

// Tests for the D1 migration runner, the deploy resource state file, and the
// wrangler.toml database_id write-back.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// D1 migrations
// ---------------------------------------------------------------------------

func TestCloudflareWorkers_AppliesPendingMigrations(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "web-app-data")
	target := newWorkersTarget(t, f, srv, cfSentinelToken)
	root := writeWrangler(t, t.TempDir(), webAppDataWrangler)
	writeMigration(t, root, "0000_init.sql", "CREATE TABLE items (id integer primary key, name text not null);")
	buildDir := writeBuildOutput(t, map[string]string{"worker.js": "export default {}"})

	_, err := target.Deploy(DeployRequest{Project: "web-app-data", BuildDir: buildDir, Root: root})
	require.NoError(t, err)

	// The migration SQL ran, and the bookkeeping row was recorded.
	assert.Contains(t, strings.Join(f.d1Statements, "\n"), "CREATE TABLE items")
	assert.Contains(t, strings.Join(f.d1Statements, "\n"), "INSERT INTO d1_migrations")

	// A rerun applies nothing new: the migration name is already recorded.
	before := len(f.d1Statements)
	_, err = target.Deploy(DeployRequest{Project: "web-app-data", BuildDir: buildDir, Root: root})
	require.NoError(t, err)
	newStatements := f.d1Statements[before:]
	joined := strings.Join(newStatements, "\n")
	assert.NotContains(t, joined, "CREATE TABLE items", "an applied migration must not run again")
	assert.NotContains(t, joined, "INSERT INTO d1_migrations", "no duplicate bookkeeping row")
}

func TestReadD1Migrations_SortedAndFiltered(t *testing.T) {
	root := t.TempDir()
	writeMigration(t, root, "0001_second.sql", "SELECT 2;")
	writeMigration(t, root, "0000_first.sql", "SELECT 1;")
	require.NoError(t, os.WriteFile(filepath.Join(root, "drizzle", "migrations", "README.md"), []byte("x"), 0o644))

	migrations, err := readD1Migrations(filepath.Join(root, "drizzle", "migrations"))
	require.NoError(t, err)
	require.Len(t, migrations, 2)
	assert.Equal(t, "0000_first.sql", migrations[0].Name)
	assert.Equal(t, "0001_second.sql", migrations[1].Name)
}

func TestReadD1Migrations_MissingDirIsEmpty(t *testing.T) {
	migrations, err := readD1Migrations(filepath.Join(t.TempDir(), "nope"))
	require.NoError(t, err)
	assert.Empty(t, migrations)
}

// TestCloudflareWorkers_MigrationAndBookkeepingShareOneRequest pins that a
// migration's SQL and its d1_migrations row are sent in one request, so a
// migration cannot be applied without being recorded.
func TestCloudflareWorkers_MigrationAndBookkeepingShareOneRequest(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "web-app-data")
	target := newWorkersTarget(t, f, srv, cfSentinelToken)
	root := writeWrangler(t, t.TempDir(), webAppDataWrangler)
	writeMigration(t, root, "0000_init.sql", "CREATE TABLE items (id integer primary key);")
	buildDir := writeBuildOutput(t, map[string]string{"worker.js": "export default {}"})

	_, err := target.Deploy(DeployRequest{Project: "web-app-data", BuildDir: buildDir, Root: root})
	require.NoError(t, err)

	combined := false
	for _, stmt := range f.d1Statements {
		if strings.Contains(stmt, "CREATE TABLE items") && strings.Contains(stmt, "INSERT INTO d1_migrations") {
			combined = true
		}
	}
	assert.True(t, combined, "the migration SQL and its bookkeeping row must be one request")
}

// TestCloudflareWorkers_D1QueryReadsArrayResult pins the D1 query response
// shape: the API returns `result` as an array of per-statement result objects,
// and the adapter reads the first entry's rows.
func TestCloudflareWorkers_D1QueryReadsArrayResult(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "web-app-data")
	target := newWorkersTarget(t, f, srv, cfSentinelToken)

	rows, err := target.d1Query(deployContext(), "d1-1", "SELECT name FROM d1_migrations")
	require.NoError(t, err)
	assert.Empty(t, rows)

	// Record a migration name in the fake, then read it back through the
	// array-shaped response.
	f.mu.Lock()
	f.d1Rows["d1-1"] = map[string]bool{"0000_init.sql": true}
	f.mu.Unlock()

	rows, err = target.d1Query(deployContext(), "d1-1", "SELECT name FROM d1_migrations")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "0000_init.sql", rows[0]["name"])
}

// ---------------------------------------------------------------------------
// KV and R2 (only when declared)
// ---------------------------------------------------------------------------

func TestCloudflareWorkers_CreatesDeclaredKVAndR2(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-api")
	target := newWorkersTarget(t, f, srv, cfSentinelToken)
	root := writeWrangler(t, t.TempDir(), `name = "my-api"
main = "src/worker/index.ts"

[[kv_namespaces]]
binding = "KV"
id = "REPLACE_WITH_KV_NAMESPACE_ID"

[[r2_buckets]]
binding = "BUCKET"
bucket_name = "my-api-bucket"
`)
	buildDir := writeBuildOutput(t, map[string]string{"worker.js": "export default {}"})

	_, err := target.Deploy(DeployRequest{Project: "my-api", BuildDir: buildDir, Root: root})
	require.NoError(t, err)

	assert.Len(t, f.kv, 1)
	assert.True(t, f.r2["my-api-bucket"])

	uploads := workerUploadRequests(f)
	require.Len(t, uploads, 1)
	assert.Contains(t, uploads[0].Body, `"type":"kv_namespace"`)
	assert.Contains(t, uploads[0].Body, `"name":"KV"`)
	assert.Contains(t, uploads[0].Body, `"type":"r2_bucket"`)
	assert.Contains(t, uploads[0].Body, `"bucket_name":"my-api-bucket"`)
}

func TestCloudflareWorkers_NoBindingsDeclaredUploadsBareScript(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "my-api")
	target := newWorkersTarget(t, f, srv, cfSentinelToken)
	root := writeWrangler(t, t.TempDir(), minimalWrangler)
	buildDir := writeBuildOutput(t, map[string]string{"worker.js": "export default {}"})

	_, err := target.Deploy(DeployRequest{Project: "my-api", BuildDir: buildDir, Root: root})
	require.NoError(t, err)
	assert.Empty(t, f.d1)
	assert.Empty(t, f.kv)
	assert.Empty(t, f.r2)
}

// ---------------------------------------------------------------------------
// Token hygiene across the binding path
// ---------------------------------------------------------------------------

func TestCloudflareWorkers_BindingPathNeverLeaksToken(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "web-app-data")
	target := newWorkersTarget(t, f, srv, cfSentinelToken)
	root := writeWrangler(t, t.TempDir(), webAppDataWrangler)
	writeMigration(t, root, "0000_init.sql", "CREATE TABLE items (id integer primary key);")
	buildDir := writeBuildOutput(t, map[string]string{"worker.js": "export default {}"})

	_, err := target.Deploy(DeployRequest{Project: "web-app-data", BuildDir: buildDir, Root: root})
	require.NoError(t, err)

	sawAuth := false
	for _, r := range f.requestsSnapshot() {
		assert.NotContains(t, r.Body, cfSentinelToken, "token must not appear in a request body")
		assert.NotContains(t, r.Path, cfSentinelToken, "token must not appear in a request path")
		if strings.Contains(r.Auth, cfSentinelToken) {
			sawAuth = true
		}
	}
	assert.True(t, sawAuth, "the token is sent in the Authorization header")
	assert.NotContains(t, readWrangler(t, root), cfSentinelToken, "the token must never be written to wrangler.toml")
}

// ---------------------------------------------------------------------------
// Resource state file
// ---------------------------------------------------------------------------

func TestDeployResourceState_RoundTrip(t *testing.T) {
	root := t.TempDir()
	state := emptyDeployResourceState()
	state.D1["my-api-d1-DB"] = "d1-1"
	state.KV["my-api-kv-KV"] = "kv-1"
	state.R2["my-api-r2-BUCKET"] = "my-api-bucket"
	require.NoError(t, state.save(root))

	loaded, err := LoadDeployResourceState(root)
	require.NoError(t, err)
	assert.Equal(t, "d1-1", loaded.D1["my-api-d1-DB"])
	assert.Equal(t, "kv-1", loaded.KV["my-api-kv-KV"])
	assert.Equal(t, "my-api-bucket", loaded.R2["my-api-r2-BUCKET"])
}

func TestLoadDeployResourceState_MissingIsEmpty(t *testing.T) {
	state, err := LoadDeployResourceState(t.TempDir())
	require.NoError(t, err)
	assert.NotNil(t, state.D1)
	assert.Empty(t, state.D1)
}

func TestLoadDeployResourceState_CorruptIsAnError(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".sprout"), 0o755))
	require.NoError(t, os.WriteFile(deployResourceStatePath(root), []byte("{not json"), 0o644))
	_, err := LoadDeployResourceState(root)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// wrangler.toml write-back (unit)
// ---------------------------------------------------------------------------

func TestReplaceD1DatabaseID(t *testing.T) {
	content := `[[d1_databases]]
binding = "DB"
database_name = "web-app-data-db"
database_id = "00000000-0000-0000-0000-000000000000"
migrations_dir = "drizzle/migrations"

[[d1_databases]]
binding = "OTHER"
database_id = "keep-me"
`
	updated, ok := replaceD1DatabaseID(content, "DB", "real-id")
	require.True(t, ok)
	assert.Contains(t, updated, `database_id = "real-id"`)
	assert.Contains(t, updated, `database_id = "keep-me"`, "a different block is untouched")
	assert.NotContains(t, updated, placeholderD1ID)

	_, ok = replaceD1DatabaseID(content, "MISSING", "x")
	assert.False(t, ok)
}

func TestWriteD1IDIntoWrangler_UnknownBindingIsAnError(t *testing.T) {
	root := writeWrangler(t, t.TempDir(), minimalWrangler)
	err := writeD1IDIntoWrangler(root, "DB", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DB")
}

// ---------------------------------------------------------------------------
// CloudflareTargetFor wires the Workers binding path
// ---------------------------------------------------------------------------

func TestCloudflareTargetFor_WorkersPerformsBindingSetup(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "web-app-data")
	target, err := CloudflareTargetFor(DeployTargetWorkers,
		CloudflareConfig{AccountID: "acct-1", Project: "web-app-data"},
		Credential{value: cfSentinelToken}, srv.URL, srv.Client())
	require.NoError(t, err)

	root := writeWrangler(t, t.TempDir(), webAppDataWrangler)
	writeMigration(t, root, "0000_init.sql", "CREATE TABLE items (id integer primary key);")
	buildDir := writeBuildOutput(t, map[string]string{"worker.js": "export default {}"})

	_, err = target.Deploy(DeployRequest{Project: "web-app-data", BuildDir: buildDir, Root: root})
	require.NoError(t, err)
	assert.Len(t, f.d1, 1, "the Workers target created the declared D1 database")
	assert.Len(t, workerUploadRequests(f), 1)
}
