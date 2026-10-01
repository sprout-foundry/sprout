// @ts-nocheck
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../config/mode', () => ({ isCloud: true }));
vi.mock('./CredentialsSettingsTab', () => ({ default: () => <div data-testid="credentials-tab" /> }));
vi.mock('./GitHubAccountPanel', () => ({ default: () => null }));
vi.mock('./SettingsPanel', () => ({ default: () => null }));
vi.mock('./EditorModelSection', () => ({ default: () => <div data-testid="editor-model-section" /> }));
vi.mock('../utils/log', async (importOriginal) => ({
  ...(await importOriginal()),
  useLog: () => ({ error: vi.fn(), warn: vi.fn(), info: vi.fn(), success: vi.fn() }),
}));

import SidebarSettingsSection from './SidebarSettingsSection';

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const noop = () => undefined;

describe('SidebarSettingsSection on a platform-hosted browser workspace', () => {
  it('offers the account-level model choice instead of the local provider pickers', async () => {
    await act(async () => {
      root.render(
        <SidebarSettingsSection
          themePack={{ id: 'default' }}
          availableThemePacks={[{ id: 'default', name: 'Default' }]}
          setThemePack={noop}
          importTheme={() => ({ success: true })}
          removeTheme={noop}
          uiScale="default"
          setUIScale={noop}
          applyPreset={async () => undefined}
          autoSaveEnabled={false}
          whitespaceRenderingMode="none"
          formatOnSaveEnabled={false}
          setAutoSaveEnabled={noop}
          setWhitespaceRenderingMode={noop}
          setFormatOnSaveEnabled={noop}
          selectedProvider=""
          selectedModel=""
          providers={[]}
          availableModels={[]}
          isLoadingProviders={false}
          isConnected
          onProviderChange={noop}
          onModelChange={noop}
        />,
      );
    });
    expect(container.querySelector('[data-testid="editor-model-section"]')).not.toBeNull();
    expect(container.querySelector('#provider-select')).toBeNull();
    expect(container.querySelector('[data-testid="credentials-tab"]')).toBeNull();
  });
});
