/**
 * Workspace domain API — adapter-aware workspace operations.
 */

export async function getWorkspace(fetchFn: typeof fetch): Promise<any> {
  const response = await fetchFn('/api/workspace');
  const text = await response.text();

  const parseWorkspacePayload = (t: string): any => {
    const trimmed = t.trim();
    if (!trimmed) return {};
    try { return JSON.parse(trimmed); } catch { return { message: trimmed }; }
  };

  const isHTMLResponseBody = (t: string): boolean => {
    const trimmed = t.trim().toLowerCase();
    return trimmed.startsWith('<!doctype html') || trimmed.startsWith('<html') || trimmed.startsWith('<head') || trimmed.startsWith('<body');
  };

  const data = parseWorkspacePayload(text);

  if (!response.ok) {
    throw new Error(data.error || data.message || 'Failed to fetch workspace');
  }

  if (isHTMLResponseBody(text)) {
    throw new Error('Workspace API returned HTML response');
  }

  if (data && typeof data === 'object' && 'workspace_root' in data && 'daemon_root' in data) {
    return data;
  }

  throw new Error('Workspace API returned malformed response');
}

export async function setWorkspace(fetchFn: typeof fetch, path: string): Promise<any> {
  const response = await fetchFn('/api/workspace', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  });

  const text = await response.text();
  const data = (() => {
    const trimmed = text.trim();
    if (!trimmed) return {};
    try { return JSON.parse(trimmed); } catch { return { message: trimmed }; }
  })();

  if (!response.ok) {
    throw new Error(data.error || data.message || 'Failed to update workspace');
  }

  const isHTML = text.trim().toLowerCase().startsWith('<!doctype html') || text.trim().toLowerCase().startsWith('<html');
  if (isHTML) {
    throw new Error('Workspace API returned HTML response');
  }

  if (data && typeof data === 'object' && 'workspace_root' in data && 'daemon_root' in data) {
    return data;
  }

  // Remote/proxy setups may respond with non-JSON success body
  const workspace = await getWorkspace(fetchFn);
  return { ...workspace, message: data.message || 'Workspace updated' };
}
