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
    [key: string]: unknown;
  };
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

export async function deleteChatSession(id: string): Promise<{ message: string }> {
  const res = await clientFetch('/api/chat-sessions/delete', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id }),
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
