/**
 * Terminal scrollback persistence service using IndexedDB.
 * Provides client-side buffering of terminal content for restoration after reconnects.
 */

import { debugLog } from '../utils/log';

const DB_NAME = 'sprout-terminal-scrollback';
const DB_VERSION = 1;
const STORE_NAME = 'scrollback';
const MAX_SIZE_BYTES = 500 * 1024; // 500KB
const MAX_AGE_MS = 24 * 60 * 60 * 1000; // 24 hours

interface ScrollbackEntry {
  sessionId: string;
  data: string;
  timestamp: number;
}

/**
 * Open the IndexedDB database and return a promise that resolves to the database instance.
 */
async function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, DB_VERSION);

    request.onerror = () => {
      const err = request.error;
      debugLog('[terminalScrollback] Failed to open database:', err);
      reject(err);
    };

    request.onsuccess = () => {
      resolve(request.result);
    };

    request.onupgradeneeded = (event) => {
      const db = (event.target as IDBOpenDBRequest).result;

      // Create the scrollback object store with sessionId as key
      if (!db.objectStoreNames.contains(STORE_NAME)) {
        const store = db.createObjectStore(STORE_NAME, { keyPath: 'sessionId' });
        // Create an index on timestamp for efficient cleanup queries
        store.createIndex('timestamp', 'timestamp', { unique: false });
        debugLog('[terminalScrollback] Created object store:', STORE_NAME);
      }
    };
  });
}

/**
 * Measure the byte size of a string (UTF-8).
 */
function byteSize(data: string): number {
  return new TextEncoder().encode(data).length;
}

/**
 * Truncate data from the beginning if it exceeds the maximum size.
 * Uses byte-accurate measurement for correct handling of multi-byte UTF-8.
 */
function truncateData(data: string): string {
  if (byteSize(data) <= MAX_SIZE_BYTES) {
    return data;
  }

  // We need to truncate to within MAX_SIZE_BYTES. Walk backward from the end
  // to find a safe truncation point that fits within the budget, using line
  // boundaries for cleaner output.
  const encoder = new TextEncoder();
  const targetSize = MAX_SIZE_BYTES - 1024; // Leave 1KB buffer
  let truncateAt = 0;

  // Scan forward to find a position where the remaining data fits.
  // Start from a rough estimate and refine.
  const roughEstimate = Math.floor(data.length * (targetSize / byteSize(data)));
  let pos = Math.max(0, roughEstimate - 500);

  while (pos < data.length) {
    const remaining = data.slice(pos);
    if (encoder.encode(remaining).length <= targetSize) {
      truncateAt = pos;
      break;
    }
    // Find next newline to try
    const nextNl = data.indexOf('\n', pos + 1);
    if (nextNl === -1) break;
    pos = nextNl + 1;
  }

  if (truncateAt === 0) {
    // Fallback: just cut at a safe byte boundary
    truncateAt = roughEstimate;
  }

  const truncated = data.slice(truncateAt);
  debugLog('[terminalScrollback] Truncated scrollback from', byteSize(data), 'to', byteSize(truncated), 'bytes');
  return truncated;
}

/**
 * Save terminal scrollback data to IndexedDB.
 * @param sessionId - The terminal session ID
 * @param data - The serialized terminal content
 */
export async function saveScrollback(sessionId: string, data: string): Promise<void> {
  try {
    const db = await openDB();

    // Check and truncate if necessary (byte-accurate for multi-byte UTF-8)
    const size = byteSize(data);
    let dataToSave = data;

    if (size > MAX_SIZE_BYTES) {
      dataToSave = truncateData(dataToSave);
    }

    const entry: ScrollbackEntry = {
      sessionId,
      data: dataToSave,
      timestamp: Date.now(),
    };

    return new Promise<void>((resolve, reject) => {
      const transaction = db.transaction(STORE_NAME, 'readwrite');
      const store = transaction.objectStore(STORE_NAME);
      const request = store.put(entry);

      request.onerror = () => {
        const err = request.error;
        debugLog('[terminalScrollback] Failed to save scrollback:', err);
        reject(err);
      };

      request.onsuccess = () => {
        debugLog('[terminalScrollback] Saved scrollback for session:', sessionId, 'size:', dataToSave.length);
        resolve();
      };

      transaction.onerror = () => {
        const err = transaction.error;
        debugLog('[terminalScrollback] Transaction error while saving scrollback:', err);
        reject(err);
      };
    });
  } catch (err) {
    // Silently catch and log errors - never block the terminal
    debugLog('[terminalScrollback] Error saving scrollback:', err);
  }
}

