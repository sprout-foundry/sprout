/**
 * Tests for cloudSessionHandlers — the CloudAdapter session-endpoint bridge.
 *
 * Verifies that the handler returns response shapes compatible with the
 * existing sessionApi.ts callers (SessionsResponse / SessionRestoreResponse).
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

// localStorage mock
function createStorageMock(): Storage {
  const store: Record<string, string> = {};
  return {
    length: 0,
    clear: () => Object.keys(store).forEach((k) => delete store[k]),
    getItem: (key: string) => (key in store ? store[key] : null),
    key: (i: number) => Object.keys(store)[i] ?? null,
    removeItem: (key: string) => {
      delete store[key];
    },
    setItem: (key: string, value: string) => {
      store[key] = value;
    },
  } as unknown as Storage;
}

import { clearAllSessions, resetActiveSessionId, saveSession } from './cloudSessionStore';
import type { Message } from '../types/app';
import { handleCloudSessionsEndpoint } from './cloudSessionHandlers';

function userMsg(content: string): Message {
  return { id: `u-${content}`, type: 'user', content, timestamp: new Date('2024-01-01T00:00:00Z') };
}
function assistantMsg(content: string): Message {
  return { id: `a-${content}`, type: 'assistant', content, timestamp: new Date('2024-01-02T00:00:00Z') };
}

describe('cloudSessionHandlers', () => {
  let originalLocalStorage: Storage | undefined;

  beforeEach(() => {
    originalLocalStorage = Object.getOwnPropertyDescriptor(window, 'localStorage')?.value as Storage | undefined;
    Object.defineProperty(window, 'localStorage', {
      value: createStorageMock(),
      configurable: true,
      writable: true,
    });
    resetActiveSessionId();
  });

  afterEach(() => {
    if (originalLocalStorage) {
      Object.defineProperty(window, 'localStorage', {
        value: originalLocalStorage,
        configurable: true,
        writable: true,
      });
    }
    clearAllSessions();
    vi.clearAllMocks();
  });

  describe('handleCloudSessionsEndpoint routing', () => {
    it('returns null for an unrecognised sessions sub-path', () => {
      const res = handleCloudSessionsEndpoint('/api/sessions/search', 'GET', '/api/sessions/search?q=x', undefined);
      expect(res).toBeNull();
    });

    it('returns null for an unsupported method on /api/sessions', () => {
      const res = handleCloudSessionsEndpoint('/api/sessions', 'DELETE', '/api/sessions', undefined);
      expect(res).toBeNull();
    });
  });

  describe('GET /api/sessions', () => {
    it('returns an empty list when nothing is persisted', async () => {
      const res = handleCloudSessionsEndpoint('/api/sessions', 'GET', '/api/sessions', undefined)!;
      expect(res.status).toBe(200);
      const body = await res.json();
      expect(body.sessions).toEqual([]);
      expect(body.current_session_id).toBe('');
    });

    it('lists persisted sessions with scope=current ignored (returns all)', async () => {
      const id = saveSession([userMsg('hello'), assistantMsg('hi')]);
      const res = handleCloudSessionsEndpoint(
        '/api/sessions',
        'GET',
        '/api/sessions?scope=current',
        undefined,
      )!;
      const body = await res.json();
      expect(body.sessions).toHaveLength(1);
      expect(body.sessions[0].session_id).toBe(id);
      expect(body.sessions[0].name).toBe('hello');
      expect(body.sessions[0].message_count).toBe(2);
      expect(body.current_session_id).toBe(id);
    });
  });

  describe('POST /api/sessions/restore', () => {
    it('returns the transcript in the { role, content } wire shape', async () => {
      const id = saveSession([userMsg('q'), assistantMsg('a')]);
      const res = handleCloudSessionsEndpoint(
        '/api/sessions/restore',
        'POST',
        '/api/sessions/restore',
        JSON.stringify({ session_id: id }),
      )!;
      expect(res.status).toBe(200);
      const body = await res.json();
      expect(body.session_id).toBe(id);
      expect(body.messages).toEqual([
        { role: 'user', content: 'q' },
        { role: 'assistant', content: 'a' },
      ]);
      expect(body.message_count).toBe(2);
    });

    it('returns 400 when the body is missing', () => {
      const res = handleCloudSessionsEndpoint('/api/sessions/restore', 'POST', '/api/sessions/restore', undefined)!;
      expect(res.status).toBe(400);
    });

    it('returns 400 when session_id is missing', () => {
      const res = handleCloudSessionsEndpoint(
        '/api/sessions/restore',
        'POST',
        '/api/sessions/restore',
        JSON.stringify({}),
      )!;
      expect(res.status).toBe(400);
    });

    it('returns 404 for an unknown session id', () => {
      const res = handleCloudSessionsEndpoint(
        '/api/sessions/restore',
        'POST',
        '/api/sessions/restore',
        JSON.stringify({ session_id: 'ghost' }),
      )!;
      expect(res.status).toBe(404);
    });
  });

  describe('DELETE /api/sessions/{id}', () => {
    it('removes the session and returns ok', async () => {
      const id = saveSession([userMsg('gone')]);
      const res = handleCloudSessionsEndpoint(`/api/sessions/${id}`, 'DELETE', `/api/sessions/${id}`, undefined)!;
      expect(res.status).toBe(200);
      // Subsequent list should be empty.
      const listRes = handleCloudSessionsEndpoint('/api/sessions', 'GET', '/api/sessions', undefined)!;
      expect((await listRes.json()).sessions).toHaveLength(0);
    });

    it('succeeds even when the id was already absent (idempotent)', async () => {
      const res = handleCloudSessionsEndpoint(
        '/api/sessions/ghost',
        'DELETE',
        '/api/sessions/ghost',
        undefined,
      )!;
      expect(res.status).toBe(200);
    });

    it('accepts a POST /api/sessions/delete with a JSON body', async () => {
      const id = saveSession([userMsg('bye')]);
      const res = handleCloudSessionsEndpoint(
        '/api/sessions/delete',
        'POST',
        '/api/sessions/delete',
        JSON.stringify({ session_id: id }),
      )!;
      expect(res.status).toBe(200);
      const listRes = handleCloudSessionsEndpoint('/api/sessions', 'GET', '/api/sessions', undefined)!;
      expect((await listRes.json()).sessions).toHaveLength(0);
    });
  });
});
