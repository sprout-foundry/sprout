/**
 * Role CRUD API module.
 *
 * Each function accepts a fetch function as its first parameter,
 * following the same transport-agnostic pattern as other API modules.
 */

export interface RoleConfig {
  name: string;
  description?: string;
  system_prompt?: string;
  temperature?: number;
  max_tokens?: number;
  allowed_tools?: string[];
  persona?: string;
}

export async function listRoles(fetchFn: typeof fetch): Promise<RoleConfig[]> {
  const response = await fetchFn('/api/roles');
  if (!response.ok) {
    throw new Error(`Failed to list roles: ${response.status} ${response.statusText}`);
  }
  return await response.json();
}

export async function getRole(fetchFn: typeof fetch, name: string): Promise<RoleConfig> {
  const response = await fetchFn(`/api/roles/${encodeURIComponent(name)}`);
  if (!response.ok) {
    throw new Error(`Failed to get role '${name}': ${response.status} ${response.statusText}`);
  }
  return await response.json();
}

export async function createRole(fetchFn: typeof fetch, role: RoleConfig): Promise<RoleConfig> {
  const response = await fetchFn('/api/roles', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(role),
  });
  if (!response.ok) {
    throw new Error(`Failed to create role '${role.name}': ${response.status} ${response.statusText}`);
  }
  return await response.json();
}

export async function updateRole(fetchFn: typeof fetch, name: string, role: RoleConfig): Promise<RoleConfig> {
  const response = await fetchFn(`/api/roles/${encodeURIComponent(name)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(role),
  });
  if (!response.ok) {
    throw new Error(`Failed to update role '${name}': ${response.status} ${response.statusText}`);
  }
  return await response.json();
}

export async function deleteRole(fetchFn: typeof fetch, name: string): Promise<void> {
  const response = await fetchFn(`/api/roles/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  });
  if (!response.ok) {
    throw new Error(`Failed to delete role '${name}': ${response.status} ${response.statusText}`);
  }
}
