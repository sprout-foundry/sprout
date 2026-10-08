import { defineConfig } from 'drizzle-kit';

// Drizzle Kit reads the schema and writes migrations into drizzle/migrations.
// D1 is SQLite, so the dialect is sqlite. `npx drizzle-kit generate` after a
// schema change adds the next numbered migration next to 0000_init.sql, which
// `wrangler d1 migrations apply` then applies.
export default defineConfig({
  dialect: 'sqlite',
  schema: './src/worker/schema.ts',
  out: './drizzle/migrations',
});
