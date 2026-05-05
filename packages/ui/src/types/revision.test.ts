import { normalizeRevision, buildRevisionFileKey } from './revision';
import type { Revision, RevisionFile } from './revision';

describe('normalizeRevision', () => {
  describe('valid data', () => {
    it('normalizes complete revision data', () => {
      const raw = {
        revision_id: 'rev-123',
        timestamp: '2024-01-01T10:00:00Z',
        files: [
          {
            file_revision_hash: 'hash-1',
            path: '/path/to/file.ts',
            operation: 'edited',
            lines_added: 10,
            lines_deleted: 5,
          },
        ],
        description: 'Test revision',
      };

      const result = normalizeRevision(raw);

      expect(result.revision_id).toBe('rev-123');
      expect(result.timestamp).toBe('2024-01-01T10:00:00Z');
      expect(result.files).toHaveLength(1);
      expect(result.files[0].path).toBe('/path/to/file.ts');
      expect(result.files[0].operation).toBe('edited');
      expect(result.files[0].lines_added).toBe(10);
      expect(result.files[0].lines_deleted).toBe(5);
      expect(result.description).toBe('Test revision');
    });

    it('normalizes revision with multiple files', () => {
      const raw = {
        revision_id: 'rev-456',
        timestamp: '2024-01-01T10:00:00Z',
        files: [
          { path: '/file1.ts', operation: 'added', lines_added: 10, lines_deleted: 0 },
          { path: '/file2.ts', operation: 'deleted', lines_added: 0, lines_deleted: 5 },
        ],
        description: 'Multiple files',
      };

      const result = normalizeRevision(raw);

      expect(result.files).toHaveLength(2);
    });
  });

  describe('missing/null data', () => {
    it('returns default revision for null input', () => {
      const result = normalizeRevision(null);

      expect(result.revision_id).toBe('unknown');
      expect(result.files).toEqual([]);
      expect(result.description).toBe('');
      expect(result.timestamp).toBeDefined();
    });

    it('returns default revision for undefined input', () => {
      const result = normalizeRevision(undefined);

      expect(result.revision_id).toBe('unknown');
      expect(result.files).toEqual([]);
      expect(result.description).toBe('');
    });

    it('returns default revision for empty object', () => {
      const result = normalizeRevision({});

      expect(result.revision_id).toBe('unknown');
      expect(result.files).toEqual([]);
      expect(result.description).toBe('');
    });
  });

  describe('field defaults', () => {
    it('defaults missing revision_id', () => {
      const result = normalizeRevision({
        timestamp: '2024-01-01T10:00:00Z',
        files: [],
        description: 'Test',
      });

      expect(result.revision_id).toBe('unknown');
    });

    it('defaults missing timestamp', () => {
      const result = normalizeRevision({
        revision_id: 'rev-123',
        files: [],
        description: 'Test',
      });

      expect(result.timestamp).toBeDefined();
      expect(typeof result.timestamp).toBe('string');
    });

    it('defaults missing description', () => {
      const result = normalizeRevision({
        revision_id: 'rev-123',
        timestamp: '2024-01-01T10:00:00Z',
        files: [],
      });

      expect(result.description).toBe('');
    });

    it('defaults missing files array', () => {
      const result = normalizeRevision({
        revision_id: 'rev-123',
        timestamp: '2024-01-01T10:00:00Z',
        description: 'Test',
      });

      expect(result.files).toEqual([]);
    });
  });

  describe('file normalization', () => {
    it('normalizes file with missing hash', () => {
      const raw = {
        revision_id: 'rev-123',
        timestamp: '2024-01-01T10:00:00Z',
        files: [
          {
            path: '/file.ts',
            operation: 'edited',
            lines_added: 5,
            lines_deleted: 3,
          },
        ],
        description: 'Test',
      };

      const result = normalizeRevision(raw);

      expect(result.files[0].file_revision_hash).toBeUndefined();
    });

    it('defaults missing file path', () => {
      const raw = {
        revision_id: 'rev-123',
        timestamp: '2024-01-01T10:00:00Z',
        files: [
          {
            operation: 'edited',
            lines_added: 5,
            lines_deleted: 3,
          },
        ],
        description: 'Test',
      };

      const result = normalizeRevision(raw);

      expect(result.files[0].path).toBe('Unknown');
    });

    it('defaults missing operation', () => {
      const raw = {
        revision_id: 'rev-123',
        timestamp: '2024-01-01T10:00:00Z',
        files: [
          {
            path: '/file.ts',
            lines_added: 5,
            lines_deleted: 3,
          },
        ],
        description: 'Test',
      };

      const result = normalizeRevision(raw);

      expect(result.files[0].operation).toBe('edited');
    });

    it('defaults lines_added to 0', () => {
      const raw = {
        revision_id: 'rev-123',
        timestamp: '2024-01-01T10:00:00Z',
        files: [
          {
            path: '/file.ts',
            operation: 'deleted',
            lines_deleted: 3,
          },
        ],
        description: 'Test',
      };

      const result = normalizeRevision(raw);

      expect(result.files[0].lines_added).toBe(0);
    });

    it('defaults lines_deleted to 0', () => {
      const raw = {
        revision_id: 'rev-123',
        timestamp: '2024-01-01T10:00:00Z',
        files: [
          {
            path: '/file.ts',
            operation: 'added',
            lines_added: 5,
          },
        ],
        description: 'Test',
      };

      const result = normalizeRevision(raw);

      expect(result.files[0].lines_deleted).toBe(0);
    });

    it('handles non-array files', () => {
      const raw = {
        revision_id: 'rev-123',
        timestamp: '2024-01-01T10:00:00Z',
        files: 'not an array',
        description: 'Test',
      };

      const result = normalizeRevision(raw);

      expect(result.files).toEqual([]);
    });
  });

  describe('type coercion', () => {
    it('coerces string numbers to numbers for lines', () => {
      const raw = {
        revision_id: 'rev-123',
        timestamp: '2024-01-01T10:00:00Z',
        files: [
          {
            path: '/file.ts',
            operation: 'edited',
            lines_added: '10' as any,
            lines_deleted: '5' as any,
          },
        ],
        description: 'Test',
      };

      const result = normalizeRevision(raw);

      expect(result.files[0].lines_added).toBe(10);
      expect(result.files[0].lines_deleted).toBe(5);
    });

    it('handles null/undefined for numeric fields', () => {
      const raw = {
        revision_id: 'rev-123',
        timestamp: '2024-01-01T10:00:00Z',
        files: [
          {
            path: '/file.ts',
            operation: 'edited',
            lines_added: null as any,
            lines_deleted: undefined as any,
          },
        ],
        description: 'Test',
      };

      const result = normalizeRevision(raw);

      expect(result.files[0].lines_added).toBe(0);
      expect(result.files[0].lines_deleted).toBe(0);
    });
  });
});

