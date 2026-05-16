/** Get a nested value from an object using dot-notation key */
export function getNestedValue(obj: object, key: string): unknown {
  return key
    .split('.')
    .reduce(
      (o: unknown, k: string) =>
        o && typeof o === 'object' && k in (o as Record<string, unknown>) ? (o as Record<string, unknown>)[k] : '',
      obj,
    );
}

/** Set a nested value in an object using dot-notation key (immutable) */
export function setNestedValue(obj: object, key: string, value: unknown): object {
  const parts = key.split('.');
  const result = { ...obj } as Record<string, unknown>;
  let current: Record<string, unknown> = result;
  for (let i = 0; i < parts.length - 1; i++) {
    if (current[parts[i]] === undefined || typeof current[parts[i]] !== 'object') {
      current[parts[i]] = {};
    } else {
      current[parts[i]] = { ...(current[parts[i]] as Record<string, unknown>) };
    }
    current = current[parts[i]] as Record<string, unknown>;
  }
  current[parts[parts.length - 1]] = value;
  return result;
}
