import { drizzle } from 'drizzle-orm/d1';
import { Hono } from 'hono';
import { asc } from 'drizzle-orm';
import { items } from './schema';

// The Worker environment: the D1 database is bound as DB (wrangler.toml) and
// drizzle wraps it. ASSETS is the static-assets binding; the wildcard route
// below forwards non-API requests to it, which is what serves the app and
// lets a client route such as /about resolve to dist/index.html.
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

// Fall through to the static assets for everything that is not an API route.
// Those assets carry `not_found_handling = "single-page-application"` so a path
// with no real file (a client route such as /about) is answered with
// dist/index.html and the router renders it — which is also what a direct
// visit to /about needs. Keeping the fallback beside the app means the API
// route table fully describes the app, with no extra runtime wiring.
app.get('*', (c) => c.env.ASSETS.fetch(c.req.raw));

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
