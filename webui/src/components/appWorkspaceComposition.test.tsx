/**
 * SP-160 Acceptance criteria 2 — the local app's workspace composition.
 *
 * The local web UI is the package's own consumer: its shell must render the
 * active space through `SproutWorkspace` (with `localHost` and
 * `SproutProviders`) and reach the composition pieces through the package's
 * public entry points, never through a private module path. Anything the app
 * needs that the entries do not export is a gap in the API — added to the
 * entries, not imported privately.
 *
 * Two enforcement layers:
 *
 * 1. Structural: the composition site (`components/AppContent.tsx`) renders
 *    `<SproutWorkspace` and imports it, the shell-props type, the mode id and
 *    the workspace-mode hook from the public `views` entry — not the private
 *    module paths. The app root (`App.tsx`) renders `<SproutProviders` from
 *    the public `providers` entry, and the entry (`index.tsx`) selects
 *    `localHost` from the public `host` entry.
 * 2. Behavioural: `SproutWorkspace` in `providers="ambient"` mode renders the
 *    registry shell under the *ambient* host and stack (no second events
 *    transport, no shadowing `HostProvider`). That is the mode the app uses,
 *    and `SproutWorkspace.test.tsx` pins it against the real registry.
 *
 * The assertions are source-shape plus one isolated render (a fake composition
 * mirroring the app's), not a render of the whole `App` — `App` boots the
 * WebSocket, the WASM shell and the initialization hooks, which are not what
 * this item changes.
 */

import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

// The registry pulls the built-in shells in with it, and the shells read the
// whole app state object / dev-server API. Stub them so the composition test
// exercises the real registry + real providers without a full app render.
vi.mock('../workspaces/CodeShell', () => ({
  default: () => <div data-testid="app-workspace-code-shell" />,
}));
vi.mock('../workspaces/DesignShell', () => ({
  default: () => <div data-testid="app-workspace-design-shell" />,
}));

import { HostProvider, localHost } from '../host';
import { SproutProviders } from '../providers';
import { SproutWorkspace } from '../views';

const here = dirname(fileURLToPath(import.meta.url));
const read = (rel: string) => readFileSync(resolve(here, rel), 'utf-8');

const appContentSource = read('./AppContent.tsx');
const appSource = read('../App.tsx');

/**
 * The composition primitives the app must reach through the public entries.
 * Each maps to the entry module that re-exports it and the private modules it
 * must NOT be imported from.
 */
const PUBLIC_COMPOSITION_IMPORTS: Array<{ entry: string; privates: RegExp[] }> = [
  { entry: '../views', privates: [/from '\.\.\/views\/SproutWorkspace'/, /from '\.\.\/views\/ViewsLayout'/] },
  { entry: '../providers', privates: [/from '\.\.\/providers\/SproutProviders'/] },
  { entry: '../host', privates: [/from '\.\.\/host\/HostProvider'/, /from '\.\.\/host\/localHost'/] },
];

describe('the local app composes its workspace through SproutWorkspace', () => {
  it('renders the space through <SproutWorkspace>, not the registry shell directly', () => {
    expect(appContentSource).toContain('<SproutWorkspace');
    // The app used to resolve and render the registry shell itself
    // (`const ModeShell = workspaceMode.Shell; … <ModeShell/>`). That assembly
    // is now SproutWorkspace's, so the app must not render the shell directly.
    expect(appContentSource).not.toMatch(/<ModeShell\b/);
    expect(appContentSource).not.toMatch(/<\s*workspaceMode\.Shell\b/);
  });

  it('uses ambient providers — the app root owns the single host + stack', () => {
    // Exactly one SproutProviders must exist: the app-level one in App.tsx. The
    // space composition reuses it (`providers="ambient"`) rather than forking a
    // second events transport and a second set of buffer/notification contexts.
    expect(appContentSource).toMatch(/<SproutWorkspace[\s\S]*?providers="ambient"/);
    expect(appContentSource).not.toContain('<SproutProviders');
    expect(appSource).toContain('<SproutProviders');
  });

  it('feeds the resolved space and shell props down, and syncs space changes back', () => {
    expect(appContentSource).toMatch(/space=\{workspaceMode\.id\}/);
    expect(appContentSource).toMatch(/shellProps=\{shellProps\}/);
    expect(appContentSource).toMatch(/onSpaceChange=\{selectWorkspaceMode\}/);
    expect(appContentSource).toMatch(/hasDesignTree=\{hasDesignTree\}/);
  });
});

