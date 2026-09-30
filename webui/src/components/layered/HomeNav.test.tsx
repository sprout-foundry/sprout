import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

const { admin, workspaces } = vi.hoisted(() => ({ admin: { value: false }, workspaces: { value: true } }));
vi.mock('../../bootstrapAdapter', () => ({
  getBootstrapUser: () => ({ id: 'u', email: 'a@b.c', tier: 'pro', admin: admin.value }),
}));
vi.mock('../../services/fullWorkspace', () => ({ useFullWorkspacesAvailable: () => workspaces.value }));

import { __resetHomeViewForTests, getHomeView } from '../../services/homeView';
import HomeNav, { homePageLabel } from './HomeNav';

let container: HTMLDivElement;
let root: Root;
beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});
beforeEach(() => {
  __resetHomeViewForTests();
  admin.value = false;
  workspaces.value = true;
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});
afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const labels = () => Array.from(container.querySelectorAll('.project-nav-item')).map((b) => b.textContent?.trim());

describe('HomeNav', () => {
  it('highlights the section that owns the current route', () => {
    act(() => root.render(<HomeNav path="/tasks/abc" projectLabel="acme/app" onBackToProject={() => undefined} />));
    expect(container.querySelector('.project-nav-item.active')?.textContent).toContain('Tasks');
  });

  it('opens platform pages and goes back to the project', () => {
    const back = vi.fn();
    act(() => root.render(<HomeNav path="/" projectLabel="acme/app" onBackToProject={back} />));
    const billing = Array.from(container.querySelectorAll('.project-nav-item')).find((b) =>
      b.textContent?.includes('Usage & billing'),
    ) as HTMLButtonElement;
    act(() => billing.click());
    expect(getHomeView()).toEqual({ open: true, path: '/account/billing' });
    act(() => (container.querySelector('.project-nav-return') as HTMLButtonElement).click());
    expect(back).toHaveBeenCalled();
  });

  it('shows Admin to admins and hides Workspaces where the deployment has none', () => {
    admin.value = true;
    workspaces.value = false;
    act(() => root.render(<HomeNav path="/" projectLabel="acme/app" onBackToProject={() => undefined} />));
    expect(labels()).toContain('Admin');
    expect(labels()).not.toContain('Workspaces');
  });
});

describe('homePageLabel', () => {
  it.each([
    ['/', 'Dashboard'],
    ['/tasks/abc', 'Tasks'],
    ['/scheduled', 'Tasks'],
    ['/account/billing', 'Usage & billing'],
    ['/admin', 'Admin'],
    ['/nowhere', 'Home'],
    ['/team?invite=abc', 'Team'],
  ])('%s → %s', (path, label) => {
    expect(homePageLabel(path)).toBe(label);
  });
});