describe('buildRevisionFileKey', () => {
  describe('with file_revision_hash', () => {
    it('builds key with hash and index', () => {
      const file: RevisionFile = {
        file_revision_hash: 'hash-abc123',
        path: '/path/to/file.ts',
        operation: 'edited',
        lines_added: 10,
        lines_deleted: 5,
      };

      const key = buildRevisionFileKey(file, 0);

      expect(key).toBe('hash-abc123::0');
    });

    it('uses different index for each file', () => {
      const file: RevisionFile = {
        file_revision_hash: 'hash-abc123',
        path: '/path/to/file.ts',
        operation: 'edited',
        lines_added: 10,
        lines_deleted: 5,
      };

      const key1 = buildRevisionFileKey(file, 0);
      const key2 = buildRevisionFileKey(file, 1);
      const key3 = buildRevisionFileKey(file, 2);

      expect(key1).not.toBe(key2);
      expect(key2).not.toBe(key3);
      expect(key3).not.toBe(key1);
    });

    it('works with RevisionDetailFile', () => {
      const file: RevisionFile & { diff?: string } = {
        file_revision_hash: 'hash-xyz789',
        path: '/file.ts',
        operation: 'edited',
        lines_added: 5,
        lines_deleted: 3,
        diff: '--- a/file.ts\n+++ b/file.ts',
      };

      const key = buildRevisionFileKey(file, 0);

      expect(key).toBe('hash-xyz789::0');
    });
  });

  describe('without file_revision_hash', () => {
    it('builds key with path and index', () => {
      const file: RevisionFile = {
        path: '/path/to/file.ts',
        operation: 'edited',
        lines_added: 10,
        lines_deleted: 5,
      };

      const key = buildRevisionFileKey(file, 0);

      expect(key).toBe('/path/to/file.ts::0');
    });

    it('handles empty path', () => {
      const file: RevisionFile = {
        path: '',
        operation: 'edited',
        lines_added: 10,
        lines_deleted: 5,
      };

      const key = buildRevisionFileKey(file, 0);

      expect(key).toBe('::0');
    });

    it('handles special characters in path', () => {
      const file: RevisionFile = {
        path: '/path/to/file [1].ts',
        operation: 'edited',
        lines_added: 10,
        lines_deleted: 5,
      };

      const key = buildRevisionFileKey(file, 0);

      expect(key).toBe('/path/to/file [1].ts::0');
    });
  });

  describe('index handling', () => {
    it('includes index in key', () => {
      const file: RevisionFile = {
        path: '/file.ts',
        operation: 'edited',
        lines_added: 10,
        lines_deleted: 5,
      };

      const key0 = buildRevisionFileKey(file, 0);
      const key5 = buildRevisionFileKey(file, 5);
      const key100 = buildRevisionFileKey(file, 100);

      expect(key0).toContain('::0');
      expect(key5).toContain('::5');
      expect(key100).toContain('::100');
    });

    it('handles negative index', () => {
      const file: RevisionFile = {
        path: '/file.ts',
        operation: 'edited',
        lines_added: 10,
        lines_deleted: 5,
      };

      const key = buildRevisionFileKey(file, -1);

      expect(key).toBe('/file.ts::-1');
    });

    it('handles large index', () => {
      const file: RevisionFile = {
        path: '/file.ts',
        operation: 'edited',
        lines_added: 10,
        lines_deleted: 5,
      };

      const key = buildRevisionFileKey(file, 999999);

      expect(key).toBe('/file.ts::999999');
    });
  });

  describe('key uniqueness', () => {
    it('generates unique keys for different files', () => {
      const file1: RevisionFile = {
        path: '/file1.ts',
        operation: 'edited',
        lines_added: 10,
        lines_deleted: 5,
      };
      const file2: RevisionFile = {
        path: '/file2.ts',
        operation: 'edited',
        lines_added: 10,
        lines_deleted: 5,
      };

      const key1 = buildRevisionFileKey(file1, 0);
      const key2 = buildRevisionFileKey(file2, 0);

      expect(key1).not.toBe(key2);
    });

    it('generates unique keys for same file at different indices', () => {
      const file: RevisionFile = {
        path: '/file.ts',
        operation: 'edited',
        lines_added: 10,
        lines_deleted: 5,
      };

      const key1 = buildRevisionFileKey(file, 0);
      const key2 = buildRevisionFileKey(file, 1);

      expect(key1).not.toBe(key2);
    });
  });

  describe('format', () => {
    it('always uses double colon separator', () => {
      const file: RevisionFile = {
        file_revision_hash: 'hash123',
        path: '/file.ts',
        operation: 'edited',
        lines_added: 10,
        lines_deleted: 5,
      };

      const key = buildRevisionFileKey(file, 0);

      expect(key).toContain('::');
    });

    it('places index after separator', () => {
      const file: RevisionFile = {
        path: '/file.ts',
        operation: 'edited',
        lines_added: 10,
        lines_deleted: 5,
      };

      const key = buildRevisionFileKey(file, 5);

      expect(key).toMatch(/.*::5$/);
    });
  });
});
