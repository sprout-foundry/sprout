package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// webAppDataWrangler mirrors the reference starter's wrangler.toml: one D1
// database whose id is the all-zero placeholder (meaning "create it"), a
// migrations directory, and commented-out KV/R2 blocks that must NOT be
// treated as declared bindings.
const webAppDataWrangler = `name = "web-app-data"
main = "src/worker/index.ts"
compatibility_date = "2024-12-01"

[assets]
directory = "./dist"
binding = "ASSETS"

[[d1_databases]]
binding = "DB"
database_name = "web-app-data-db"
database_id = "00000000-0000-0000-0000-000000000000"
migrations_dir = "drizzle/migrations"

# [[kv_namespaces]]
# binding = "KV"
# id = "REPLACE_WITH_KV_NAMESPACE_ID"
#
# [[r2_buckets]]
# binding = "BUCKET"
# bucket_name = "web-app-data-bucket"
`

// writeMigration writes a numbered migration file under the project's
// migrations directory.
func writeMigration(t *testing.T, root, name, sql string) {
	t.Helper()
	dir := filepath.Join(root, "drizzle", "migrations")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(sql), 0o644))
}

// readWrangler reads the project's wrangler.toml back.
func readWrangler(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, WranglerFileName))
	require.NoError(t, err)
	return string(data)
}

// workerUploadRequests returns the script-upload requests the fake recorded.
func workerUploadRequests(f *cloudflareFake) []fakeRequest {
	var out []fakeRequest
	for _, r := range f.requestsSnapshot() {
		if strings.Contains(r.Path, "/workers/scripts/") {
			out = append(out, r)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// wrangler.toml parsing
// ---------------------------------------------------------------------------

func TestLoadWranglerConfig_ParsesDeclaredBindings(t *testing.T) {
	root := writeWrangler(t, t.TempDir(), webAppDataWrangler)

	cfg, err := LoadWranglerConfig(root)
	require.NoError(t, err)
	assert.Equal(t, "web-app-data", cfg.Name)
	assert.Equal(t, "src/worker/index.ts", cfg.Main)
	require.Len(t, cfg.D1Databases, 1)
	assert.Equal(t, "DB", cfg.D1Databases[0].Binding)
	assert.Equal(t, "web-app-data-db", cfg.D1Databases[0].DatabaseName)
	assert.True(t, isPlaceholderD1ID(cfg.D1Databases[0].DatabaseID))
	assert.Equal(t, "drizzle/migrations", cfg.D1Databases[0].MigrationsDir)
	// The commented-out blocks are not bindings.
	assert.Empty(t, cfg.KVNamespaces)
	assert.Empty(t, cfg.R2Buckets)
}

func TestLoadWranglerConfig_MissingFileIsAnError(t *testing.T) {
	_, err := LoadWranglerConfig(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), WranglerFileName)
}

func TestIsPlaceholderD1ID(t *testing.T) {
	assert.True(t, isPlaceholderD1ID(""))
	assert.True(t, isPlaceholderD1ID("  "))
	assert.True(t, isPlaceholderD1ID(placeholderD1ID))
	assert.False(t, isPlaceholderD1ID("abc-123"))
}

// ---------------------------------------------------------------------------
// Create-when-placeholder, reuse-on-rerun, no cross-project sharing
// ---------------------------------------------------------------------------

func TestCloudflareWorkers_CreatesPlaceholderD1AndWritesIDBack(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "web-app-data")
	target := newWorkersTarget(t, f, srv, cfSentinelToken)
	root := writeWrangler(t, t.TempDir(), webAppDataWrangler)
	writeMigration(t, root, "0000_init.sql", "CREATE TABLE items (id integer primary key, name text not null);")
	buildDir := writeBuildOutput(t, map[string]string{"worker.js": "export default {}"})

	_, err := target.Deploy(DeployRequest{Project: "web-app-data", BuildDir: buildDir, Root: root})
	require.NoError(t, err)

	// A D1 database was created.
	require.Len(t, f.d1, 1)

	// The real id was written back into wrangler.toml, replacing the
	// placeholder, and the rest of the file (comments included) survived.
	content := readWrangler(t, root)
	assert.NotContains(t, content, placeholderD1ID, "the placeholder id must be replaced")
	assert.Contains(t, content, `database_id = "d1-1"`)
	assert.Contains(t, content, "# [[kv_namespaces]]", "comments are preserved")
	assert.Contains(t, content, `database_name = "web-app-data-db"`)

	// The script upload carried the D1 binding with the real id.
	uploads := workerUploadRequests(f)
	require.Len(t, uploads, 1)
	assert.Contains(t, uploads[0].Body, `"type":"d1"`)
	assert.Contains(t, uploads[0].Body, `"name":"DB"`)
	assert.Contains(t, uploads[0].Body, `"id":"d1-1"`)
}

func TestCloudflareWorkers_RerunReusesResources(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "web-app-data")
	target := newWorkersTarget(t, f, srv, cfSentinelToken)
	root := writeWrangler(t, t.TempDir(), webAppDataWrangler)
	writeMigration(t, root, "0000_init.sql", "CREATE TABLE items (id integer primary key, name text not null);")
	buildDir := writeBuildOutput(t, map[string]string{"worker.js": "export default {}"})

	_, err := target.Deploy(DeployRequest{Project: "web-app-data", BuildDir: buildDir, Root: root})
	require.NoError(t, err)
	firstID := readWrangler(t, root)

	_, err = target.Deploy(DeployRequest{Project: "web-app-data", BuildDir: buildDir, Root: root})
	require.NoError(t, err)

	assert.Len(t, f.d1, 1, "a rerun must not create a duplicate database")
	assert.Equal(t, firstID, readWrangler(t, root), "the id is stable across reruns")

	// The second deploy did not re-create the database (one POST to the
	// create endpoint only).
	creates := 0
	for _, r := range f.requestsSnapshot() {
		if r.Method == "POST" && strings.HasSuffix(r.Path, "/d1/database") {
			creates++
		}
	}
	assert.Equal(t, 1, creates, "only the first deploy creates the database")
}

