/**
 * Settings domain API — adapter-aware settings operations.
 */

export async function getSettings(fetchFn: typeof fetch): Promise<any> {
  const response = await fetchFn('/api/settings');
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function getSettingsLayer(fetchFn: typeof fetch, layer: 'global' | 'workspace' | 'session'): Promise<any> {
  const response = await fetchFn(`/api/settings?layer=${layer}`);
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function getSettingsProvenance(fetchFn: typeof fetch): Promise<any> {
  const response = await fetchFn('/api/settings?layer=provenance');
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function updateSettings(fetchFn: typeof fetch, settings: Record<string, any>, layer?: 'session' | 'workspace' | 'global'): Promise<any> {
  const url = layer ? `/api/settings?layer=${layer}` : '/api/settings';
  const response = await fetchFn(url, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(settings),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function getMCPSettings(fetchFn: typeof fetch): Promise<any> {
  const response = await fetchFn('/api/settings/mcp');
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function updateMCPSettings(fetchFn: typeof fetch, settingsData: any): Promise<any> {
  const response = await fetchFn('/api/settings/mcp', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(settingsData),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function addMCPServer(fetchFn: typeof fetch, server: any): Promise<any> {
  const response = await fetchFn('/api/settings/mcp/servers/', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(server),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function updateMCPServer(fetchFn: typeof fetch, name: string, server: any): Promise<any> {
  const response = await fetchFn(`/api/settings/mcp/servers/${encodeURIComponent(name)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(server),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function deleteMCPServer(fetchFn: typeof fetch, name: string): Promise<any> {
  const response = await fetchFn(`/api/settings/mcp/servers/${encodeURIComponent(name)}`, { method: 'DELETE' });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function getCustomProviders(fetchFn: typeof fetch): Promise<any> {
  const response = await fetchFn('/api/settings/providers');
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function addCustomProvider(fetchFn: typeof fetch, provider: any): Promise<any> {
  const response = await fetchFn('/api/settings/providers', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(provider),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function updateCustomProvider(fetchFn: typeof fetch, name: string, provider: any): Promise<any> {
  const response = await fetchFn(`/api/settings/providers/${encodeURIComponent(name)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(provider),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function deleteCustomProvider(fetchFn: typeof fetch, name: string): Promise<any> {
  const response = await fetchFn(`/api/settings/providers/${encodeURIComponent(name)}`, { method: 'DELETE' });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function getSkills(fetchFn: typeof fetch): Promise<any> {
  const response = await fetchFn('/api/settings/skills');
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function updateSkills(fetchFn: typeof fetch, skills: any): Promise<any> {
  const response = await fetchFn('/api/settings/skills', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(skills),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function getSubagentTypes(fetchFn: typeof fetch): Promise<any> {
  const response = await fetchFn('/api/settings/subagent-types');
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function updateSubagentType(fetchFn: typeof fetch, name: string, updates: Record<string, any>): Promise<any> {
  const response = await fetchFn(`/api/settings/subagent-types/${encodeURIComponent(name)}/`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(updates),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
  return response.json();
}

export async function getHotkeys(fetchFn: typeof fetch): Promise<any> {
  const response = await fetchFn('/api/hotkeys');
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function updateHotkeys(fetchFn: typeof fetch, config: any): Promise<any> {
  const response = await fetchFn('/api/hotkeys', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function validateHotkeys(fetchFn: typeof fetch, config: any): Promise<any> {
  const response = await fetchFn('/api/hotkeys/validate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  });
  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
  return response.json();
}

export async function applyHotkeyPreset(fetchFn: typeof fetch, preset: string): Promise<any> {
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
