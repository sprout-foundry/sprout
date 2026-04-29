/**
 * Editor/Semantic domain API — adapter-aware editor and semantic operations.
 */

export async function getPrettierConfig(fetchFn: typeof fetch, filePath: string): Promise<any> {
  const response = await fetchFn(`/api/prettier/config?path=${encodeURIComponent(filePath)}`);
  if (!response.ok) throw new Error('Failed to fetch prettier config');
  return response.json();
}

export async function getDiagnostics(fetchFn: typeof fetch, path: string, content: string): Promise<any> {
  const response = await fetchFn('/api/diagnostics', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content }),
  });
  if (!response.ok) throw new Error('Failed to fetch diagnostics');
  return response.json();
}

export async function getSemanticDiagnostics(fetchFn: typeof fetch, path: string, content: string, languageId: string, trigger: 'edit' | 'save' = 'edit'): Promise<any> {
  const response = await fetchFn('/api/semantic/diagnostics', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content, language_id: languageId, trigger }),
  });
  if (!response.ok) throw new Error('Failed to fetch semantic diagnostics');
  return response.json();
}

export async function getSemanticDefinition(fetchFn: typeof fetch, path: string, content: string, languageId: string, line: number, column: number): Promise<any> {
  const response = await fetchFn('/api/semantic/definition', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content, language_id: languageId, line, column }),
  });
  if (!response.ok) throw new Error('Failed to fetch definition');
  return response.json();
}

export async function getSemanticHover(fetchFn: typeof fetch, path: string, content: string, languageId: string, line: number, column: number): Promise<any> {
  const response = await fetchFn('/api/semantic/hover', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content, language_id: languageId, line, column }),
  });
  if (!response.ok) throw new Error('Failed to fetch hover');
  return response.json();
}

export async function getSemanticRename(fetchFn: typeof fetch, path: string, content: string, languageId: string, line: number, column: number): Promise<any> {
  const response = await fetchFn('/api/semantic/rename', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content, language_id: languageId, line, column }),
  });
  if (!response.ok) throw new Error('Failed to fetch rename');
  return response.json();
}

export async function getSemanticReferences(fetchFn: typeof fetch, path: string, content: string, languageId: string, line: number, column: number): Promise<any> {
  const response = await fetchFn('/api/semantic/references', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content, language_id: languageId, line, column }),
  });
  if (!response.ok) throw new Error('Failed to fetch references');
  return response.json();
}

export async function getSemanticCodeActions(fetchFn: typeof fetch, path: string, content: string, languageId: string, line: number, column: number): Promise<any> {
  const response = await fetchFn('/api/semantic/code-actions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content, language_id: languageId, line, column }),
  });
  if (!response.ok) throw new Error('Failed to fetch code actions');
  return response.json();
}

export async function getWorkspaceSymbols(fetchFn: typeof fetch, query: string): Promise<any> {
  const response = await fetchFn(`/api/semantic/workspace-symbols?query=${encodeURIComponent(query)}`);
  if (!response.ok) throw new Error('Failed to fetch workspace symbols');
  return response.json();
}
