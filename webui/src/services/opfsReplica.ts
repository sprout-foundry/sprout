/**
 * OPFS (Origin Private File System) Replica Service.
 *
 * Provides a local file-system-backed replica of workspace files using the
 * browser's Origin Private File System API.  All data is stored under
 * `navigator.storage.getDirectory()` so it is isolated to this origin and
 * persists across browser sessions.
 *
 * A metadata index is kept in `.opfs-meta/index.json` to track per-file
 * sequence numbers and sync state.
 */

/* ------------------------------------------------------------------ */
/* Type declarations for OPFS APIs not yet in the default TypeScript    */
/* DOM lib.  navigator.storage.getDirectory() is part of the            */
/* File System Access API and may not be in the shipped DOM lib.       */
/* ------------------------------------------------------------------ */

interface StorageManagerWithOPFS extends StorageManager {
  getDirectory(): Promise<FileSystemDirectoryHandle>;
}

// ----------------------------------------------------------------------
// Public interfaces
// ----------------------------------------------------------------------

/**
 * Per-file metadata stored in the OPFS replica.
 */
export interface OPFSFileMetadata {
  /** Browser sequence counter */
  browserSeq: number;
  /** Container sequence counter */
  containerSeq: number;
  /** Timestamp in milliseconds since Unix epoch when the file was last synced */
  lastSynced: number;
  /** File size in bytes */
  size: number;
  /** ISO 8601 timestamp */
  modifiedAt: string;
}

/**
 * Replica status returned by {@link OPFSReplicaService.getStatus}.
 */
export interface OPFSReplicaStatus {
  /** Number of files in the replica */
  fileCount: number;
  /** Total size of all files in bytes */
  totalSize: number;
  /** ISO 8601 string representing the most recent `lastSynced` value across all files, or `null` if no files have been synced */
  lastSyncTimestamp: string | null;
}

/**
 * File manifest entry for {@link OPFSReplicaService.initReplica}.
 */
export interface OPFSManifestEntry {
  /** File path relative to the replica root */
  path: string;
  /** File content as a UTF-8 string */
  content: string;
  /** Optional partial metadata to associate with the file */
  metadata?: Partial<OPFSFileMetadata>;
}

/**
 * Patch operation for {@link OPFSReplicaService.applyPatch}.
 */
export interface OPFSPatchOp {
  /** Operation type */
  op: 'upsert' | 'delete';
  /** File path relative to the replica root */
  path: string;
  /** File content for upsert (plain UTF-8 string) */
  content?: string;
  /** File content for upsert (base64-encoded) */
  content_base64?: string;
  /** Optional partial metadata to associate with the file */
  metadata?: Partial<OPFSFileMetadata>;
}

// ----------------------------------------------------------------------
// Internal types
// ----------------------------------------------------------------------

/** Serialized form of the metadata index stored on disk. */
interface SerializedMetadataIndex {
  [path: string]: OPFSFileMetadata;
}

// ----------------------------------------------------------------------
// Constants
// ----------------------------------------------------------------------

const METADATA_INDEX_PATH = '.opfs-meta/index.json';

// ----------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------

/**
 * Decode a base64 string to a UTF-8 string.
 *
 * @throws Error if the input is not valid base64.
 */
function base64Decode(base64: string): string {
  try {
    const binary = atob(base64);
    return new TextDecoder().decode(
      Uint8Array.from(binary, (c) => c.charCodeAt(0)),
    );
  } catch (e) {
    throw new Error(`invalid base64 content: ${e instanceof Error ? e.message : String(e)}`);
  }
}

/**
 * Resolve a file path into directory path + file name.
 */
function splitPath(filePath: string): { dirPath: string; fileName: string } {
  const idx = filePath.lastIndexOf('/');
  if (idx === -1) {
    return { dirPath: '', fileName: filePath };
  }
  return { dirPath: filePath.slice(0, idx), fileName: filePath.slice(idx + 1) };
}

/**
 * Build an {@link OPFSFileMetadata} from partial input, filling defaults.
 */
function defaultMetadata(partial: Partial<OPFSFileMetadata> = {}): OPFSFileMetadata {
  const now = Date.now();
  return {
    browserSeq: partial.browserSeq ?? 0,
    containerSeq: partial.containerSeq ?? 0,
    lastSynced: partial.lastSynced ?? 0,
    size: partial.size ?? 0,
    modifiedAt: partial.modifiedAt ?? new Date(now).toISOString(),
  };
}

// ----------------------------------------------------------------------
// Service class
// ----------------------------------------------------------------------

export class OPFSReplicaService {
  private root: FileSystemDirectoryHandle | null = null;
  private metadataIndex: Map<string, OPFSFileMetadata> = new Map();
  private initialized: boolean = false;
  private unavailable: boolean = false;

