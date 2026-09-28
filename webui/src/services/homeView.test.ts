import { beforeEach, describe, expect, it } from 'vitest';
import { __resetHomeViewForTests, closeHome, getHomeView, normalizeHomePath, openHome, syncHomePath } from './homeView';

beforeEach(() => __resetHomeViewForTests());

describe('homeView', () => {
  it('normalizes the platform route from any link form', () => {
    expect(normalizeHomePath('/?from=editor#/tasks')).toBe('/tasks');
    expect(normalizeHomePath('#/account/billing')).toBe('/account/billing');
    expect(normalizeHomePath('/?from=editor')).toBe('/');
    expect(normalizeHomePath('team')).toBe('/team');
  });

  it('keeps the last page across close and reopen', () => {
    openHome('/tasks');
    syncHomePath('/tasks/abc');
    closeHome();
    expect(getHomeView()).toEqual({ open: false, path: '/tasks/abc' });
    openHome(getHomeView().path);
    expect(getHomeView()).toEqual({ open: true, path: '/tasks/abc' });
  });

  it('mirrors Home into the editor URL', () => {
    window.history.replaceState(null, '', '/webui/?repo=x');
    openHome('/tasks');
    expect(new URL(window.location.href).searchParams.get('home')).toBe('/tasks');
    expect(new URL(window.location.href).searchParams.get('repo')).toBe('x');
    closeHome();
    expect(new URL(window.location.href).searchParams.has('home')).toBe(false);
  });
});
