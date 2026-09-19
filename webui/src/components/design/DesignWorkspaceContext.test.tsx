/**
 * DesignWorkspaceContext (SP-140-5 unified layout) and the sidebar assets
 * pane that consumes it.
 *
 * Pins the contract the unified layout depends on:
 * - the provider fetches the inventory only while Design mode is active
 * - `select` is one state that the pane renders (row highlight + testids the
 *   e2e specs drive)
 * - outside a provider the pane renders nothing (hosts without the shell)
 */

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { SproutAdapterProvider } from '../../contexts/SproutAdapterContext';
import * as designApiModule from '../../services/api/designApi';
import type { DesignInventory } from '../../services/api/types/design';
import { assetDisplayName, assetMatchesSection } from './assetNames';
import DesignAssetsPane from './DesignAssetsPane';
import { DesignWorkspaceProvider, useDesignWorkspace } from './DesignWorkspaceContext';

vi.mock('../../services/api/designApi', async (importOriginal) => {
  const actual = (await importOriginal()) as typeof designApiModule;
  const entry = (path: string, kind: string) => ({
    path,
    name: path.split('/').pop(),
    kind,
    size: 128,
    modified: 0,
    status: '',
  });
  const inventory: DesignInventory = {
    exists: true,
    wireframes: [],
    layouts: [],
    screens: [entry('screens/inbox.html', 'screen')],
    flows: [entry('flows/sign-up.mmd', 'flow')],
    tokenFiles: [entry('tokens/colors.json', 'tokens')],
    flowSummaries: [],
    feedback: [],
  };
  return { ...actual, listAssets: vi.fn().mockResolvedValue(inventory) };
});

const listAssets = vi.mocked(designApiModule.listAssets);

function renderPane(tab: 'flows' | 'screens' | 'tokens', active = true) {
  return render(
    <SproutAdapterProvider>
      <DesignWorkspaceProvider tab={tab} active={active}>
        <DesignAssetsPane />
      </DesignWorkspaceProvider>
    </SproutAdapterProvider>,
  );
}

describe('assetNames', () => {
  it('strips serialization extensions from asset names', () => {
    expect(assetDisplayName('sign-up.mmd')).toBe('sign-up');
    expect(assetDisplayName('login.html')).toBe('login');
    expect(assetDisplayName('color.tokens.json')).toBe('color');
    expect(assetDisplayName('brand.svg')).toBe('brand');
    expect(assetDisplayName('no-extension')).toBe('no-extension');
  });

  it('matches a section by path segment, whatever the path root', () => {
    expect(assetMatchesSection('design/flows/sign-up.mmd', 'flows')).toBe(true);
    expect(assetMatchesSection('flows/sign-up.mmd', 'flows')).toBe(true);
    expect(assetMatchesSection('design/screens/login.html', 'flows')).toBe(false);
  });
});

describe('DesignAssetsPane', () => {
  it('lists the active section assets with extension-free names', async () => {
    renderPane('flows');
    expect(await screen.findByText('sign-up')).toBeInTheDocument();
    expect(screen.getByTestId('design-rail-row-flows/sign-up.mmd')).toBeInTheDocument();
    expect(screen.queryByText('sign-up.mmd')).not.toBeInTheDocument();
  });

  it('switches the list with the section', async () => {
    const rendered = renderPane('flows');
    await screen.findByText('sign-up');

    rendered.rerender(
      <SproutAdapterProvider>
        <DesignWorkspaceProvider tab="screens" active>
          <DesignAssetsPane />
        </DesignWorkspaceProvider>
      </SproutAdapterProvider>,
    );
    expect(await screen.findByText('inbox')).toBeInTheDocument();
    expect(screen.queryByText('sign-up')).not.toBeInTheDocument();
  });

  it('does not fetch while Design mode is inactive', async () => {
    listAssets.mockClear();
    renderPane('flows', false);
    await waitFor(() => expect(listAssets).not.toHaveBeenCalled());
    expect(screen.queryByTestId('design-rail-row-flows/sign-up.mmd')).not.toBeInTheDocument();
  });

  it('renders nothing outside a provider (hosts without the workspace shell)', () => {
    const { container } = render(<DesignAssetsPane />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe('DesignWorkspaceProvider', () => {
  it('fetches once while active and shares selection through the context', async () => {
    listAssets.mockClear();
    let selected: string | null = null;
    let select: ((path: string | null) => void) | null = null;

    function Probe() {
      const workspace = useDesignWorkspace();
      if (workspace) {
        selected = workspace.selected;
        select = workspace.select;
      }
      return <DesignAssetsPane />;
    }

    render(
      <SproutAdapterProvider>
        <DesignWorkspaceProvider tab="flows" active>
          <Probe />
        </DesignWorkspaceProvider>
      </SproutAdapterProvider>,
    );

    await waitFor(() => expect(listAssets).toHaveBeenCalledTimes(1));

    act(() => select?.('flows/sign-up.mmd'));
    expect(selected).toBe('flows/sign-up.mmd');
    expect(screen.getByTestId('design-rail-row-flows/sign-up.mmd')).toHaveClass('selected');
  });
});
