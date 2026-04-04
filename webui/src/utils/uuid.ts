/**
 * Generates a UUID v4 string.
 * Uses crypto.randomUUID() if available, otherwise falls back to a custom implementation.
 */
export const generateUUID = (): string => {
  if (crypto.randomUUID) {
    return crypto.randomUUID();
  }

  // Fallback for older browsers that don't support crypto.randomUUID()
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const randomNumber = Math.floor(Math.random() * 16);
    const value = c === 'x' ? randomNumber : (randomNumber & 0x3) | 0x8;
    return value.toString(16);
  });
};