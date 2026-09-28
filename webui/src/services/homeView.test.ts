import { beforeEach, describe, expect, it } from 'vitest';
import {
  __resetHomeViewForTests,
  closeHome,
  getHomeView,
  normalizeHomePath,
  openHome,
  searchForRepo,
  syncHomePath,
} from './homeView';

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

  it('records entering and leaving Home as history entries, not page moves', () => {
    window.history.replaceState(null, '', '/webui/');
    const start = window.history.length;
    openHome('/');
    openHome('/tasks');
    syncHomePath('/tasks/1');
    closeHome();
    expect(window.history.length - start).toBe(2);
  });

  it('follows Back and Forward', () => {
    window.history.replaceState(null, '', '/webui/?home=%2Ftasks');
    window.dispatchEvent(new PopStateEvent('popstate'));
    expect(getHomeView()).toEqual({ open: true, path: '/tasks' });
    window.history.replaceState(null, '', '/webui/');
    window.dispatchEvent(new PopStateEvent('popstate'));
    expect(getHomeView().open).toBe(false);
  });

  it('leaves Home behind when opening another repository', () => {
    window.history.replaceState(null, '', '/webui/?layout=layered&home=%2Ftasks&repo=old');
    const params = new URLSearchParams(searchForRepo('https://github.com/acme/new'));
    expect(params.get('repo')).toBe('https://github.com/acme/new');
    expect(params.get('layout')).toBe('layered');
    expect(params.has('home')).toBe(false);
  });
});