func TestCloudflareWorkers_TwoProjectsDoNotShareAResource(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "app-a")
	target := newWorkersTarget(t, f, srv, cfSentinelToken)

	rootA := writeWrangler(t, t.TempDir(), `name = "app-a"
main = "src/worker/a.ts"

[[d1_databases]]
binding = "DB"
database_name = "shared-display-name"
database_id = "00000000-0000-0000-0000-000000000000"
`)
	rootB := writeWrangler(t, t.TempDir(), `name = "app-b"
main = "src/worker/b.ts"

[[d1_databases]]
binding = "DB"
database_name = "shared-display-name"
database_id = "00000000-0000-0000-0000-000000000000"
`)
	buildDir := writeBuildOutput(t, map[string]string{"worker.js": "export default {}"})

	// Two different projects declare the same display database_name: the
	// resource is found/created under the stable per-worker name, so they get
	// distinct databases rather than adopting one another's.
	_, err := target.Deploy(DeployRequest{Project: "app-a", BuildDir: buildDir, Root: rootA})
	require.NoError(t, err)

	targetB, err := NewCloudflareWorkersTarget(
		CloudflareConfig{AccountID: "acct-1", Project: "app-b"},
		Credential{value: cfSentinelToken}, srv.URL, srv.Client())
	require.NoError(t, err)
	_, err = targetB.Deploy(DeployRequest{Project: "app-b", BuildDir: buildDir, Root: rootB})
	require.NoError(t, err)

	assert.Len(t, f.d1, 2, "each project gets its own database even with a shared display name")

	idA := readWrangler(t, rootA)
	idB := readWrangler(t, rootB)
	assert.NotEqual(t, idA, idB, "the two projects must not share a database id")

	// The created databases are named from the worker name, not the display
	// name.
	names := map[string]bool{}
	for _, db := range f.d1 {
		names[db.Name] = true
	}
	assert.True(t, names["app-a-d1-DB"], "database named from the worker name: %v", names)
	assert.True(t, names["app-b-d1-DB"], "database named from the worker name: %v", names)
}

func TestStableResourceName_DistinctPerWorker(t *testing.T) {
	assert.NotEqual(t, stableResourceName("app-a", "d1-DB"), stableResourceName("app-b", "d1-DB"))
	assert.Equal(t, stableResourceName("app-a", "d1-DB"), stableResourceName("app-a", "d1-DB"))
	assert.Equal(t, "app-a", stableResourceName("app-a", ""))
}

// ---------------------------------------------------------------------------
// All-or-nothing
// ---------------------------------------------------------------------------

func TestCloudflareWorkers_BindingFailureDoesNotUploadScript(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "web-app-data")
	target := newWorkersTarget(t, f, srv, cfSentinelToken)
	root := writeWrangler(t, t.TempDir(), webAppDataWrangler)
	buildDir := writeBuildOutput(t, map[string]string{"worker.js": "export default {}"})

	// The D1 create endpoint fails: the deploy must fail naming the binding
	// and must NOT upload the script (which would make it live).
	f.fail(500, `{"success":false,"errors":[{"code":7500,"message":"database create failed"}]}`, "/d1/database")

	_, err := target.Deploy(DeployRequest{Project: "web-app-data", BuildDir: buildDir, Root: root})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBindingUnavailable)
	assert.Contains(t, err.Error(), "DB", "the error names the binding")
	assert.Empty(t, workerUploadRequests(f), "the script must not go live with a binding missing")
}

func TestCloudflareWorkers_MigrationFailureDoesNotUploadScript(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "web-app-data")
	target := newWorkersTarget(t, f, srv, cfSentinelToken)
	root := writeWrangler(t, t.TempDir(), webAppDataWrangler)
	writeMigration(t, root, "0000_init.sql", "CREATE TABLE items (id integer primary key);")
	buildDir := writeBuildOutput(t, map[string]string{"worker.js": "export default {}"})

	f.fail(500, `{"success":false,"errors":[{"code":7500,"message":"query failed"}]}`, "/query")

	_, err := target.Deploy(DeployRequest{Project: "web-app-data", BuildDir: buildDir, Root: root})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBindingUnavailable)
	assert.Empty(t, workerUploadRequests(f), "the script must not go live with migrations unapplied")
}

func TestCloudflareWorkers_NoRootIsRefused(t *testing.T) {
	f, srv := newCloudflareFake(t, "acct-1", "web-app-data")
	target := newWorkersTarget(t, f, srv, cfSentinelToken)
	buildDir := writeBuildOutput(t, map[string]string{"worker.js": "export default {}"})

	_, err := target.Deploy(DeployRequest{Project: "web-app-data", BuildDir: buildDir})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBindingUnavailable)
	assert.Empty(t, f.requestsSnapshot())
}
