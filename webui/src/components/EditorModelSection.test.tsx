import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

const api = vi.hoisted(() => ({
  state: { provider: '', model: '', key_providers: ['openai'], recommend_byok: true },
  setEditorModel: vi.fn(),
  listProviderModels: vi.fn(),
}));
vi.mock('../services/editorModel', async (orig) => ({
  ...(await orig<typeof import('../services/editorModel')>()),
  getEditorModel: () => Promise.resolve({ ...api.state }),
  setEditorModel: (c: unknown) => api.setEditorModel(c),
  listProviderModels: (p: string) => api.listProviderModels(p),
}));
vi.mock('../services/platformProvider', () => ({ loadManagedContextWindow: vi.fn() }));

import { HostProvider } from '../host/HostProvider';
import { makeTestHost } from '../host/testHost';
import { PLATFORM_ACCOUNT_ITEMS, intentPath } from '../host/platform';
import EditorModelSection from './EditorModelSection';

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  api.state = { provider: '', model: '', key_providers: ['openai'], recommend_byok: true };
  api.setEditorModel.mockReset().mockResolvedValue(undefined);
  api.listProviderModels.mockReset().mockResolvedValue([{ id: 'gpt-x' }, { id: 'gpt-y' }]);
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

async function render() {
  const host = {
    ...makeTestHost(),
    navigation: { open: () => undefined, accountItems: PLATFORM_ACCOUNT_ITEMS, intentPath },
  };
  await act(async () => {
    root.render(
      <HostProvider host={host}>
        <EditorModelSection />
      </HostProvider>,
    );
  });
}

const radios = () => container.querySelectorAll<HTMLInputElement>('input[type="radio"]');

function setInput(el: HTMLInputElement, value: string) {
  act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value);
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

describe('EditorModelSection', () => {
  it('switches the editor to a model from the saved key', async () => {
    await render();
    expect(radios()[0].checked).toBe(true);

    await act(async () => radios()[1].click());
    expect(api.listProviderModels).toHaveBeenCalledWith('openai');
    const options = [...container.querySelectorAll('datalist option')].map((o) => o.getAttribute('value'));
    expect(options).toEqual(['gpt-x', 'gpt-y']);

    setInput(container.querySelector<HTMLInputElement>('input[aria-label="Model"]')!, 'gpt-x');
    await act(async () => {
      container.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
    expect(api.setEditorModel).toHaveBeenCalledWith({ provider: 'openai', model: 'gpt-x' });
    expect(container.textContent).toContain('Saved');
  });

  it('asks for a saved key first when there is none', async () => {
    api.state.key_providers = [];
    await render();
    await act(async () => radios()[1].click());
    expect(container.textContent).toContain('Save an API key first');
    expect(container.querySelector<HTMLButtonElement>('button[type="submit"]')!.disabled).toBe(true);
  });
});
