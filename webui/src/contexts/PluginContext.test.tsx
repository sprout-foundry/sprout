// @ts-nocheck
/**
 * Regression test: PluginContext must reflect plugins registered AFTER the
 * provider mounts (external IIFE plugin bundles register asynchronously,
 * well after the first render).
 *
 * Previously the provider's useMemo depended on the stable `setTick` setter
 * identity instead of the tick value, so `pluginViews` was computed once
 * (empty) at mount and never updated — the sidebar could never render
 * in-app plugin views in cloud mode.
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import type { PluginView } from '../types/plugin';
import { pluginRegistry } from '../services/pluginRegistry';
import { PluginContextProvider, usePlugins } from './PluginContext';

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;
let latest: {
  pluginViews: readonly PluginView[];
  pluginPanels: unknown[];
  pluginSettingsTabs: unknown[];
} | undefined;

const FAKE_COMPONENT = () => createElement('div', null, 'fake');

function makePlugin(id: string) {
  return {
    id,
    views: [
      {
        id: `${id}-home`,
        label: `${id} Home`,
        order: 1,
        component: FAKE_COMPONENT,
      },
    ],
    panels: [
      {
        id: `${id}-panel`,
        label: `${id} Panel`,
        component: FAKE_COMPONENT,
      },
    ],
  };
}

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  latest = undefined;
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  // Clean up any plugins this test registered so the singleton registry
  // doesn't leak state into other tests.
  for (const id of pluginRegistry.getPluginIds()) {
    pluginRegistry.unregister(id);
  }
});

function TestConsumer() {
  const ctx = usePlugins();
  latest = ctx;
  return createElement('div', { 'data-testid': 'consumer' });
}

function renderProvider() {
  act(() => {
    root.render(createElement(PluginContextProvider, null, createElement(TestConsumer)));
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('PluginContext — late plugin registration (cloud mode plugin bundles)', () => {
  it('starts with empty views/panels when nothing is registered', () => {
    renderProvider();

    expect(latest).toBeDefined();
    expect(latest!.pluginViews).toHaveLength(0);
    expect(latest!.pluginPanels).toHaveLength(0);
  });

  it('picks up a plugin registered AFTER mount WITHOUT remounting', () => {
    // 1. Mount with an empty registry.
    renderProvider();
    expect(latest!.pluginViews).toHaveLength(0);

    // 2. Simulate the external IIFE bundle registering itself. This is the
    //    exact path a plugin bundle takes (window.__sproutRegisterPlugin).
    const plugin = makePlugin('late-test');
    act(() => {
      (window as unknown as { __sproutRegisterPlugin: (p: unknown) => void }).__sproutRegisterPlugin(
        plugin,
      );
    });

    // 3. The same mounted consumer must now see the plugin's views/panels.
    expect(latest!.pluginViews).toHaveLength(1);
    expect(latest!.pluginViews[0].id).toBe('late-test-home');
    expect(latest!.pluginPanels).toHaveLength(1);

    // No remount: the consumer div is still the original node.
    expect(container.querySelector('[data-testid="consumer"]')).not.toBeNull();
  });

  it('reflects unregistering a plugin after mount', () => {
    const plugin = makePlugin('unreg-test');
    pluginRegistry.register(plugin);

    renderProvider();
    expect(latest!.pluginViews).toHaveLength(1);

    act(() => {
      pluginRegistry.unregister('unreg-test');
    });

    expect(latest!.pluginViews).toHaveLength(0);
  });
});