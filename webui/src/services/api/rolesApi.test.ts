/**
 * Tests for the Roles API module.
 *
 * Covers all 5 CRUD functions with mock fetch, verifying:
 * - Correct HTTP method and URL
 * - JSON body serialization for create/update
 * - Error throwing on non-ok responses
 * - encodeURIComponent usage for role names with special characters
 */

import { describe, test, expect, vi, beforeEach } from 'vitest';
import * as rolesApi from './rolesApi';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeMockResponse(
  status: number,
  statusText: string,
  body: unknown = undefined,
): Response {
  const jsonPromise = body !== undefined ? Promise.resolve(body) : Promise.resolve(undefined);
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText,
    json: () => jsonPromise,
    headers: new Headers(),
    body: null as any,
    bodyUsed: false,
    redirected: false,
    type: 'basic' as ResponseType,
    url: '',
    clone: () => makeMockResponse(status, statusText, body),
    blob: vi.fn(),
    formData: vi.fn(),
    arrayBuffer: vi.fn(),
    text: vi.fn(),
  } as Response;
}

// ---------------------------------------------------------------------------
// listRoles
// ---------------------------------------------------------------------------

describe('listRoles', () => {
  beforeEach(() => vi.clearAllMocks());

  test('calls GET /api/roles and returns the roles array', async () => {
    const roles = [
      { name: 'agent', description: 'Default agent' },
      { name: 'coder', description: 'Coding role' },
    ];
    const mockFetch = vi.fn().mockResolvedValue(makeMockResponse(200, 'OK', roles));

    const result = await rolesApi.listRoles(mockFetch);

    expect(mockFetch).toHaveBeenCalledWith('/api/roles');
    expect(result).toEqual(roles);
  });

  test('throws on non-ok response', async () => {
    const mockFetch = vi.fn().mockResolvedValue(makeMockResponse(500, 'Internal Server Error'));

    await expect(rolesApi.listRoles(mockFetch)).rejects.toThrow(
      'Failed to list roles: 500 Internal Server Error',
    );
  });

  test('throws on 401 response', async () => {
    const mockFetch = vi.fn().mockResolvedValue(makeMockResponse(401, 'Unauthorized'));

    await expect(rolesApi.listRoles(mockFetch)).rejects.toThrow(
      'Failed to list roles: 401 Unauthorized',
    );
  });

  test('returns empty array when API returns empty list', async () => {
    const mockFetch = vi.fn().mockResolvedValue(makeMockResponse(200, 'OK', []));

    const result = await rolesApi.listRoles(mockFetch);

    expect(result).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// getRole
// ---------------------------------------------------------------------------

describe('getRole', () => {
  beforeEach(() => vi.clearAllMocks());

  test('calls GET /api/roles/:name and returns the role', async () => {
    const role = { name: 'agent', description: 'Default agent' };
    const mockFetch = vi.fn().mockResolvedValue(makeMockResponse(200, 'OK', role));

    const result = await rolesApi.getRole(mockFetch, 'agent');

    expect(mockFetch).toHaveBeenCalledWith('/api/roles/agent');
    expect(result).toEqual(role);
  });

  test('throws on non-ok response', async () => {
    const mockFetch = vi.fn().mockResolvedValue(makeMockResponse(404, 'Not Found'));

    await expect(rolesApi.getRole(mockFetch, 'missing')).rejects.toThrow(
      "Failed to get role 'missing': 404 Not Found",
    );
  });

  test('encodes role names with special characters', async () => {
    const role = { name: 'my/role', description: 'Has slash' };
    const mockFetch = vi.fn().mockResolvedValue(makeMockResponse(200, 'OK', role));

    await rolesApi.getRole(mockFetch, 'my/role');

    expect(mockFetch).toHaveBeenCalledWith('/api/roles/my%2Frole');
  });
});

// ---------------------------------------------------------------------------
// createRole
// ---------------------------------------------------------------------------

describe('createRole', () => {
  beforeEach(() => vi.clearAllMocks());

  test('calls POST /api/roles with JSON body and returns the created role', async () => {
    const role = { name: 'coder', description: 'Code writer', temperature: 0.7 };
    const mockFetch = vi.fn().mockResolvedValue(makeMockResponse(201, 'Created', role));

    const result = await rolesApi.createRole(mockFetch, role);

    expect(mockFetch).toHaveBeenCalledWith('/api/roles', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(role),
    });
    expect(result).toEqual(role);
  });

  test('throws on non-ok response', async () => {
    const mockFetch = vi.fn().mockResolvedValue(makeMockResponse(400, 'Bad Request'));

    await expect(
      rolesApi.createRole(mockFetch, { name: 'duplicate' }),
    ).rejects.toThrow(
      "Failed to create role 'duplicate': 400 Bad Request",
    );
  });

  test('serializes role with all fields', async () => {
    const role = {
      name: 'full',
      description: 'Full role',
      system_prompt: 'You are...',
      temperature: 0.5,
      max_tokens: 1024,
      allowed_tools: ['read_file', 'shell_command'],
      persona: 'coder',
    };
    const mockFetch = vi.fn().mockResolvedValue(makeMockResponse(201, 'Created', role));

    await rolesApi.createRole(mockFetch, role);

    expect(mockFetch).toHaveBeenCalledWith('/api/roles', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(role),
    });
  });

  test('serializes role with optional fields omitted', async () => {
    const role = { name: 'minimal' };
    const mockFetch = vi.fn().mockResolvedValue(makeMockResponse(201, 'Created', role));

    await rolesApi.createRole(mockFetch, role);

    expect(mockFetch).toHaveBeenCalledWith('/api/roles', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'minimal' }),
    });
  });
});

