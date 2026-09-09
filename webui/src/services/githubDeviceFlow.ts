/**
 * GitHub OAuth Device Flow — studio-native sign-in.
 *
 * On studio surfaces (iOS/Android) the webui cannot reach
 * github.com/login/* cross-origin (no CORS), so the native shell performs
 * the exchange. The webui drives it through the generic bridge:
 *
 *   window.SproutStudioBridge.call('github', { op, ... })
 *
 * Ops (identical reply shapes on iOS and Android):
 * - deviceFlowStart → {userCode, verificationUri, deviceCode, interval, expiresIn}
 * - deviceFlowPoll  → {status:"ok", token} | {status:"pending"}
 *                     | {status:"slow_down"} | {error, message}
 * - openExternal    → {ok:true} | {error:"invalid-url"} | {error,...}
 *
 * The resulting token is stored exactly like a manually-pasted PAT
 * (localStorage `github_pat`), so repo browsing, browserGit, and agent
 * git push/pull treat both paths identically. In non-studio environments
 * (no bridge) isDeviceFlowAvailable() is false and the panel shows the
 * PAT form only.
 */

import { storeToken, storeUser, validateToken } from './githubService';
import type { GitHubUser } from './githubService';

interface BridgeResult {
  userCode?: string;
  verificationUri?: string;
  deviceCode?: string;
  interval?: number;
  expiresIn?: number;
  status?: string;
  token?: string;
  ok?: boolean;
  error?: string;
  message?: string;
}

export interface DeviceFlowSession {
  userCode: string;
  verificationUri: string;
  deviceCode: string;
  interval: number;
  expiresAt: number;
}

export function isDeviceFlowAvailable(): boolean {
  const bridge = (
    window as unknown as {
      SproutStudioBridge?: { call: (channel: string, payload: unknown, timeout?: number) => Promise<BridgeResult> };
    }
  ).SproutStudioBridge;
  return !!bridge && typeof bridge.call === 'function';
}

async function bridgeGithub(payload: Record<string, unknown>): Promise<BridgeResult> {
  const bridge = (
    window as unknown as {
      SproutStudioBridge: { call: (channel: string, payload: unknown, timeout?: number) => Promise<BridgeResult> };
    }
  ).SproutStudioBridge;
  return bridge.call('github', payload, 20000);
}

export async function startDeviceFlow(): Promise<DeviceFlowSession> {
  const result = await bridgeGithub({ op: 'deviceFlowStart' });
  if (result.error || !result.userCode || !result.deviceCode || !result.verificationUri) {
    throw new Error(result.message || result.error || 'Could not start GitHub sign-in.');
  }
  return {
    userCode: result.userCode,
    verificationUri: result.verificationUri,
    deviceCode: result.deviceCode,
    interval: (result.interval ?? 5) * 1000,
    expiresAt: Date.now() + (result.expiresIn ?? 900) * 1000,
  };
}

export async function openVerificationPage(uri: string): Promise<void> {
  const result = await bridgeGithub({ op: 'openExternal', url: uri });
  if (result.error) {
    throw new Error(result.message || 'Could not open the browser.');
  }
}

/**
 * Single poll attempt. Resolves:
 * - {done: true, user}   — authorized; token stored, user profile fetched
 * - {done: false, retry} — still waiting (pending or slow-down, with hint)
 * Rejects on hard errors (expired, denied, network).
 */
export async function pollDeviceFlow(
  session: DeviceFlowSession,
): Promise<{ done: boolean; retryInMs?: number; user?: GitHubUser }> {
  if (Date.now() > session.expiresAt) {
    throw new Error('The GitHub sign-in code expired. Start again.');
  }
  const result = await bridgeGithub({ op: 'deviceFlowPoll', deviceCode: session.deviceCode });
  if (result.status === 'ok' && result.token) {
    // Treat the device-flow token exactly like a pasted PAT.
    const validated: GitHubUser = await validateToken(result.token);
    storeToken(result.token);
    storeUser(validated);
    return { done: true, user: validated };
  }
  if (result.status === 'pending') {
    return { done: false, retryInMs: session.interval };
  }
  if (result.status === 'slow_down') {
    // GitHub asks for a longer interval; back off by 5s per RFC 8628.
    return { done: false, retryInMs: session.interval + 5000 };
  }
  throw new Error(result.message || result.error || 'GitHub sign-in failed.');
}
