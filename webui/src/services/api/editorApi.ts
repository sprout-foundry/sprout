/**
 * Editor/Semantic domain API — adapter-aware editor and semantic operations.
 */

import type {
  DiagnosticsResponse,
  SemanticDiagnosticsResponse,
  SemanticDefinitionResponse,
  SemanticHoverResponse,
  SemanticRenameResponse,
  SemanticReferencesResponse,
  SemanticCodeActionsResponse,
  SemanticInlayHintsResponse,
  SemanticSignatureHelpResponse,
  WorkspaceSymbolsResponse,
} from './types';

export async function getPrettierConfig(fetchFn: typeof fetch, filePath: string): Promise<Record<string, unknown>> {
  const response = await fetchFn(`/api/files/prettier-config?path=${encodeURIComponent(filePath)}`);
  if (!response.ok) throw new Error('Failed to fetch prettier config');
  return response.json();
}

export async function getDiagnostics(
  fetchFn: typeof fetch,
  path: string,
  content: string,
): Promise<DiagnosticsResponse> {
  const response = await fetchFn('/api/diagnostics', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content }),
  });
  if (!response.ok) throw new Error('Failed to fetch diagnostics');
  return response.json();
}

export async function getSemanticDiagnostics(
  fetchFn: typeof fetch,
  path: string,
  content: string,
  languageId: string,
  trigger: 'edit' | 'save' = 'edit',
): Promise<SemanticDiagnosticsResponse> {
  const response = await fetchFn('/api/semantic', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content, language_id: languageId, method: 'diagnostics', trigger }),
  });
  if (!response.ok) throw new Error('Failed to fetch semantic diagnostics');
  return response.json();
}

export async function getSemanticDefinition(
  fetchFn: typeof fetch,
  path: string,
  content: string,
  languageId: string,
  line: number,
  column: number,
): Promise<SemanticDefinitionResponse> {
  const response = await fetchFn('/api/semantic', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content, language_id: languageId, method: 'definition', position: { line, column } }),
  });
  if (!response.ok) throw new Error('Failed to fetch definition');
  return response.json();
}

export async function getSemanticHover(
  fetchFn: typeof fetch,
  path: string,
  content: string,
  languageId: string,
  line: number,
  column: number,
): Promise<SemanticHoverResponse> {
  const response = await fetchFn('/api/semantic', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content, language_id: languageId, method: 'hover', position: { line, column } }),
  });
  if (!response.ok) throw new Error('Failed to fetch hover');
  return response.json();
}

export async function getSemanticRename(
  fetchFn: typeof fetch,
  path: string,
  content: string,
  languageId: string,
  line: number,
  column: number,
): Promise<SemanticRenameResponse> {
  const response = await fetchFn('/api/semantic', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content, language_id: languageId, method: 'rename', position: { line, column } }),
  });
  if (!response.ok) throw new Error('Failed to fetch rename');
  return response.json();
}

export async function getSemanticReferences(
  fetchFn: typeof fetch,
  path: string,
  content: string,
  languageId: string,
  line: number,
  column: number,
): Promise<SemanticReferencesResponse> {
  const response = await fetchFn('/api/semantic', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, content, language_id: languageId, method: 'references', position: { line, column } }),
  });
  if (!response.ok) throw new Error('Failed to fetch references');
  return response.json();
}

export async function getSemanticCodeActions(
  fetchFn: typeof fetch,
  path: string,
  content: string,
  languageId: string,
  line: number,
  column: number,
): Promise<SemanticCodeActionsResponse> {
  const response = await fetchFn('/api/semantic', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      path,
      content,
      language_id: languageId,
      method: 'code_actions',
      position: { line, column },
    }),
  });
  if (!response.ok) throw new Error('Failed to fetch code actions');
  return response.json();
}

export async function getWorkspaceSymbols(fetchFn: typeof fetch, query: string): Promise<WorkspaceSymbolsResponse> {
  const response = await fetchFn(`/api/workspace/symbols?query=${encodeURIComponent(query)}`);
  if (!response.ok) throw new Error('Failed to fetch workspace symbols');
  return response.json();
}

export async function getSemanticInlayHints(
  fetchFn: typeof fetch,
  path: string,
  content: string,
  languageId: string,
): Promise<SemanticInlayHintsResponse> {
  const response = await fetchFn('/api/semantic', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      path,
      content,
      language_id: languageId,
      method: 'inlay_hints',
    }),
  });
  if (!response.ok) throw new Error('Failed to fetch inlay hints');
  return response.json();
}

export async function getSemanticSignatureHelp(
  fetchFn: typeof fetch,
  path: string,
  content: string,
  languageId: string,
  line: number,
  column: number,
): Promise<SemanticSignatureHelpResponse> {
  const response = await fetchFn('/api/semantic', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      path,
      content,
      language_id: languageId,
      method: 'signature_help',
      position: { line, column },
    }),
  });
  if (!response.ok) throw new Error('Failed to fetch signature help');
  return response.json();
}