// ---------------------------------------------------------------------------
// updateRole
// ---------------------------------------------------------------------------

describe('updateRole', () => {
  beforeEach(() => vi.clearAllMocks());

  test('calls PUT /api/roles/:name with JSON body and returns the updated role', async () => {
    const role = { name: 'agent', description: 'Updated agent' };
    const mockFetch = vi.fn().mockResolvedValue(makeMockResponse(200, 'OK', role));

    const result = await rolesApi.updateRole(mockFetch, 'agent', role);

    expect(mockFetch).toHaveBeenCalledWith('/api/roles/agent', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(role),
    });
    expect(result).toEqual(role);
  });

  test('throws on non-ok response', async () => {
    const mockFetch = vi.fn().mockResolvedValue(makeMockResponse(404, 'Not Found'));

    await expect(
      rolesApi.updateRole(mockFetch, 'missing', { name: 'missing' }),
    ).rejects.toThrow(
      "Failed to update role 'missing': 404 Not Found",
    );
  });

  test('encodes role names with special characters', async () => {
    const role = { name: 'my/role', description: 'Updated' };
    const mockFetch = vi.fn().mockResolvedValue(makeMockResponse(200, 'OK', role));

    await rolesApi.updateRole(mockFetch, 'my/role', role);

    expect(mockFetch).toHaveBeenCalledWith('/api/roles/my%2Frole', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(role),
    });
  });
});

// ---------------------------------------------------------------------------
// deleteRole
// ---------------------------------------------------------------------------

describe('deleteRole', () => {
  beforeEach(() => vi.clearAllMocks());

  test('calls DELETE /api/roles/:name and resolves without error', async () => {
    const mockFetch = vi.fn().mockResolvedValue(makeMockResponse(204, 'No Content'));

    const result = await rolesApi.deleteRole(mockFetch, 'agent');

    expect(mockFetch).toHaveBeenCalledWith('/api/roles/agent', { method: 'DELETE' });
    expect(result).toBeUndefined();
  });

  test('throws on non-ok response', async () => {
    const mockFetch = vi.fn().mockResolvedValue(makeMockResponse(500, 'Internal Server Error'));

    await expect(rolesApi.deleteRole(mockFetch, 'agent')).rejects.toThrow(
      "Failed to delete role 'agent': 500 Internal Server Error",
    );
  });

  test('encodes role names with special characters', async () => {
    const mockFetch = vi.fn().mockResolvedValue(makeMockResponse(204, 'No Content'));

    await rolesApi.deleteRole(mockFetch, 'my/role');

    expect(mockFetch).toHaveBeenCalledWith('/api/roles/my%2Frole', { method: 'DELETE' });
  });
});
