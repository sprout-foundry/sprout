import { clientFetch } from './clientSession';
import type { WorktreeInfo } from '../types/app';

export type { WorktreeInfo };

export interface WorktreesResponse {
  message: string;
  worktrees: WorktreeInfo[];
  current: string;
}

export interface WorktreeCreateRequest {
  path: string;
  branch: string;
  base_ref?: string;
}

export interface WorktreeCreateResponse {
  message: string;
  path: string;
  branch: string;
  output: string;
}

export interface WorktreeRemoveResponse {
  message: string;
  path: string;
  output: string;
}

export interface WorktreeCheckoutResponse {
  message: string;
  path: string;
  workspace: string;
}

export async function listWorktrees(): Promise<WorktreesResponse> {
  const res = await clientFetch('/api/git/worktrees');
  if (!res.ok) throw new Error('Failed to list worktrees');
  return res.json();
}

export async function createWorktree(request: WorktreeCreateRequest): Promise<WorktreeCreateResponse> {
  const res = await clientFetch('/api/git/worktree/create', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!res.ok) throw new Error('Failed to create worktree');
  return res.json();
}

export async function removeWorktree(path: string): Promise<WorktreeRemoveResponse> {
  const res = await clientFetch('/api/git/worktree/remove', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  });
  if (!res.ok) throw new Error('Failed to remove worktree');
  return res.json();
}

export async function checkoutWorktree(path: string): Promise<WorktreeCheckoutResponse> {
  const res = await clientFetch('/api/git/worktree/checkout', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  });
  if (!res.ok) throw new Error('Failed to checkout worktree');
  return res.json();
}