  /**
   * Check whether OPFS is available in the current environment.
   */
  static isAvailable(): boolean {
    if (typeof navigator === 'undefined') return false;
    if (!('storage' in navigator)) return false;
    return typeof (navigator.storage as StorageManagerWithOPFS).getDirectory === 'function';
  }

  // ------------------------------------------------------------------
  // Public API
  // ------------------------------------------------------------------

  /**
   * Opens the OPFS root directory and loads the metadata index.
   * Call this once at application startup before using other methods.
   * If OPFS is not available, sets an internal flag so other methods
   * degrade gracefully.
   */
  async init(): Promise<void> {
    if (this.initialized) return;

    if (!OPFSReplicaService.isAvailable()) {
      this.unavailable = true;
      this.initialized = true;
      return;
    }

    try {
      this.root = await navigator.storage.getDirectory() as FileSystemDirectoryHandle;
      await this.loadMetadataIndex();
      this.initialized = true;
    } catch {
      this.unavailable = true;
      this.initialized = true;
    }
  }

  /**
   * Creates or updates the entire replica from a file manifest.
   * Returns the count of files written and their total byte size.
   */
  async initReplica(
    manifest: OPFSManifestEntry[],
  ): Promise<{ fileCount: number; totalSize: number }> {
    if (!this.isReady()) return { fileCount: 0, totalSize: 0 };

    let fileCount = 0;
    let totalSize = 0;

    for (const entry of manifest) {
      const content = entry.content ?? '';
      const bytes = new TextEncoder().encode(content).length;

      await this.writeFile(entry.path, content);

      this.metadataIndex.set(
        entry.path,
        defaultMetadata({ ...entry.metadata, size: bytes }),
      );

      fileCount++;
      totalSize += bytes;
    }

    await this.persistMetadataIndex();
    return { fileCount, totalSize };
  }

  /**
   * Applies a single patch operation to the replica.
   *
   * - **upsert**: Write the content (string or base64) and update metadata.
   * - **delete**: Remove the file and its metadata.
   */
  async applyPatch(patch: OPFSPatchOp): Promise<void> {
    if (!this.isReady()) return;

    if (patch.op === 'delete') {
      await this.deleteFile(patch.path);
      this.metadataIndex.delete(patch.path);
    } else {
      // upsert
      let content: string;
      if (patch.content !== undefined) {
        content = patch.content;
      } else if (patch.content_base64 !== undefined) {
        content = base64Decode(patch.content_base64);
      } else {
        content = '';
      }

      const bytes = new TextEncoder().encode(content).length;

      await this.writeFile(patch.path, content);

      const existingMeta = this.metadataIndex.get(patch.path) ?? defaultMetadata();
      const merged = this.mergeMetadata(existingMeta, {
        ...patch.metadata,
        size: bytes,
      });
      // mergeMetadata's zero-gate prevents size=0 from overwriting a stale
      // nonzero value, but a genuinely empty file must record size 0.
      // Override here since applyPatch knows the true byte count.
      merged.size = bytes;
      this.metadataIndex.set(patch.path, defaultMetadata(merged));
    }

    await this.persistMetadataIndex();
  }

  /**
   * Reads a file from the OPFS replica.
   *
   * Returns `exists: false` if the file is not present or OPFS is
   * unavailable.
   */
  async getFile(path: string): Promise<{
    exists: boolean;
    content: string | null;
    metadata: OPFSFileMetadata | null;
  }> {
    if (!this.isReady()) {
      return { exists: false, content: null, metadata: null };
    }

    try {
      const content = await this.readFile(path);
      if (content === null) {
        return { exists: false, content: null, metadata: null };
      }
      return {
        exists: true,
        content,
        metadata: this.metadataIndex.get(path) ?? null,
      };
    } catch {
      return { exists: false, content: null, metadata: null };
    }
  }

  /**
   * Returns a summary of the current replica state.
   */
  getStatus(): OPFSReplicaStatus {
    let lastSynced = 0;

    for (const meta of this.metadataIndex.values()) {
      if (meta.lastSynced > lastSynced) {
        lastSynced = meta.lastSynced;
      }
    }

    const totalSize = Array.from(this.metadataIndex.values()).reduce(
      (sum, m) => sum + m.size,
      0,
    );

    return {
      fileCount: this.metadataIndex.size,
      totalSize,
      lastSyncTimestamp: lastSynced > 0 ? new Date(lastSynced).toISOString() : null,
    };
  }

  /**
   * Merges partial metadata for a file into the in-memory index and
   * persists it.
   */
  async storeMetadata(
    path: string,
    metadata: Partial<OPFSFileMetadata>,
  ): Promise<void> {
    if (!this.isReady()) return;

    const existing = this.metadataIndex.get(path);
    if (!existing) {
      this.metadataIndex.set(path, defaultMetadata(metadata));
      await this.persistMetadataIndex();
      return;
    }

    this.metadataIndex.set(
      path,
      defaultMetadata(this.mergeMetadata(existing, metadata)),
    );
    await this.persistMetadataIndex();
  }

