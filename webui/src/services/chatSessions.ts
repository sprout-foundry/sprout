import { clientFetch } from './clientSession';

export interface ChatSession {
  id: string;
  name: string;
  created_at: string;
  last_active_at: string;
  message_count: number;
  current_session_id: string;
  active_query: boolean;
  current_query?: string;
  is_default: boolean;
  is_active: boolean;
  provider?: string;
  model?: string;
  worktree_path?: string;
}

export interface ChatSessionsResponse {
  message: string;
  chat_sessions: ChatSession[];
  active_chat_id: string;
  total_sessions: number;
}

export interface ChatSessionSwitchResponse {
  message: string;
  active_chat_id: string;
  chat_session: {
    id: string;
    name: string;
    messages: Array<{ role: string; content: string; reasoning_content?: string }>;
    total_tokens?: number;
    total_cost?: number;
    session_id?: string;
    agent_state?: string;
    provider?: string;
    model?: string;
    worktree_path?: string;
    [key: string]: unknown;
  };
}

export interface WorktreeInfo {
  path: string;
  branch: string;
  is_main: boolean;
  is_current: boolean;
  parent_path?: string;
  parent_branch?: string;
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
  removeWorktree = false,
): Promise<{ message: string; worktree_removed?: boolean; worktree_error?: string }> {
  const res = await clientFetch('/api/chat-sessions/delete', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id, remove_worktree: removeWorktree }),
  });
  if (!res.ok) throw new Error('Failed to delete chat session');
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

export async function getChatSessionWorktree(chatId: string): Promise<{ message: string; chat_id: string; worktree_path: string }> {
  const res = await clientFetch(`/api/chat-session/${chatId}/worktree`);
  if (!res.ok) throw new Error('Failed to get chat session worktree');
  return res.json();
}

export async function setChatSessionWorktree(chatId: string, worktreePath: string): Promise<{
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

export async function switchChatSessionWorktree(chatId: string, worktreePath: string): Promise<{
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

export async function createWorktree(path: string, branch: string, baseRef?: string): Promise<{
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
