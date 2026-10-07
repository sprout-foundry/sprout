import { drizzle } from 'drizzle-orm/d1';
import { Hono } from 'hono';
import { asc } from 'drizzle-orm';
import { items } from './schema';

// The Worker environment: the D1 database is bound as DB (wrangler.toml) and
// drizzle wraps it. The static-assets binding is declared so the type is
// complete; the assets are served by the Worker runtime before the Hono app
// sees a request, so the app itself never reads it.
export type Env = {
  DB: D1Database;
  ASSETS: Fetcher;
};

const app = new Hono<{ Bindings: Env }>();

app.get('/api/items', async (c) => {
  const db = drizzle(c.env.DB);
  const rows = await db.select().from(items).orderBy(asc(items.id));
  return c.json(rows);
});

app.post('/api/items', async (c) => {
  const body = await readName(c.req.raw);
  if (body === null) {
    return c.json({ error: 'name is required' }, 400);
  }
  const db = drizzle(c.env.DB);
  const [created] = await db.insert(items).values({ name: body }).returning();
  return c.json(created, 201);
});

async function readName(request: Request): Promise<string | null> {
  try {
    const body = (await request.json()) as { name?: unknown };
    if (typeof body.name !== 'string' || body.name.trim() === '') {
      return null;
    }
    return body.name.trim();
  } catch {
    return null;
  }
}

export default app;
