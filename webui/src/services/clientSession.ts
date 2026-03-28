export const WEBUI_CLIENT_ID_HEADER = 'X-Ledit-Client-ID';
export const WEBUI_CLIENT_ID_QUERY_PARAM = 'client_id';
const WEBUI_CLIENT_ID_STORAGE_KEY = 'ledit.webuiClientId';

export function getWebUIClientId(): string {
  if (typeof window === 'undefined') {
    return 'default';
  }

  let existing = window.sessionStorage.getItem(WEBUI_CLIENT_ID_STORAGE_KEY);
  if (existing) {
    return existing;
  }

  const generated =
    typeof window.crypto?.randomUUID === 'function'
      ? window.crypto.randomUUID()
      : `webui-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
  window.sessionStorage.setItem(WEBUI_CLIENT_ID_STORAGE_KEY, generated);
  return generated;
}

export function appendClientIdToUrl(input: string): string {
  if (typeof window === 'undefined') {
    return input;
  }

  const url = new URL(input, window.location.origin);
  url.searchParams.set(WEBUI_CLIENT_ID_QUERY_PARAM, getWebUIClientId());
  if (url.origin === window.location.origin) {
    return `${url.pathname}${url.search}${url.hash}`;
  }
  return url.toString();
}

export async function clientFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const headers = new Headers(init?.headers || {});
  headers.set(WEBUI_CLIENT_ID_HEADER, getWebUIClientId());
  return fetch(input, { ...init, headers });
}