describe('the local app imports composition pieces from the public entries', () => {
  it('reaches no composition primitive through a private module path', () => {
    for (const { entry, privates } of PUBLIC_COMPOSITION_IMPORTS) {
      for (const source of [appContentSource, appSource]) {
        for (const privatePath of privates) {
          if (privatePath.test(source)) {
            throw new Error(
              `the app imports a composition primitive privately (${privatePath}); import it from ${entry} instead`,
            );
          }
        }
      }
    }
  });

  it('imports SproutWorkspace + its companion types from the views entry', () => {
    // The views entry is where the composition lives; the app must not reach
    // into `../workspaces/registry`, `../workspaces/shell` or
    // `../workspaces/useWorkspaceMode` for these — they are re-exported.
    expect(appContentSource).toMatch(/import \{[^}]*SproutWorkspace[^}]*\} from '\.\.\/views';/);
    expect(appContentSource).toMatch(/import \{[^}]*useWorkspaceMode[^}]*\} from '\.\.\/views';/);
    expect(appContentSource).toMatch(/import type \{[^}]*WorkspaceShellProps[^}]*\} from '\.\.\/views';/);
    expect(appContentSource).toMatch(/import type \{[^}]*WorkspaceModeId[^}]*\} from '\.\.\/views';/);
    expect(appContentSource).not.toMatch(/from '\.\.\/workspaces\/registry'/);
    expect(appContentSource).not.toMatch(/from '\.\.\/workspaces\/shell'/);
    expect(appContentSource).not.toMatch(/from '\.\.\/workspaces\/useWorkspaceMode'/);
  });

  it('the views entry actually re-exports the pieces the app imports', () => {
    const viewsEntry = read('../views/index.ts');
    expect(viewsEntry).toMatch(/export \{ SproutWorkspace \} from '\.\/SproutWorkspace';/);
    expect(viewsEntry).toMatch(/export \{ useWorkspaceMode \} from '\.\.\/workspaces\/useWorkspaceMode';/);
    expect(viewsEntry).toMatch(/WorkspaceShellProps/);
    expect(viewsEntry).toMatch(/WorkspaceModeId/);
  });
});

describe('the app composition renders the registry shell under the ambient localHost', () => {
  it('mounts the space through SproutWorkspace with a single ambient stack', async () => {
    // A faithful stand-in for the app's composition: HostProvider(localHost)
    // + SproutProviders + SproutWorkspace(providers="ambient"), exactly as
    // index.tsx + App.tsx + AppContent.tsx nest them.
    render(
      <HostProvider host={localHost}>
        <SproutProviders eventsProvider={makeEvents()} isConnected>
          <SproutWorkspace providers="ambient" project={{ id: '/workspace/project' }} space="code" />
        </SproutProviders>
      </HostProvider>,
    );
    const workspace = await screen.findByTestId('sprout-workspace');
    expect(workspace).toHaveAttribute('data-project', '/workspace/project');
    expect(workspace).toHaveAttribute('data-space', 'code');
    // The registry shell renders (the fake composition uses the real registry
    // and real providers); `SproutWorkspace.test.tsx` asserts the shell identity.
  });
});

/** A minimal events transport so `SproutProviders` needs no default (and no WebSocket). */
function makeEvents() {
  return {
    connect: () => undefined,
    disconnect: () => undefined,
    onEvent: () => undefined,
    removeEvent: () => undefined,
    sendEvent: () => undefined,
    isConnected: () => true,
    onReconnect: () => undefined,
    freeze: () => undefined,
    resume: () => undefined,
    resetAndReconnect: () => undefined,
    getQueuedMessageCount: () => 0,
  } as unknown as Parameters<typeof SproutProviders>[0]['eventsProvider'];
}
