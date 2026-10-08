import { integer, sqliteTable, text } from 'drizzle-orm/sqlite-core';

// The one table this starter ships. `items` is a D1 table (D1 is SQLite), so
// the schema uses drizzle-orm's sqlite-core builders. A migration matching
// this shape lives in drizzle/migrations; regenerate it with
// `npx drizzle-kit generate` after changing this file.
export const items = sqliteTable('items', {
  id: integer('id').primaryKey({ autoIncrement: true }),
  name: text('name').notNull(),
});

export type Item = typeof items.$inferSelect;
export type NewItem = typeof items.$inferInsert;
