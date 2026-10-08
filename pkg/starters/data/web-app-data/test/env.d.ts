// The `cloudflare:test` module exposes `env` typed as `Cloudflare.Env`.
// Declaring the D1 binding here (matching the `DB` binding in wrangler.toml)
// gives the API test a typed `env.DB`.
declare namespace Cloudflare {
  interface Env {
    DB: D1Database;
  }
}

// The API test imports the migration SQL as text (`?raw`), so TypeScript
// needs the module shape for it.
declare module '*?raw' {
  const content: string;
  export default content;
}
