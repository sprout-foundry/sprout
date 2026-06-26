/**
 * Tests for OPFSReplicaService
 *
 * Tests all public methods by stubbing internal storage methods.
 * The OPFS API (navigator.storage.getDirectory()) is mocked to
 * return a controllable in-memory filesystem, and the service's
 * private file I/O is intercepted via method replacement.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// ---------------------------------------------------------------------------
// Imports
// ---------------------------------------------------------------------------

import {
  OPFSReplicaService,
  type OPFSFileMetadata,
  type OPFSManifestEntry,
  type OPFSPatchOp,
  type OPFSReplicaStatus,
  opfsReplicaService,
} from './opfsReplica';

// ---------------------------------------------------------------------------
// Mock filesystem — in-memory key-value store
// ---------------------------------------------------------------------------

/** File entry stored in the mock filesystem. */
interface MockFileEntry {
  content: string;
}

/**
 * In-memory filesystem using a flat Map keyed by path string.
 * Used to back the mocked OPFS operations.
 */
class MockFS {
  files = new Map<string, MockFileEntry>();

  reset() {
    this.files.clear();
  }

  writeFile(path: string, content: string): void {
    this.files.set(path, { content });
  }

  readFile(path: string): MockFileEntry | null {
    return this.files.get(path) ?? null;
  }

  removeFile(path: string): boolean {
    return this.files.delete(path);
  }

  hasFile(path: string): boolean {
    return this.files.has(path);
  }
}

/** Shared mock filesystem instance. */
const mockFs = new MockFS();

// ---------------------------------------------------------------------------
// Helpers — mock navigator.storage for isAvailable checks
// ---------------------------------------------------------------------------

function setupMockNavigatorStorage(mode: 'available' | 'unavailable' | 'error' = 'available') {
  if (mode === 'available') {
    (navigator as any).storage = {
      getDirectory: () => Promise.resolve({} as FileSystemDirectoryHandle),
    } as StorageManager;
  } else if (mode === 'unavailable') {
    (navigator as any).storage = {} as StorageManager;
  } else {
    (navigator as any).storage = {
      getDirectory: () => {
        throw new Error('OPFS access denied');
      },
    } as StorageManager;
  }
}

// ---------------------------------------------------------------------------
// Helper: replace the service's private methods with mock implementations
// that operate on our in-memory filesystem.
// ---------------------------------------------------------------------------

