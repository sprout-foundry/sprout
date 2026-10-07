import type { APIAdapter } from '@sprout/ui';
import type { HostCapabilities, SproutHost } from './types';

/**
 * Explicit capability keys a host shell declares through its installed
 * adapter. A host shell advertises what it provides by installing an adapter
 * (the studio shell reports the `capabilities.json` it ships), so these keys
 * are a truthful, host-specific statement and must reach the selected host's
 * `capabilities`.
 */
export const ADAPTER_CAPABILITY_KEYS = [
  'supportsSSH',
  'supportsGit',
  'supportsChat',
  'supportsWorkspaceSwitching',
  'supportsFolderPicker',
  'supportsExport',
  'supportsInstances',
  'supportsLocalTerminal',
  'supportsSettings',
] as const satisfies readonly (keyof APIAdapter)[];

/** Maps each adapter capability key to its `HostCapabilities` field. */
const CAPABILITY_FIELD: Record<(typeof ADAPTER_CAPABILITY_KEYS)[number], keyof HostCapabilities> = {
  supportsSSH: 'ssh',
  supportsGit: 'git',
  supportsChat: 'chat',
  supportsWorkspaceSwitching: 'workspaceSwitching',
  supportsFolderPicker: 'folderPicker',
  supportsExport: 'export',
  supportsInstances: 'instances',
  supportsLocalTerminal: 'localTerminal',
  supportsSettings: 'settings',
};

/**
 * Overlay the capability keys the adapter explicitly declares onto the host's
 * capabilities, in place, and return the same host.
 *
 * The adapter is the host shell's own declaration of what it provides (it
 * ships the concrete native operations), so a declared key wins over the
 * host's local default: a studio shell that advertises the folder picker and
 * workspace switching reports them even though `localHost` defaults
 * `folderPicker` off. Keys the adapter leaves `undefined` are not touched, so
 * a plain local build (no capabilities declared) keeps today's
 * `localHost.capabilities` byte-identical, and the cloud host is unaffected
 * (its adapter declares no folder-picker/workspace-switching overrides and any
 * change is caught by `hosts.test.ts`).
 *
 * Generic by construction: no platform- or studio-specific branch — every
 * adapter-declared capability is merged, whatever its value.
 */
export function applyHostCapabilities(host: SproutHost, adapter: APIAdapter | null | undefined): SproutHost {
  if (!adapter) return host;
  for (const key of ADAPTER_CAPABILITY_KEYS) {
    const declared = adapter[key];
    if (typeof declared === 'boolean') {
      host.capabilities[CAPABILITY_FIELD[key]] = declared;
    }
  }
  return host;
}
