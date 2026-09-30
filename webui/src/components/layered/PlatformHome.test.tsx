import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { __resetHomeViewForTests, openHome } from '../../services/homeView';
import PlatformHome from './PlatformHome';

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  __resetHomeViewForTests();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe('PlatformHome', () => {
  it('loads the page Home first opens to', () => {
    act(() => root.render(<PlatformHome />));
    expect(container.querySelector('iframe')).toBeNull();

    act(() => openHome('/account/billing'));
    expect(container.querySelector('iframe')?.getAttribute('src')).toContain('#/account/billing');
  });
});
