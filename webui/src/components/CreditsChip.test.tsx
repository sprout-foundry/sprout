import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../config/mode', () => ({ isCloud: true }));
vi.mock('../bootstrapAdapter', () => ({ getPlatformURL: () => undefined }));

import { CreditsChip } from './CreditsChip';

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
  vi.unstubAllGlobals();
});

async function renderWith(body: unknown) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200 })));
  await act(async () => {
    root.render(<CreditsChip />);
  });
  await act(async () => {
    await Promise.resolve();
  });
}

describe('CreditsChip', () => {
  it('shows remaining credits and links to usage and billing', async () => {
    await renderWith({ ledger: 'v2', total_remaining: 450_000 });
    const chip = container.querySelector<HTMLAnchorElement>('[data-testid="header-credits-chip"]');
    expect(chip?.textContent).toBe('450K credits');
    expect(chip?.getAttribute('href')).toBe('/?from=editor#/account/billing');
  });

  it('renders nothing on the legacy ledger', async () => {
    await renderWith({ ledger: 'v1' });
    expect(container.querySelector('[data-testid="header-credits-chip"]')).toBeNull();
  });
});
