/**
 * Read a stored value under `key`. A value still saved under the key's
 * earlier spelling is moved to `key` first, so it is read once more and
 * then lives only under the current name.
 */
export function readMovedItem(storage: Storage, key: string, earlierKey: string): string | null {
  const value = storage.getItem(key);
  if (value !== null || key === earlierKey) return value;
  const earlier = storage.getItem(earlierKey);
  if (earlier === null) return null;
  try {
    storage.setItem(key, earlier);
    storage.removeItem(earlierKey);
  } catch {
    // Quota or a locked storage: the earlier value still answers this read.
  }
  return earlier;
}
