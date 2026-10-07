/**
 * The hosted editor's model: the platform's managed model, or a model from a
 * provider the user saved their own API key for (Home › Settings). The choice
 * is kept on the user's account, so it follows them across browsers; the
 * platform routes the agent's requests by it.
 */

import { outwardURL } from '../host/outwardURL';

/**
 * Resolve a platform **account API** path (e.g. '/user/me/editor-model')
 * against the host's outward platform origin. The host declares that base on
 * its transport; when it supplies none the path stays relative (same-origin),
 * today's behavior. This is the transport-level resolver, not a page route —
 * the API base is the same origin as the platform's pages but is not one of
 * them.
 */
function platformApi(path: string): string {
  return outwardURL(path);
}

export interface EditorModelState {
  /** Empty for the managed model. */
  provider: string;
  model: string;
  /** Providers with a saved key the editor can use. */
  key_providers: string[];
  /** Whether to suggest an own key when managed credits run out. */
  recommend_byok: boolean;
}

export interface ProviderModel {
  id: string;
  context_length?: number;
}

export const PROVIDER_LABELS: Record<string, string> = {
  openai: 'OpenAI',
  anthropic: 'Anthropic',
  openrouter: 'OpenRouter',
  google: 'Google',
  groq: 'Groq',
  deepinfra: 'DeepInfra',
};

async function readError(res: Response): Promise<string> {
  const body = (await res.json().catch(() => ({}))) as { error?: unknown };
  return typeof body.error === 'string' ? body.error : `HTTP ${res.status}`;
}

export async function getEditorModel(): Promise<EditorModelState> {
  const res = await fetch(platformApi('/user/me/editor-model'), { credentials: 'include' });
  if (!res.ok) throw new Error(await readError(res));
  return (await res.json()) as EditorModelState;
}

export async function setEditorModel(choice: { provider: string; model: string }): Promise<void> {
  const res = await fetch(platformApi('/user/me/editor-model'), {
    method: 'PUT',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(choice),
  });
  if (!res.ok) throw new Error(await readError(res));
}

export async function listProviderModels(provider: string): Promise<ProviderModel[]> {
  const res = await fetch(platformApi(`/user/me/editor-model/models?provider=${encodeURIComponent(provider)}`), {
    credentials: 'include',
  });
  if (!res.ok) throw new Error(await readError(res));
  return ((await res.json()) as { models?: ProviderModel[] }).models ?? [];
}
