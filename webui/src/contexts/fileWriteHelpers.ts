import { showThemedConfirm } from '@sprout/ui';
import { debugLog } from '../utils/log';

// ---------------------------------------------------------------------------
// Adapter-agnostic file write helpers (consent flow for external paths)
// These are module-level so they don't get recreated on every render.
// ---------------------------------------------------------------------------

const consentTokenHeader = 'X-Sprout-Consent-Token';

export interface ConsentRequiredError {
  code: string;
  path: string;
  operation: 'read' | 'write';
  error?: string;
}

export interface ConsentTokenResponse {
  token: string;
  path: string;
  operation: 'read' | 'write';
  expires_at: string;
}

export async function parseConsentRequired(response: Response): Promise<ConsentRequiredError | null> {
  if (response.status !== 403) {
    return null;
  }

  const contentType = response.headers.get('content-type') || '';
  if (!contentType.includes('application/json')) {
    return null;
  }

  try {
    const body = (await response.json()) as Partial<ConsentRequiredError>;
    if (body.code === 'external_path_consent_required' && body.path && body.operation) {
      return {
        code: body.code,
        path: body.path,
        operation: body.operation,
        error: body.error,
      };
    }
  } catch (err) {
    debugLog('[parseConsentRequired] failed to parse consent response:', err);
    return null;
  }

  return null;
}

export async function issueConsent(
  fetchFn: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>,
  path: string,
  operation: 'read' | 'write'
): Promise<string> {
  const response = await fetchFn('/api/file/consent', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, operation }),
  });

  if (!response.ok) {
    throw new Error(`Failed to grant external ${operation} access: ${response.statusText}`);
  }

  const data = (await response.json()) as ConsentTokenResponse;
  return data.token;
}

export async function withConsentRetry(
  request: () => Promise<Response>,
  path: string,
  operation: 'read' | 'write',
  retryWithToken: (token: string) => Promise<Response>,
  fetchFn: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>,
): Promise<Response> {
  const initial = await request();
  const consent = await parseConsentRequired(initial);
  if (!consent) {
    return initial;
  }

  const approved = await showThemedConfirm(
    `External file ${operation} requested.\n\nPath: ${consent.path}\n\nAllow this one-time access?`,
    { title: 'External File Access', type: 'warning' },
  );
  if (!approved) {
    throw new Error(`External file ${operation} canceled by user: ${consent.path}`);
  }

  const token = await issueConsent(fetchFn, path, operation);
  return retryWithToken(token);
}

/**
 * Write a file using the provided fetch function (adapter-aware).
 * Handles the consent flow for external paths (403 → prompt → retry with token).
 */
export async function writeFileWithFetch(
  fetchFn: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>,
  filePath: string,
  content: string
): Promise<Response> {
  const baseUrl = `/api/file?path=${encodeURIComponent(filePath)}`;
  const body = JSON.stringify({ content });

  return withConsentRetry(
    () => fetchFn(baseUrl, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body,
    }),
    filePath,
    'write',
    (token) => fetchFn(baseUrl, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        [consentTokenHeader]: token,
      },
      body,
    }),
    fetchFn,
  );
}