/**
 * Load terminal scrollback data from IndexedDB.
 * @param sessionId - The terminal session ID
 * @returns The saved scrollback data, or null if not found or too old
 */
export async function loadScrollback(sessionId: string): Promise<string | null> {
  try {
    const db = await openDB();

    return new Promise<string | null>((resolve, reject) => {
      const transaction = db.transaction(STORE_NAME, 'readonly');
      const store = transaction.objectStore(STORE_NAME);
      const request = store.get(sessionId);

      request.onerror = () => {
        const err = request.error;
        debugLog('[terminalScrollback] Failed to load scrollback:', err);
        reject(err);
      };

      request.onsuccess = () => {
        const entry = request.result as ScrollbackEntry | undefined;

        if (!entry) {
          resolve(null);
          return;
        }

        // Check if entry is too old
        const age = Date.now() - entry.timestamp;
        if (age > MAX_AGE_MS) {
          debugLog('[terminalScrollback] Scrollback entry too old, deleting:', sessionId);
          deleteScrollback(sessionId).catch(() => {});
          resolve(null);
          return;
        }

        debugLog('[terminalScrollback] Loaded scrollback for session:', sessionId, 'size:', entry.data.length);
        resolve(entry.data);
      };

      transaction.onerror = () => {
        const err = transaction.error;
        debugLog('[terminalScrollback] Transaction error while loading scrollback:', err);
        reject(err);
      };
    });
  } catch (err) {
    debugLog('[terminalScrollback] Error loading scrollback:', err);
    return null;
  }
}

/**
 * Delete terminal scrollback data from IndexedDB.
 * @param sessionId - The terminal session ID
 */
export async function deleteScrollback(sessionId: string): Promise<void> {
  try {
    const db = await openDB();

    return new Promise<void>((resolve, reject) => {
      const transaction = db.transaction(STORE_NAME, 'readwrite');
      const store = transaction.objectStore(STORE_NAME);
      const request = store.delete(sessionId);

      request.onerror = () => {
        const err = request.error;
        debugLog('[terminalScrollback] Failed to delete scrollback:', err);
        reject(err);
      };

      request.onsuccess = () => {
        debugLog('[terminalScrollback] Deleted scrollback for session:', sessionId);
        resolve();
      };

      transaction.onerror = () => {
        const err = transaction.error;
        debugLog('[terminalScrollback] Transaction error while deleting scrollback:', err);
        reject(err);
      };
    });
  } catch (err) {
    debugLog('[terminalScrollback] Error deleting scrollback:', err);
  }
}

/**
 * Clean up all scrollback entries older than 24 hours.
 * Should be called on initialization to remove stale data.
 */
export async function cleanupOldEntries(): Promise<void> {
  try {
    const db = await openDB();
    const cutoffTime = Date.now() - MAX_AGE_MS;
    let deletedCount = 0;

    return new Promise<void>((resolve, reject) => {
      const transaction = db.transaction(STORE_NAME, 'readwrite');
      const store = transaction.objectStore(STORE_NAME);
      const index = store.index('timestamp');

      // Use a cursor to find and delete old entries
      const request = index.openCursor(IDBKeyRange.upperBound(cutoffTime));

      request.onerror = () => {
        const err = request.error;
        debugLog('[terminalScrollback] Failed to cleanup old entries:', err);
        reject(err);
      };

      request.onsuccess = () => {
        const cursor = request.result;

        if (cursor) {
          cursor.delete();
          deletedCount++;
          cursor.continue();
        } else {
          if (deletedCount > 0) {
            debugLog('[terminalScrollback] Cleaned up', deletedCount, 'old entries');
          }
          resolve();
        }
      };

      transaction.onerror = () => {
        const err = transaction.error;
        debugLog('[terminalScrollback] Transaction error during cleanup:', err);
        reject(err);
      };
    });
  } catch (err) {
    debugLog('[terminalScrollback] Error cleaning up old entries:', err);
  }
}
