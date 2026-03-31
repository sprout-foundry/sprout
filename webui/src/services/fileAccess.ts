import { clientFetch } from './clientSession';
import { showThemedConfirm } from '../components/ThemedDialog';

const consentTokenHeader = 'X-Ledit-Consent-Token';

interface ConsentRequiredError {
  code: string;
  path: string;
  operation: 'read' | 'write';
  error?: string;
}

interface ConsentTokenResponse {
  token: string;
  path: string;
  operation: 'read' | 'write';
  expires_at: string;
}

async function parseConsentRequired(response: Response): Promise<ConsentRequiredError | null> {
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
  } catch {
    return null;
  }

  return null;
}

async function issueConsent(path: string, operation: 'read' | 'write'): Promise<string> {
  const response = await clientFetch('/api/file/consent', {
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

async function withConsentRetry(
  request: () => Promise<Response>,
  path: string,
  operation: 'read' | 'write',
  retryWithToken: (token: string) => Promise<Response>
): Promise<Response> {
  const initial = await request();
  const consent = await parseConsentRequired(initial);
  if (!consent) {
    return initial;
  }

  const approved = await showThemedConfirm(
    `External file ${operation} requested.\n\nPath: ${consent.path}\n\nAllow this one-time access?`,
    { title: 'External File Access', type: 'warning' }
  );
  if (!approved) {
    throw new Error(`External file ${operation} canceled by user: ${consent.path}`);
  }

  const token = await issueConsent(path, operation);
  return retryWithToken(token);
}

export async function readFileWithConsent(filePath: string): Promise<Response> {
  const baseUrl = `/api/file?path=${encodeURIComponent(filePath)}`;

  return withConsentRetry(
    () => clientFetch(baseUrl),
    filePath,
    'read',
    (token) =>
      clientFetch(baseUrl, {
        headers: { [consentTokenHeader]: token },
      })
  );
}

export async function writeFileWithConsent(filePath: string, content: string): Promise<Response> {
  const baseUrl = `/api/file?path=${encodeURIComponent(filePath)}`;

  return withConsentRetry(
    () =>
      clientFetch(baseUrl, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ content }),
      }),
    filePath,
    'write',
    (token) =>
      clientFetch(baseUrl, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          [consentTokenHeader]: token,
        },
        body: JSON.stringify({ content }),
      })
  );
}
