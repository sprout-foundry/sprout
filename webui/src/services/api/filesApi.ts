/**
 * Files domain API — adapter-aware file operations.
 */

import { FilesResponse, CreateItemResponse, DeleteItemResponse, RenameItemResponse } from './types';

export async function getFiles(fetchFn: typeof fetch): Promise<FilesResponse> {
  const response = await fetchFn('/api/files');
  if (!response.ok) throw new Error('Failed to fetch files');
  return response.json();
}

export async function createItem(fetchFn: typeof fetch, path: string, isDirectory = false): Promise<CreateItemResponse> {
  const response = await fetchFn('/api/files/create', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, is_directory: isDirectory }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Failed to create item' }));
    throw new Error(data.message || data.error || 'Failed to create item');
  }
  return response.json();
}

export async function deleteItem(fetchFn: typeof fetch, path: string): Promise<DeleteItemResponse> {
  const response = await fetchFn('/api/files/delete', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Failed to delete item' }));
    throw new Error(data.message || data.error || 'Failed to delete item');
  }
  return response.json();
}

export async function renameItem(fetchFn: typeof fetch, oldPath: string, newPath: string): Promise<RenameItemResponse> {
  const response = await fetchFn('/api/files/rename', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ old_path: oldPath, new_path: newPath }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Failed to rename item' }));
    throw new Error(data.message || data.error || 'Failed to rename item');
  }
  return response.json();
}

export async function openInFileBrowser(fetchFn: typeof fetch, path: string): Promise<void> {
  const response = await fetchFn('/api/files/open-in-browser', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Failed to open in file browser' }));
    throw new Error(data.message || data.error || 'Failed to open in file browser');
  }
}
