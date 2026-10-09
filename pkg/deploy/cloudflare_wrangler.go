// Wrangler configuration for the Cloudflare Workers adapter.
//
// A Workers project declares the resources its script binds to (D1 databases,
// KV namespaces, R2 buckets) in wrangler.toml, not in the deploy config. The
// adapter reads that file from the project root so it can create or reuse each
// declared resource and attach it to the uploaded script; without it a
// server-side starter deploys with no database and its API fails at runtime.
//
// Only the blocks the file actually declares matter: a commented-out KV or R2
// block is not a binding, so the adapter never invents one. The file is parsed
// with the TOML decoder the module already depends on (via viper), so no new
// third-party surface is introduced.
package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// WranglerFileName is the conventional Cloudflare Workers config file at the
// project root.
const WranglerFileName = "wrangler.toml"

// placeholderD1ID is the all-zero database id the reference starter ships. It
// points at nothing, so the adapter reads it as "create the database".
const placeholderD1ID = "00000000-0000-0000-0000-000000000000"

// WranglerConfig is the subset of wrangler.toml the Workers adapter needs: the
// worker name and entry point, and the bindings the project declares. Each
// slice holds exactly the blocks present in the file — an absent block is an
// empty slice, never a synthesized default.
type WranglerConfig struct {
	// Name is the worker name (`name`). It is the script name the adapter
	// uploads to and the anchor for the resource names it creates.
	Name string `toml:"name"`
	// Main is the worker entry point (`main`), relative to the project root.
	Main string `toml:"main"`
	// D1Databases are the declared [[d1_databases]] blocks.
	D1Databases []WranglerD1 `toml:"d1_databases"`
	// KVNamespaces are the declared [[kv_namespaces]] blocks.
	KVNamespaces []WranglerKV `toml:"kv_namespaces"`
	// R2Buckets are the declared [[r2_buckets]] blocks.
	R2Buckets []WranglerR2 `toml:"r2_buckets"`
}

// WranglerD1 is one [[d1_databases]] block.
type WranglerD1 struct {
	Binding       string `toml:"binding"`
	DatabaseName  string `toml:"database_name"`
	DatabaseID    string `toml:"database_id"`
	MigrationsDir string `toml:"migrations_dir"`
}

// WranglerKV is one [[kv_namespaces]] block.
type WranglerKV struct {
	Binding string `toml:"binding"`
	ID      string `toml:"id"`
}

// WranglerR2 is one [[r2_buckets]] block.
type WranglerR2 struct {
	Binding    string `toml:"binding"`
	BucketName string `toml:"bucket_name"`
}

// LoadWranglerConfig reads and parses wrangler.toml from the project root. A
// missing file is an error: a Workers deploy without its declared bindings
// would silently ship a broken app, so the adapter refuses rather than
// guessing.
func LoadWranglerConfig(root string) (*WranglerConfig, error) {
	path := filepath.Join(strings.TrimSpace(root), WranglerFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("cloudflare: no %s in %s (a Workers deploy needs the project's declared bindings)", WranglerFileName, root)
		}
		return nil, fmt.Errorf("cloudflare: read %s: %w", path, err)
	}
	cfg := &WranglerConfig{}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("cloudflare: parse %s: %w", path, err)
	}
	return cfg, nil
}

// isPlaceholderD1ID reports whether id is empty or the all-zero placeholder,
// both of which mean "this database does not exist yet — create it".
func isPlaceholderD1ID(id string) bool {
	id = strings.TrimSpace(id)
	return id == "" || id == placeholderD1ID
}

// isRealKVID reports whether a [[kv_namespaces]] id is a real namespace id
// rather than an empty or placeholder value. A placeholder (the all-zero id,
// or a REPLACE_WITH… marker) means "create it".
func isRealKVID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || id == placeholderD1ID {
		return false
	}
	return !strings.HasPrefix(strings.ToUpper(id), "REPLACE")
}
