import { env, SELF } from 'cloudflare:test';
import { beforeAll, describe, expect, it } from 'vitest';
import migrationSql from '../drizzle/migrations/0000_init.sql?raw';

// The API test runs inside the Workers runtime through
// @cloudflare/vitest-plugin. SELF dispatches a request through the
// Worker entry point (the Hono app) with the D1 binding from wrangler.toml;
// env.DB is the same local database, so the test can apply the migration
// directly and then exercise the route end to end. The migration SQL is
// imported as text so it is applied exactly as written on disk.

async function applyMigration() {
  const statements = migrationSql
    .split(';')
    .map((part) => part.trim())
    .filter((part) => part.length > 0);
  for (const statement of statements) {
    await env.DB.prepare(statement).run();
  }
}

beforeAll(async () => {
  await applyMigration();
});

describe('items API', () => {
  it('lists an empty table', async () => {
    const res = await SELF.fetch('http://example.com/api/items');
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual([]);
  });

  it('creates an item and lists it back', async () => {
    const create = await SELF.fetch('http://example.com/api/items', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'First item' }),
    });
    expect(create.status).toBe(201);
    const created = (await create.json()) as { id: number; name: string };
    expect(created.name).toBe('First item');
    expect(created.id).toBeGreaterThan(0);

    const list = await SELF.fetch('http://example.com/api/items');
    const rows = (await list.json()) as { id: number; name: string }[];
    expect(rows).toContainEqual({ id: created.id, name: 'First item' });
  });

  it('rejects a create without a name', async () => {
    const res = await SELF.fetch('http://example.com/api/items', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({}),
    });
    expect(res.status).toBe(400);
  });
});
