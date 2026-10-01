import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

const openHome = vi.fn();
vi.mock('../../services/homeView', () => ({ openHome: (p: string) => openHome(p) }));

import NewProjectDialog, { toRepoName } from './NewProjectDialog';

let container: HTMLDivElement;
let root: Root;
const fetchMock = vi.fn();

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  fetchMock.mockReset();
  openHome.mockReset();
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
});

const json = (status: number, body: unknown) =>
  Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }));

async function render(onCreated = vi.fn(), onClose = vi.fn()) {
  await act(async () => {
    root.render(<NewProjectDialog onCreated={onCreated} onClose={onClose} />);
  });
  return { onCreated, onClose };
}

function typeName(value: string) {
  const input = container.querySelector<HTMLInputElement>('input[aria-label="Repository name"]')!;
  act(() => {
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
    setter.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

async function submit() {
  await act(async () => {
    container.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  });
}

describe('toRepoName', () => {
  it('keeps what GitHub keeps', () => {
    expect(toRepoName('My App!')).toBe('My-App');
    expect(toRepoName('api_v2.0')).toBe('api_v2.0');
  });
});

describe('NewProjectDialog', () => {
  it('creates a private repository and hands back its URL', async () => {
    fetchMock.mockImplementation((url: string, init?: RequestInit) =>
      init?.method === 'POST'
        ? json(201, { html_url: 'https://github.com/ada/new-app' })
        : json(200, { github_connected: true }),
    );
    const { onCreated } = await render();
    typeName('new app');
    await submit();

    const post = fetchMock.mock.calls.find(([, init]) => init?.method === 'POST')!;
    expect(String(post[0])).toContain('/user/me/repos');
    expect(JSON.parse(post[1].body)).toEqual({ name: 'new-app', private: true });
    expect(onCreated).toHaveBeenCalledWith('https://github.com/ada/new-app');
  });

  it('shows why a name was refused', async () => {
    fetchMock.mockImplementation((_url: string, init?: RequestInit) =>
      init?.method === 'POST'
        ? json(409, { error: 'name already exists on this account', code: 'name_unavailable' })
        : json(200, { github_connected: true }),
    );
    const { onCreated } = await render();
    typeName('taken');
    await submit();

    expect(container.querySelector('[role="alert"]')?.textContent).toBe('name already exists on this account');
    expect(onCreated).not.toHaveBeenCalled();
  });

  it('offers to connect GitHub when the account has none', async () => {
    fetchMock.mockImplementation(() => json(200, { github_connected: false }));
    const { onClose } = await render();

    const connect = [...container.querySelectorAll('button')].find((b) => b.textContent === 'Connect GitHub')!;
    act(() => connect.click());
    expect(openHome).toHaveBeenCalledWith('/settings');
    expect(onClose).toHaveBeenCalled();
  });
});
