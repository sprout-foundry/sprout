/**
 * Handler that bridges browser-native git (isomorphic-git) into the
 * CloudAdapter fetch pipeline. Returns Response objects matching the
 * shape the webui git panel expects.
 */

import { executeGitOp } from './browserGit';

function jsonOk(data: unknown): Response {
  return new Response(JSON.stringify(data), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function jsonError(message: string, status: number): Response {
  return new Response(JSON.stringify({ error: message }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

export async function handleBrowserGitRequest(
  urlPath: string,
  _method: string,
  fullUrl: string,
  bodyStr?: string,
): Promise<Response> {
  const op = urlPath.replace('/api/git/', '');

  const query: Record<string, string> = {};
  const qIdx = fullUrl.indexOf('?');
  if (qIdx >= 0) {
    const params = new URLSearchParams(fullUrl.substring(qIdx));
    params.forEach((v, k) => {
      query[k] = v;
    });
  }

  let body: Record<string, unknown> | undefined;
  if (bodyStr) {
    try {
      body = JSON.parse(bodyStr);
    } catch {
      /* ignore */
    }
  }

  try {
    const result = await executeGitOp(op, body, query);
    return jsonOk(result);
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return jsonError(msg, 500);
  }
}
