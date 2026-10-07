/**
 * The WASM loading overlay only shows in a hosted shell (no local terminal)
 * while the in-browser runtime downloads/initializes. A local-terminal host
 * never renders it. Proves the flip with a localTerminal:true host (hidden)
 * vs a localTerminal:false host (shown when loading).
 */

import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { HostProvider } from '../host/HostProvider';
import { makeTestHost } from '../host/testHost';
import { WasmLoadingOverlay } from './WasmLoadingOverlay';

let container: HTMLDivElement;
let root: ReturnType<typeof createRoot>;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

describe('WasmLoadingOverlay', () => {
  it('never renders on a local-terminal host (even while loading)', async () => {
    await act(async () => {
      root.render(
        <HostProvider host={makeTestHost({ localTerminal: true })}>
          <WasmLoadingOverlay isLoading />
        </HostProvider>,
      );
    });
    expect(container.querySelector('.wasm-loading-overlay')).toBeNull();
  });

  it('renders while loading on a hosted shell (no local terminal)', async () => {
    await act(async () => {
      root.render(
        <HostProvider host={makeTestHost({ localTerminal: false })}>
          <WasmLoadingOverlay isLoading />
        </HostProvider>,
      );
    });
    expect(container.querySelector('.wasm-loading-overlay')).not.toBeNull();
  });

  it('hides when not loading and there is no error on a hosted shell', async () => {
    await act(async () => {
      root.render(
        <HostProvider host={makeTestHost({ localTerminal: false })}>
          <WasmLoadingOverlay isLoading={false} />
        </HostProvider>,
      );
    });
    expect(container.querySelector('.wasm-loading-overlay')).toBeNull();
  });

  it('shows the error state on a hosted shell', async () => {
    await act(async () => {
      root.render(
        <HostProvider host={makeTestHost({ localTerminal: false })}>
          <WasmLoadingOverlay isLoading={false} error="Wasm failed to initialize" />
        </HostProvider>,
      );
    });
    expect(container.querySelector('.wasm-loading-overlay')).not.toBeNull();
    expect(container.textContent).toContain('Runtime Error');
  });
});
