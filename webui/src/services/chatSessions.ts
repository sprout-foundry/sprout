import type { WorktreeInfo } from '../types/app';
import type { ChatSession as CanonicalChatSession } from '../types/generated';
import { clientFetch } from './clientSession';

export type { WorktreeInfo };

// Re-export the canonical wire-format shape from the generated types
// module. Importers that want JUST the server-side fields can pull this
// in instead of the computed-augmented version below. SP-034-5c.
export type { CanonicalChatSession };

/**
 * Frontend-facing ChatSession: extends the canonical wire shape with
 * computed-only fields the server doesn't persist (`is_default`,
 * `is_active`). The canonical fields live in `types/generated.ts`;
 * adding a wire-format field there flows through here automatically.
 */
export interface ChatSession extends CanonicalChatSession {
  is_default: boolean;
  is_active: boolean;
}

export interface ChatSessionsResponse {
  message: string;
  chat_sessions: ChatSession[];
  active_chat_id: string;
  total_sessions: number;
}

export interface ChatSessionSwitchResponseChatSession {
  id: string;
  name: string;
  messages?: Array<{ role: string; content: string; reasoning_content?: string; timestamp?: string }>;
  total_tokens?: number;
  total_cost?: number;
  session_id?: string;
  agent_state?: string;
  active_query: boolean;
  is_default: boolean;
  is_pinned: boolean;
  provider?: string;
  model?: string;
  worktree_path?: string;
  created_at: string;
  last_active_at: string;
  message_count: number;
  current_session_id: string;
}

export interface ChatSessionSwitchResponse {
  message: string;
  active_chat_id: string;
  chat_session: ChatSessionSwitchResponseChatSession;
}

export interface WorktreeListResponse {
  message: string;
  worktrees: WorktreeInfo[];
  current: string;
}

export async function listChatSessions(): Promise<ChatSessionsResponse> {
  const res = await clientFetch('/api/chat-sessions');
  if (!res.ok) throw new Error('Failed to list chat sessions');
  return res.json();
}

export async function createChatSession(name?: string): Promise<{ message: string; chat_session: ChatSession }> {
  const res = await clientFetch('/api/chat-sessions/create', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(name ? { name } : {}),
  });
  if (!res.ok) throw new Error('Failed to create chat session');
  return res.json();
}

export async function deleteChatSession(
  id: string,
  shouldRemoveWorktree = false,
): Promise<{ message: string; worktree_removed?: boolean; worktree_error?: string }> {
  const res = await clientFetch('/api/chat-sessions/delete', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id, remove_worktree: shouldRemoveWorktree }),
  });
  if (!res.ok) throw new Error('Failed to delete chat session');
  return res.json();
}

export async function deleteAllChatSessions(): Promise<{
  message: string;
  deleted_count: number;
  active_chat_id: string;
}> {
  const res = await clientFetch('/api/chat-sessions/delete-all', { method: 'POST' });
  if (!res.ok) throw new Error('Failed to delete all chat sessions');
  return res.json();
}

export async function renameChatSession(
  id: string,
  name: string,
): Promise<{ message: string; chat_session: ChatSession }> {
  const res = await clientFetch('/api/chat-sessions/rename', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id, name }),
  });
  if (!res.ok) throw new Error('Failed to rename chat session');
  return res.json();
}

export async function switchChatSession(id: string): Promise<ChatSessionSwitchResponse> {
  const res = await clientFetch('/api/chat-sessions/switch', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id }),
  });
  if (!res.ok) throw new Error('Failed to switch chat session');
  return res.json();
}

export async function getChatSessionWorktree(
  chatId: string,
): Promise<{ message: string; chat_id: string; worktree_path: string }> {
  const res = await clientFetch(`/api/chat-session/${chatId}/worktree`);
  if (!res.ok) throw new Error('Failed to get chat session worktree');
  return res.json();
}

export async function setChatSessionWorktree(
  chatId: string,
  worktreePath: string,
): Promise<{
  message: string;
  chat_id: string;
  worktree_path: string;
  chat_session: ChatSession;
}> {
  const res = await clientFetch(`/api/chat-session/${chatId}/worktree`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ worktree_path: worktreePath }),
  });
  if (!res.ok) throw new Error('Failed to set chat session worktree');
  return res.json();
}

export async function switchChatSessionWorktree(
  chatId: string,
  worktreePath: string,
): Promise<{
  message: string;
  chat_id: string;
  worktree_path: string;
  chat_session: ChatSessionSwitchResponse['chat_session'];
}> {
  const res = await clientFetch(`/api/chat-session/${chatId}/worktree/switch`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ worktree_path: worktreePath }),
  });
  if (!res.ok) throw new Error('Failed to switch chat session worktree');
  return res.json();
}

export async function listWorktrees(): Promise<WorktreeListResponse> {
  const res = await clientFetch('/api/git/worktrees');
  if (!res.ok) throw new Error('Failed to list worktrees');
  return res.json();
}

export async function createWorktree(
  path: string,
  branch: string,
  baseRef?: string,
): Promise<{
  message: string;
  path: string;
  branch: string;
  output: string;
}> {
  const res = await clientFetch('/api/git/worktree/create', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, branch, base_ref: baseRef }),
  });
  if (!res.ok) throw new Error('Failed to create worktree');
  return res.json();
}

export async function removeWorktree(path: string): Promise<{ message: string; path: string; output: string }> {
  const res = await clientFetch('/api/git/worktree/remove', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  });
  if (!res.ok) throw new Error('Failed to remove worktree');
  return res.json();
}

export async function checkoutWorktree(path: string): Promise<{ message: string; path: string; workspace: string }> {
  const res = await clientFetch('/api/git/worktree/checkout', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  });
  if (!res.ok) throw new Error('Failed to checkout worktree');
  return res.json();
}

export async function createChatSessionInWorktree(req: {
  branch: string;
  base_ref?: string;
  name?: string;
  auto_switch_workspace?: boolean;
}): Promise<{
  message: string;
  chat_session: ChatSessionSwitchResponse['chat_session'];
  worktree_path: string;
  branch: string;
  workspace_root: string;
}> {
  const res = await clientFetch('/api/chat-sessions/create-in-worktree', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => 'Unknown error');
    throw new Error(text || 'Failed to create chat session in worktree');
  }
  return res.json();
}

/**
 * Fork a chat session at a given user-message breakpoint (1-based).
 * Saves the current session and creates a new one from truncated history.
 */
export async function forkChatSession(
  chatId: string,
  breakpointIndex: number,
): Promise<{
  success: boolean;
  chat_id: string;
  session_id: string;
  message: string;
}> {
  const res = await clientFetch('/api/chat-sessions/fork', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ chat_id: chatId, breakpoint_index: breakpointIndex }),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => 'Unknown error');
    throw new Error(text || 'Failed to fork chat session');
  }
  return res.json();
}

/**
 * Get the list of forkable breakpoints (user messages) for a chat session.
 */
export async function getChatSessionBreakpoints(chatId: string): Promise<{
  breakpoints: Array<{ index: number; content: string }>;
}> {
  const res = await clientFetch('/api/chat-sessions/breakpoints', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ chat_id: chatId }),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => 'Unknown error');
    throw new Error(text || 'Failed to get chat session breakpoints');
  }
  return res.json();
}