  /**
   * Merges `partial` into `existing` using a non-zero-gate pattern aligned
   * with the Go sync protocol. Only fields that are explicitly present in
   * `partial` AND non-zero (for numbers) or non-empty (for strings) will
   * overwrite the existing value. This prevents accidental zeroing of
   * sequence counters and preserves untouched metadata fields.
   */
  private mergeMetadata(
    existing: OPFSFileMetadata,
    partial: Partial<OPFSFileMetadata>,
  ): OPFSFileMetadata {
    const result = { ...existing };
    if (partial.browserSeq !== undefined && partial.browserSeq !== 0) {
      result.browserSeq = partial.browserSeq;
    }
    if (partial.containerSeq !== undefined && partial.containerSeq !== 0) {
      result.containerSeq = partial.containerSeq;
    }
    if (partial.lastSynced !== undefined && partial.lastSynced !== 0) {
      result.lastSynced = partial.lastSynced;
    }
    if (partial.size !== undefined && partial.size !== 0) {
      result.size = partial.size;
    }
    if (partial.modifiedAt !== undefined && partial.modifiedAt !== '') {
      result.modifiedAt = partial.modifiedAt;
    }
    return result;
  }

  // ------------------------------------------------------------------
  // Private helpers
  // ------------------------------------------------------------------

  /** True when init has been called and OPFS is usable. */
  private isReady(): boolean {
    return this.initialized && !this.unavailable && this.root !== null;
  }

  /**
   * Serialises the in-memory metadata index to a JSON file inside OPFS.
   */
  private async persistMetadataIndex(): Promise<void> {
    if (!this.root) return;

    const serialized: SerializedMetadataIndex = {};
    for (const [path, meta] of this.metadataIndex) {
      serialized[path] = meta;
    }

    const json = JSON.stringify(serialized, null, 2);
    await this.writeFile(METADATA_INDEX_PATH, json);
  }

  /**
   * Loads the metadata index from OPFS and populates the in-memory Map.
   */
  private async loadMetadataIndex(): Promise<void> {
    if (!this.root) return;

    try {
      const content = await this.readFile(METADATA_INDEX_PATH);
      if (content === null || content.trim() === '') {
        this.metadataIndex = new Map();
        return;
      }

      const serialized: SerializedMetadataIndex = JSON.parse(content);
      this.metadataIndex = new Map(
        Object.entries(serialized).map(([path, meta]) => [path, meta]),
      );
    } catch {
      // Corrupt or missing index — start fresh
      this.metadataIndex = new Map();
    }
  }

  /**
   * Writes a UTF-8 string to OPFS, creating intermediate directories
   * as needed.
   */
  private async writeFile(path: string, content: string): Promise<void> {
    if (!this.root) return;

    const { dir, fileName } = await this.getDirectoryForPath(path);
    const handle = await dir.getFileHandle(fileName, { create: true });
    const writable = await handle.createWritable();
    await writable.write(content);
    await writable.close();
  }

  /**
   * Reads a UTF-8 string from OPFS. Returns `null` if the file doesn't
   * exist.
   */
  private async readFile(path: string): Promise<string | null> {
    if (!this.root) return null;

    try {
      const { dir, fileName } = await this.getDirectoryForPath(path);
      const handle = await dir.getFileHandle(fileName, { create: false });
      const file = await handle.getFile();
      return await file.text();
    } catch {
      return null;
    }
  }

  /**
   * Deletes a file from OPFS. Silently succeeds if the file doesn't
   * exist.
   */
  private async deleteFile(path: string): Promise<void> {
    if (!this.root) return;

    try {
      const { dir, fileName } = await this.getDirectoryForPath(path);
      await dir.removeEntry(fileName);
    } catch {
      // File may not exist — ignore
    }
  }

  /**
   * Traverses the OPFS tree to get (or create) the directory that
   * contains the given file, then returns it along with the bare file
   * name.
   */
  private async getDirectoryForPath(filePath: string): Promise<{
    dir: FileSystemDirectoryHandle;
    fileName: string;
  }> {
    if (!this.root) {
      throw new Error('OPFS not initialized');
    }

    const { dirPath, fileName } = splitPath(filePath);
    let current: FileSystemDirectoryHandle = this.root;

    const parts = dirPath.split('/').filter(Boolean);
    for (const part of parts) {
      current = await current.getDirectoryHandle(part, { create: true });
    }

    return { dir: current, fileName };
  }
}

// ----------------------------------------------------------------------
// Singleton instance
// ----------------------------------------------------------------------

/**
 * Singleton instance of the OPFS replica service. Import and use this
 * directly across the application.
 */
export const opfsReplicaService = new OPFSReplicaService();