function patchServiceMethods(service: OPFSReplicaService) {
  // Replace writeFile: stores content in mockFs
  (service as any).writeFile = async (path: string, content: string) => {
    mockFs.writeFile(path, content);
  };

  // Replace readFile: reads from mockFs
  (service as any).readFile = async (path: string) => {
    const entry = mockFs.readFile(path);
    return entry ? entry.content : null;
  };

  // Replace deleteFile: removes from mockFs
  (service as any).deleteFile = async (path: string) => {
    mockFs.removeFile(path);
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('OPFSReplicaService', () => {
  let service: OPFSReplicaService;

  beforeEach(() => {
    service = new OPFSReplicaService();
    mockFs.reset();
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  // ------------------------------------------------------------------------
  // isAvailable() — static method
  // ------------------------------------------------------------------------

  describe('isAvailable()', () => {
    it('returns true when OPFS API is present', () => {
      setupMockNavigatorStorage('available');
      expect(OPFSReplicaService.isAvailable()).toBe(true);
    });

    it('returns false when navigator.storage.getDirectory is missing', () => {
      setupMockNavigatorStorage('unavailable');
      expect(OPFSReplicaService.isAvailable()).toBe(false);
    });

    it('returns false when navigator is undefined (SSR)', () => {
      const original = globalThis.navigator;
      // @ts-expect-error — intentionally deleting for SSR simulation
      delete globalThis.navigator;
      expect(OPFSReplicaService.isAvailable()).toBe(false);
      globalThis.navigator = original;
    });
  });

  // ------------------------------------------------------------------------
  // init()
  // ------------------------------------------------------------------------

  describe('init()', () => {
    it('initializes successfully with available OPFS', async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);

      const status = service.getStatus();
      expect(status.fileCount).toBe(0);
      expect(status.totalSize).toBe(0);
      expect(status.lastSyncTimestamp).toBeNull();
    });

    it('handles OPFS unavailable gracefully without throwing', async () => {
      setupMockNavigatorStorage('unavailable');
      await service.init();

      const status = service.getStatus();
      expect(status.fileCount).toBe(0);
      expect(status.totalSize).toBe(0);
    });

    it('handles OPFS access error gracefully', async () => {
      setupMockNavigatorStorage('error');
      await service.init();

      const status = service.getStatus();
      expect(status.fileCount).toBe(0);
      expect(status.totalSize).toBe(0);
    });

    it('is idempotent — calling init() twice is safe', async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);
      await service.init();

      expect(service.getStatus().fileCount).toBe(0);
    });
  });

  // ------------------------------------------------------------------------
  // initReplica()
  // ------------------------------------------------------------------------

  describe('initReplica()', () => {
    beforeEach(async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);
    });

    it('returns { fileCount: 0, totalSize: 0 } for empty manifest', async () => {
      const result = await service.initReplica([]);
      expect(result.fileCount).toBe(0);
      expect(result.totalSize).toBe(0);
    });

    it('creates files from manifest entries and returns correct counts', async () => {
      const manifest: OPFSManifestEntry[] = [
        { path: 'hello.txt', content: 'Hello, world!' },
        { path: 'foo/bar.json', content: '{"key":"value"}' },
      ];
      const result = await service.initReplica(manifest);
      expect(result.fileCount).toBe(2);
      const expectedBytes =
        new TextEncoder().encode('Hello, world!').length + new TextEncoder().encode('{"key":"value"}').length;
      expect(result.totalSize).toBe(expectedBytes);
    });

    it('creates files in nested directories', async () => {
      const manifest: OPFSManifestEntry[] = [{ path: 'a/b/c/deep.txt', content: 'deep content' }];
      const result = await service.initReplica(manifest);
      expect(result.fileCount).toBe(1);
      expect(result.totalSize).toBe(new TextEncoder().encode('deep content').length);

      // Verify the file was actually written to our mock
      const entry = mockFs.readFile('a/b/c/deep.txt');
      expect(entry).not.toBeNull();
      expect(entry!.content).toBe('deep content');
    });

    it('stores metadata for each file', async () => {
      const manifest: OPFSManifestEntry[] = [
        {
          path: 'data.txt',
          content: 'some data',
          metadata: { browserSeq: 42, containerSeq: 10 },
        },
      ];
      await service.initReplica(manifest);

      const file = await service.getFile('data.txt');
      expect(file.exists).toBe(true);
      expect(file.content).toBe('some data');
      expect(file.metadata).toBeDefined();
      expect(file.metadata!.browserSeq).toBe(42);
      expect(file.metadata!.containerSeq).toBe(10);
      expect(file.metadata!.size).toBe(new TextEncoder().encode('some data').length);
    });

    it('persists metadata index to OPFS', async () => {
      await service.initReplica([{ path: 'test.txt', content: 'test content' }]);

      // The metadata index file should exist in our mock
      const metaEntry = mockFs.readFile('.opfs-meta/index.json');
      expect(metaEntry).not.toBeNull();
      expect(metaEntry!.content).toContain('test.txt');
    });

    it('returns { fileCount: 0, totalSize: 0 } when service is not ready', async () => {
      // Fresh mockFs — no data from previous tests
      mockFs.reset();
      setupMockNavigatorStorage('unavailable');
      const freshService = new OPFSReplicaService();
      await freshService.init();

      const result = await freshService.initReplica([{ path: 'test.txt', content: 'content' }]);
      expect(result.fileCount).toBe(0);
      expect(result.totalSize).toBe(0);
    });

    it('handles manifest entry with empty content', async () => {
      const manifest: OPFSManifestEntry[] = [{ path: 'empty.txt', content: '' }];
      const result = await service.initReplica(manifest);
      expect(result.fileCount).toBe(1);
      expect(result.totalSize).toBe(0);

      const entry = mockFs.readFile('empty.txt');
      expect(entry).not.toBeNull();
      expect(entry!.content).toBe('');
    });

    it('handles manifest with all metadata fields', async () => {
      const manifest: OPFSManifestEntry[] = [
        {
          path: 'full-meta.txt',
          content: 'data',
          metadata: {
            browserSeq: 100,
            containerSeq: 50,
            lastSynced: 1700000000000,
            size: 999,
            modifiedAt: '2024-01-01T00:00:00Z',
          },
        },
      ];
      await service.initReplica(manifest);

      const file = await service.getFile('full-meta.txt');
      expect(file.metadata!.browserSeq).toBe(100);
      expect(file.metadata!.containerSeq).toBe(50);
      expect(file.metadata!.lastSynced).toBe(1700000000000);
      // size is overridden by actual content length
      expect(file.metadata!.size).toBe(new TextEncoder().encode('data').length);
      expect(file.metadata!.modifiedAt).toBe('2024-01-01T00:00:00Z');
    });
  });

  // ------------------------------------------------------------------------
  // applyPatch() — upsert
  // ------------------------------------------------------------------------

  describe('applyPatch() — upsert', () => {
    beforeEach(async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);
    });

    it('creates new file with string content', async () => {
      await service.applyPatch({
        op: 'upsert',
        path: 'new.txt',
        content: 'new content',
      });

      const entry = mockFs.readFile('new.txt');
      expect(entry).not.toBeNull();
      expect(entry!.content).toBe('new content');

      const file = await service.getFile('new.txt');
      expect(file.exists).toBe(true);
      expect(file.content).toBe('new content');
    });

    it('creates new file with base64 content', async () => {
      const originalText = 'Hello from base64!';
      const base64 = btoa(originalText);
      await service.applyPatch({
        op: 'upsert',
        path: 'base64.txt',
        content_base64: base64,
      });

      const entry = mockFs.readFile('base64.txt');
      expect(entry).not.toBeNull();
      expect(entry!.content).toBe(originalText);
    });

    it('updates existing file content', async () => {
      await service.applyPatch({
        op: 'upsert',
        path: 'update.txt',
        content: 'original',
      });
      await service.applyPatch({
        op: 'upsert',
        path: 'update.txt',
        content: 'updated',
      });

      const entry = mockFs.readFile('update.txt');
      expect(entry!.content).toBe('updated');

      const file = await service.getFile('update.txt');
      expect(file.content).toBe('updated');
    });

    it('prefers content over content_base64 when both provided', async () => {
      await service.applyPatch({
        op: 'upsert',
        path: 'both.txt',
        content: 'plain content',
        content_base64: btoa('base64 content'),
      });

      const entry = mockFs.readFile('both.txt');
      expect(entry!.content).toBe('plain content');
    });

    it('creates empty file when neither content nor content_base64 provided', async () => {
      await service.applyPatch({
        op: 'upsert',
        path: 'empty.txt',
      });

      const entry = mockFs.readFile('empty.txt');
      expect(entry).not.toBeNull();
      expect(entry!.content).toBe('');
    });

    it('updates metadata on upsert', async () => {
      await service.applyPatch({
        op: 'upsert',
        path: 'meta.txt',
        content: 'content',
        metadata: { browserSeq: 5, containerSeq: 3, lastSynced: 1000 },
      });

      const file = await service.getFile('meta.txt');
      expect(file.metadata).toBeDefined();
      expect(file.metadata!.browserSeq).toBe(5);
      expect(file.metadata!.containerSeq).toBe(3);
      expect(file.metadata!.lastSynced).toBe(1000);
      expect(file.metadata!.size).toBe(new TextEncoder().encode('content').length);
    });

    it('merges new metadata with existing on update', async () => {
      await service.applyPatch({
        op: 'upsert',
        path: 'merge.txt',
        content: 'first',
        metadata: { browserSeq: 1, containerSeq: 1 },
      });
      await service.applyPatch({
        op: 'upsert',
        path: 'merge.txt',
        content: 'second',
        metadata: { browserSeq: 2 },
      });

      const file = await service.getFile('merge.txt');
      expect(file.metadata!.browserSeq).toBe(2);
      expect(file.metadata!.containerSeq).toBe(1);
    });

    it('upsert into nested path creates file in nested directory', async () => {
      await service.applyPatch({
        op: 'upsert',
        path: 'deep/nested/file.txt',
        content: 'nested content',
      });

      const entry = mockFs.readFile('deep/nested/file.txt');
      expect(entry).not.toBeNull();
      expect(entry!.content).toBe('nested content');

      const file = await service.getFile('deep/nested/file.txt');
      expect(file.exists).toBe(true);
      expect(file.content).toBe('nested content');
    });

    it('does nothing when service is not ready', async () => {
      mockFs.reset();
      setupMockNavigatorStorage('unavailable');
      const freshService = new OPFSReplicaService();
      await freshService.init();

      await freshService.applyPatch({
        op: 'upsert',
        path: 'nope.txt',
        content: 'content',
      });

      expect(freshService.getStatus().fileCount).toBe(0);
    });
  });

  // ------------------------------------------------------------------------
  // applyPatch() — delete
  // ------------------------------------------------------------------------

  describe('applyPatch() — delete', () => {
    beforeEach(async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);
    });

    it('removes existing file', async () => {
      await service.applyPatch({
        op: 'upsert',
        path: 'todelete.txt',
        content: 'to be deleted',
      });
      expect(mockFs.hasFile('todelete.txt')).toBe(true);

      await service.applyPatch({ op: 'delete', path: 'todelete.txt' });
      expect(mockFs.hasFile('todelete.txt')).toBe(false);

      const after = await service.getFile('todelete.txt');
      expect(after.exists).toBe(false);
    });

    it('handles non-existent file gracefully (no error)', async () => {
      await service.applyPatch({ op: 'delete', path: 'nonexistent.txt' });
      expect(service.getStatus().fileCount).toBe(0);
    });

    it('removes metadata entry for deleted file', async () => {
      await service.applyPatch({
        op: 'upsert',
        path: 'meta.txt',
        content: 'data',
        metadata: { browserSeq: 1, containerSeq: 1 },
      });
      expect(service.getStatus().fileCount).toBe(1);

      await service.applyPatch({ op: 'delete', path: 'meta.txt' });
      expect(service.getStatus().fileCount).toBe(0);
    });

    it('does nothing when service is not ready', async () => {
      mockFs.reset();
      setupMockNavigatorStorage('unavailable');
      const freshService = new OPFSReplicaService();
      await freshService.init();

      await freshService.applyPatch({ op: 'delete', path: 'anything.txt' });
      expect(freshService.getStatus().fileCount).toBe(0);
    });
  });

  // ------------------------------------------------------------------------
  // getFile()
  // ------------------------------------------------------------------------

  describe('getFile()', () => {
    beforeEach(async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);
    });

    it('returns file content and metadata for existing file', async () => {
      await service.applyPatch({
        op: 'upsert',
        path: 'data.txt',
        content: 'some data',
        metadata: { browserSeq: 7, containerSeq: 2 },
      });

      const result = await service.getFile('data.txt');
      expect(result.exists).toBe(true);
      expect(result.content).toBe('some data');
      expect(result.metadata).not.toBeNull();
      expect(result.metadata!.browserSeq).toBe(7);
      expect(result.metadata!.containerSeq).toBe(2);
    });

    it('returns { exists: false } for non-existent file', async () => {
      const result = await service.getFile('nonexistent.txt');
      expect(result.exists).toBe(false);
      expect(result.content).toBeNull();
      expect(result.metadata).toBeNull();
    });

    it('returns { exists: false } when service is not ready', async () => {
      mockFs.reset();
      setupMockNavigatorStorage('unavailable');
      const freshService = new OPFSReplicaService();
      await freshService.init();

      const result = await freshService.getFile('anything.txt');
      expect(result.exists).toBe(false);
      expect(result.content).toBeNull();
      expect(result.metadata).toBeNull();
    });

    it('returns { exists: false } when file has never been written', async () => {
      const result = await service.getFile('never-written.txt');
      expect(result.exists).toBe(false);
    });
  });

  // ------------------------------------------------------------------------
  // getStatus()
  // ------------------------------------------------------------------------

  describe('getStatus()', () => {
    it('returns zeroed status for empty replica', () => {
      const status = service.getStatus();
      expect(status.fileCount).toBe(0);
      expect(status.totalSize).toBe(0);
      expect(status.lastSyncTimestamp).toBeNull();
    });

    it('returns correct fileCount and totalSize after initReplica', async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);

      await service.initReplica([
        { path: 'a.txt', content: 'aaa' },
        { path: 'b.txt', content: 'bbbbb' },
        { path: 'c.txt', content: 'cc' },
      ]);

      const status = service.getStatus();
      expect(status.fileCount).toBe(3);
      expect(status.totalSize).toBe(3 + 5 + 2); // 10 bytes
    });

    it('returns null lastSyncTimestamp when no files have lastSynced', async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);

      await service.initReplica([{ path: 'file.txt', content: 'data' }]);

      const status = service.getStatus();
      expect(status.lastSyncTimestamp).toBeNull();
    });

    it('returns lastSyncTimestamp when files have been synced', async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);

      const ts = 1700000000000;
      await service.initReplica([
        {
          path: 'synced.txt',
          content: 'data',
          metadata: { lastSynced: ts },
        },
      ]);

      const status = service.getStatus();
      expect(status.lastSyncTimestamp).toBe(new Date(ts).toISOString());
    });

    it('returns the most recent lastSynced timestamp across all files', async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);

      await service.initReplica([
        { path: 'old.txt', content: 'old', metadata: { lastSynced: 1000 } },
        { path: 'new.txt', content: 'new', metadata: { lastSynced: 5000 } },
      ]);

      const status = service.getStatus();
      expect(status.lastSyncTimestamp).toBe(new Date(5000).toISOString());
    });

    it('returns updated status after file deletion', async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);

      await service.initReplica([
        { path: 'a.txt', content: 'aaa' },
        { path: 'b.txt', content: 'bb' },
      ]);

      await service.applyPatch({ op: 'delete', path: 'a.txt' });
      const status = service.getStatus();
      expect(status.fileCount).toBe(1);
      expect(status.totalSize).toBe(new TextEncoder().encode('bb').length);
    });
  });

  // ------------------------------------------------------------------------
  // storeMetadata()
  // ------------------------------------------------------------------------

  describe('storeMetadata()', () => {
    beforeEach(async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);
    });

    it('stores metadata for a new file path', async () => {
      // Write the file so getFile can find it
      await service.applyPatch({
        op: 'upsert',
        path: 'newfile.txt',
        content: 'file content',
      });
      await service.storeMetadata('newfile.txt', {
        browserSeq: 99,
        containerSeq: 5,
      });

      const status = service.getStatus();
      expect(status.fileCount).toBe(1);

      const file = await service.getFile('newfile.txt');
      expect(file.exists).toBe(true);
      expect(file.metadata).not.toBeNull();
      expect(file.metadata!.browserSeq).toBe(99);
      expect(file.metadata!.containerSeq).toBe(5);
    });

    it('merges partial metadata with existing metadata', async () => {
      await service.applyPatch({
        op: 'upsert',
        path: 'existing.txt',
        content: 'data',
        metadata: { browserSeq: 1, containerSeq: 10, lastSynced: 100 },
      });

      await service.storeMetadata('existing.txt', {
        browserSeq: 2,
        lastSynced: 200,
      });

      const file = await service.getFile('existing.txt');
      expect(file.metadata!.browserSeq).toBe(2);
      expect(file.metadata!.containerSeq).toBe(10);
      expect(file.metadata!.lastSynced).toBe(200);
      expect(file.metadata!.size).toBe(new TextEncoder().encode('data').length);
    });

    it('persists metadata index to mock filesystem', async () => {
      await service.applyPatch({
        op: 'upsert',
        path: 'persist.txt',
        content: 'data',
      });
      await service.storeMetadata('persist.txt', { browserSeq: 42 });

      const metaEntry = mockFs.readFile('.opfs-meta/index.json');
      expect(metaEntry).not.toBeNull();
      expect(metaEntry!.content).toContain('persist.txt');
    });

    it('does nothing when service is not ready', async () => {
      mockFs.reset();
      setupMockNavigatorStorage('unavailable');
      const freshService = new OPFSReplicaService();
      await freshService.init();

      await freshService.storeMetadata('test.txt', { browserSeq: 1 });
      expect(freshService.getStatus().fileCount).toBe(0);
    });

    it('fills in default values for missing metadata fields', async () => {
      await service.applyPatch({
        op: 'upsert',
        path: 'defaults.txt',
        content: 'data',
      });
      await service.storeMetadata('defaults.txt', { browserSeq: 1 });

      const file = await service.getFile('defaults.txt');
      expect(file.metadata).not.toBeNull();
      expect(file.metadata!.browserSeq).toBe(1);
      expect(file.metadata!.containerSeq).toBe(0);
      expect(file.metadata!.lastSynced).toBe(0);
      expect(file.metadata!.size).toBe(new TextEncoder().encode('data').length);
      expect(file.metadata!.modifiedAt).toBeDefined();
    });

    it('overwrites existing metadata fields when new values provided', async () => {
      await service.applyPatch({
        op: 'upsert',
        path: 'overwrite.txt',
        content: 'data',
        metadata: {
          browserSeq: 1,
          containerSeq: 10,
          size: 100,
          modifiedAt: '2024-01-01T00:00:00Z',
        },
      });

      await service.storeMetadata('overwrite.txt', {
        containerSeq: 20,
        size: 200,
      });

      const file = await service.getFile('overwrite.txt');
      expect(file.metadata!.browserSeq).toBe(1);
      expect(file.metadata!.containerSeq).toBe(20);
      expect(file.metadata!.size).toBe(200);
    });
  });

  // ------------------------------------------------------------------------
  // Metadata index persistence across init
  // ------------------------------------------------------------------------

  describe('metadata index persistence', () => {
    it('loads metadata index from OPFS on re-init', async () => {
      // Initialize first service and write data
      mockFs.reset();
      (navigator as any).storage = {
        getDirectory: () => Promise.resolve({} as FileSystemDirectoryHandle),
      } as StorageManager;
      await service.init();
      patchServiceMethods(service);

      await service.applyPatch({
        op: 'upsert',
        path: 'saved.txt',
        content: 'content',
        metadata: { browserSeq: 5, containerSeq: 3 },
      });

      // Create a new service — patch BEFORE init so it reads from mockFs
      const newService = new OPFSReplicaService();
      patchServiceMethods(newService);
      // navigator already configured above, don't reset it
      await newService.init();

      const status = newService.getStatus();
      expect(status.fileCount).toBe(1);

      const file = await newService.getFile('saved.txt');
      expect(file.exists).toBe(true);
      expect(file.content).toBe('content');
      expect(file.metadata!.browserSeq).toBe(5);
      expect(file.metadata!.containerSeq).toBe(3);
    });

    it('starts fresh when metadata index is corrupt', async () => {
      setupMockNavigatorStorage('available');
      // Write corrupt JSON to the metadata index path
      mockFs.writeFile('.opfs-meta/index.json', '{corrupt json');

      const freshService = new OPFSReplicaService();
      await freshService.init();
      patchServiceMethods(freshService);

      const status = freshService.getStatus();
      expect(status.fileCount).toBe(0);
    });
  });

  // ------------------------------------------------------------------------
  // Singleton instance
  // ------------------------------------------------------------------------

  describe('opfsReplicaService singleton', () => {
    it('is an instance of OPFSReplicaService', () => {
      expect(opfsReplicaService).toBeInstanceOf(OPFSReplicaService);
    });
  });

  // ------------------------------------------------------------------------
  // Edge cases & integration scenarios
  // ------------------------------------------------------------------------

  describe('edge cases', () => {
    it('handles multiple files with same prefix', async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);

      await service.initReplica([
        { path: 'file.txt', content: 'one' },
        { path: 'file.txt.bak', content: 'two' },
        { path: 'file.txt~', content: 'three' },
      ]);

      const status = service.getStatus();
      expect(status.fileCount).toBe(3);

      const f1 = await service.getFile('file.txt');
      expect(f1.content).toBe('one');
      const f2 = await service.getFile('file.txt.bak');
      expect(f2.content).toBe('two');
      const f3 = await service.getFile('file.txt~');
      expect(f3.content).toBe('three');
    });

    it('handles special characters in file paths', async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);

      await service.applyPatch({
        op: 'upsert',
        path: 'data/config.yaml',
        content: 'key: value',
      });

      const file = await service.getFile('data/config.yaml');
      expect(file.exists).toBe(true);
      expect(file.content).toBe('key: value');
    });

    it('handles large content in upsert', async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);

      const largeContent = 'x'.repeat(100_000);
      await service.applyPatch({
        op: 'upsert',
        path: 'large.bin',
        content: largeContent,
      });

      const file = await service.getFile('large.bin');
      expect(file.content).toBe(largeContent);
    });

    it('preserves metadata during content-only update', async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);

      await service.applyPatch({
        op: 'upsert',
        path: 'preserve.txt',
        content: 'original',
        metadata: { browserSeq: 1, containerSeq: 10, lastSynced: 100 },
      });

      await service.applyPatch({
        op: 'upsert',
        path: 'preserve.txt',
        content: 'updated',
        // No metadata — should preserve existing
      });

      const file = await service.getFile('preserve.txt');
      expect(file.content).toBe('updated');
      expect(file.metadata!.browserSeq).toBe(1);
      expect(file.metadata!.containerSeq).toBe(10);
      expect(file.metadata!.lastSynced).toBe(100);
    });

    it('handles base64-encoded binary content', async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);

      // Use text that encodes cleanly to/from base64
      const originalText = 'Hello binary content!';
      const base64 = btoa(originalText);

      await service.applyPatch({
        op: 'upsert',
        path: 'binary.dat',
        content_base64: base64,
      });

      const file = await service.getFile('binary.dat');
      expect(file.exists).toBe(true);
      expect(file.content).toBe(originalText);
    });

    it('handles deeply nested path in applyPatch', async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);

      await service.applyPatch({
        op: 'upsert',
        path: 'a/b/c/d/e/f/g/file.txt',
        content: 'very deep',
      });

      const file = await service.getFile('a/b/c/d/e/f/g/file.txt');
      expect(file.exists).toBe(true);
      expect(file.content).toBe('very deep');
    });

    it('handles concurrent patch operations', async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);

      // Apply multiple patches in sequence
      await Promise.all([
        service.applyPatch({ op: 'upsert', path: 'a.txt', content: 'A' }),
        service.applyPatch({ op: 'upsert', path: 'b.txt', content: 'B' }),
        service.applyPatch({ op: 'upsert', path: 'c.txt', content: 'C' }),
      ]);

      const status = service.getStatus();
      expect(status.fileCount).toBe(3);
    });

    it('initReplica with mixed metadata completeness', async () => {
      setupMockNavigatorStorage('available');
      await service.init();
      patchServiceMethods(service);

      await service.initReplica([
        {
          path: 'full.txt',
          content: 'data',
          metadata: { browserSeq: 1, containerSeq: 2, lastSynced: 100, size: 10, modifiedAt: '2024-01-01' },
        },
        { path: 'partial.txt', content: 'data', metadata: { browserSeq: 3 } },
        { path: 'no-meta.txt', content: 'data' },
      ]);

      const full = await service.getFile('full.txt');
      expect(full.metadata!.browserSeq).toBe(1);
      expect(full.metadata!.containerSeq).toBe(2);

      const partial = await service.getFile('partial.txt');
      expect(partial.metadata!.browserSeq).toBe(3);
      expect(partial.metadata!.containerSeq).toBe(0); // default
      expect(partial.metadata!.lastSynced).toBe(0); // default

      const noMeta = await service.getFile('no-meta.txt');
      expect(noMeta.metadata).toBeDefined();
      // browserSeq defaults to Date.now() per the implementation
      expect(typeof noMeta.metadata!.browserSeq).toBe('number');
      expect(noMeta.metadata!.containerSeq).toBe(0); // default
      expect(noMeta.metadata!.lastSynced).toBe(0); // default
    });
  });
});
