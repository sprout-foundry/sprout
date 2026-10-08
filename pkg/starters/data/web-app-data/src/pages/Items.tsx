import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';

// A minimal client for the API under /api/*. It fetches the items from
// GET /api/items on mount and posts a new one to POST /api/items. Both the
// dev server (wrangler dev) and the deployed Worker serve the app and the
// API from the same origin, so the paths are relative.
type Item = { id: number; name: string };

export default function Items() {
  const [items, setItems] = useState<Item[]>([]);
  const [name, setName] = useState('');
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetch('/api/items')
      .then((res) => (res.ok ? res.json() : Promise.reject(new Error(String(res.status)))))
      .then((rows) => {
        if (!cancelled) {
          setItems(rows as Item[]);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setError('Could not load items.');
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  async function addItem(event: React.FormEvent) {
    event.preventDefault();
    const trimmed = name.trim();
    if (trimmed === '') {
      return;
    }
    try {
      const res = await fetch('/api/items', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: trimmed }),
      });
      if (!res.ok) {
        throw new Error(String(res.status));
      }
      const created = (await res.json()) as Item;
      setItems((current) => [...current, created]);
      setName('');
      setError(null);
    } catch {
      setError('Could not add the item.');
    }
  }

  return (
    <>
      <h1>Items</h1>
      <p>
        These items are stored in the D1 database and read through the API at{' '}
        <code>/api/items</code>.
      </p>

      <form onSubmit={addItem}>
        <label htmlFor="item-name">New item</label>{' '}
        <input
          id="item-name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="Item name"
        />{' '}
        <button type="submit">Add item</button>
      </form>

      {error ? <p role="alert">{error}</p> : null}

      <ul data-testid="item-list">
        {items.map((item) => (
          <li key={item.id}>{item.name}</li>
        ))}
      </ul>

      <p>
        <Link to="/">Back home</Link>
      </p>
    </>
  );
}
