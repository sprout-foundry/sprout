// The `cloudflare:test` module exposes `env` typed as the empty ProvidedEnv.
// Declaring the D1 binding here (matching the `DB` binding in wrangler.toml)
// gives the API test a typed `env.DB`.
declare module 'cloudflare:test' {
  interface ProvidedEnv {
    DB: D1Database;
  }
}

// The API test imports the migration SQL as text (`?raw`), so TypeScript
// needs the module shape for it.
declare module '*?raw' {
  const content: string;
  export default content;
}
