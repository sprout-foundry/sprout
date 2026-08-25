/**
 * Settings domain API — adapter-aware settings operations.
 */

import type {
  SproutSettings,
  MCPSettingsResponse,
  MCPServerConfig,
  CustomProvidersResponse,
  CustomProviderConfig,
  SkillsResponse,
  SubagentTypesResponse,
  HotkeyConfig,
} from './types';

export async function getSettings(fetchFn: typeof fetch): Promise<SproutSettings> {
  const response = await fetchFn('/api/settings');
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function getSettingsLayer(
  fetchFn: typeof fetch,
  layer: 'global' | 'workspace' | 'session',
): Promise<Record<string, unknown>> {
  const response = await fetchFn(`/api/settings?layer=${layer}`);
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function getSettingsProvenance(fetchFn: typeof fetch): Promise<{ sources: Record<string, string> }> {
  const response = await fetchFn('/api/settings?layer=provenance');
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function updateSettings(
  fetchFn: typeof fetch,
  settings: Record<string, unknown>,
  layer?: 'session' | 'workspace' | 'global',
): Promise<{ message: string }> {
  const url = layer ? `/api/settings?layer=${layer}` : '/api/settings';
  const response = await fetchFn(url, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(settings),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function getMCPSettings(fetchFn: typeof fetch): Promise<MCPSettingsResponse> {
  const response = await fetchFn('/api/settings/mcp');
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function updateMCPSettings(
  fetchFn: typeof fetch,
  settingsData: MCPSettingsResponse,
): Promise<{ message: string }> {
  const response = await fetchFn('/api/settings/mcp', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(settingsData),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function addMCPServer(fetchFn: typeof fetch, server: MCPServerConfig): Promise<{ message: string }> {
  const response = await fetchFn('/api/settings/mcp/servers/', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(server),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function updateMCPServer(
  fetchFn: typeof fetch,
  name: string,
  server: MCPServerConfig,
): Promise<{ message: string }> {
  const response = await fetchFn(`/api/settings/mcp/servers/${encodeURIComponent(name)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(server),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function deleteMCPServer(fetchFn: typeof fetch, name: string): Promise<{ message: string }> {
  const response = await fetchFn(`/api/settings/mcp/servers/${encodeURIComponent(name)}`, { method: 'DELETE' });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function getCustomProviders(fetchFn: typeof fetch): Promise<CustomProvidersResponse> {
  const response = await fetchFn('/api/settings/providers');
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function addCustomProvider(
  fetchFn: typeof fetch,
  provider: CustomProviderConfig,
): Promise<{ message: string }> {
  const response = await fetchFn('/api/settings/providers', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(provider),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function updateCustomProvider(
  fetchFn: typeof fetch,
  name: string,
  provider: CustomProviderConfig,
): Promise<{ message: string }> {
  const response = await fetchFn(`/api/settings/providers/${encodeURIComponent(name)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(provider),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function deleteCustomProvider(fetchFn: typeof fetch, name: string): Promise<{ message: string }> {
  const response = await fetchFn(`/api/settings/providers/${encodeURIComponent(name)}`, { method: 'DELETE' });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function getSkills(fetchFn: typeof fetch): Promise<SkillsResponse> {
  const response = await fetchFn('/api/settings/skills');
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function updateSkills(fetchFn: typeof fetch, skills: SkillsResponse): Promise<{ message: string }> {
  const response = await fetchFn('/api/settings/skills', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(skills),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function getSubagentTypes(fetchFn: typeof fetch): Promise<SubagentTypesResponse> {
  const response = await fetchFn('/api/settings/subagent-types');
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  const data = await response.json();
  // Strip the `test` mock-client sentinel from the catalog — see
  // miscApi.stripTestProvider for the rationale.
  if (Array.isArray(data.available_providers)) {
    data.available_providers = data.available_providers.filter((p: { id?: string }) => p?.id !== 'test');
  }
  return data;
}

export async function getHotkeys(fetchFn: typeof fetch): Promise<HotkeyConfig> {
  const response = await fetchFn('/api/hotkeys');
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function updateHotkeys(
  fetchFn: typeof fetch,
  config: HotkeyConfig,
): Promise<{ success: boolean; config: HotkeyConfig }> {
  const response = await fetchFn('/api/hotkeys', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function validateHotkeys(
  fetchFn: typeof fetch,
  config: HotkeyConfig,
): Promise<{ valid: boolean; config: HotkeyConfig }> {
  const response = await fetchFn('/api/hotkeys/validate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function applyHotkeyPreset(
  fetchFn: typeof fetch,
  preset: string,
): Promise<{ success: boolean; preset: string; config: HotkeyConfig }> {
  const response = await fetchFn('/api/hotkeys/preset', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ preset }),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
  return response.json();
}

// ---------------------------------------------------------------------------
// SP-086-4: Skill install / manage endpoints
// ---------------------------------------------------------------------------

export interface SkillInstallOptions {
  ref?: string;
  force?: boolean;
}

export async function listInstalledSkills(fetchFn: typeof fetch): Promise<
  Array<{
    id: string;
    origin: { type: string; installed_at?: string };
    installed_at?: string;
    updated_at?: string;
  }>
> {
  const response = await fetchFn('/api/skills');
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function listSkillRegistry(
  fetchFn: typeof fetch,
): Promise<import('./types/settings').SkillRegistryEntry[]> {
  const response = await fetchFn('/api/skills/registry');
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function installSkill(
  fetchFn: typeof fetch,
  source: string,
  opts?: SkillInstallOptions,
): Promise<import('./types/settings').SkillInstallResult[]> {
  const payload: Record<string, unknown> = { source };
  if (opts?.ref) payload.ref = opts.ref;
  if (opts?.force) payload.force = opts.force;
  const response = await fetchFn('/api/skills/install', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function updateSkill(
  fetchFn: typeof fetch,
  id: string,
): Promise<import('./types/settings').SkillInstallResult[]> {
  const response = await fetchFn('/api/skills/update', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id }),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function removeSkill(fetchFn: typeof fetch, id: string): Promise<{ status: string; id: string }> {
  const response = await fetchFn('/api/skills/remove', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id }),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}
