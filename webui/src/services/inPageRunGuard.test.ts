import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('./notificationBus', () => ({ notificationBus: { notify: vi.fn() } }));

import {
  noteInPageRun,
  reportInterruptedInPageRuns,
  resetInPageRunGuard,
  RUNNING_TITLE_PREFIX,
  takeInterruptedInPageRuns,
} from './inPageRunGuard';
import { notificationBus } from './notificationBus';

beforeEach(() => {
  window.sessionStorage.clear();
  resetInPageRunGuard();
  document.title = 'Sprout';
  vi.clearAllMocks();
});

afterEach(() => {
  resetInPageRunGuard();
});

function leavePrevented(): boolean {
  const event = new Event('beforeunload', { cancelable: true });
  window.dispatchEvent(event);
  return event.defaultPrevented;
}

describe('in-page run guard', () => {
  it('marks the title and guards unload only while a run is in flight', () => {
    noteInPageRun('a', true);
    noteInPageRun('b', true);
    expect(document.title).toBe(`${RUNNING_TITLE_PREFIX}Sprout`);
    expect(leavePrevented()).toBe(true);

    noteInPageRun('a', false);
    expect(document.title).toBe(`${RUNNING_TITLE_PREFIX}Sprout`);
    noteInPageRun('b', false);
    expect(document.title).toBe('Sprout');
    expect(leavePrevented()).toBe(false);
  });

  it('reports a run the page lost on reload, once', () => {
    noteInPageRun('chat-x', true);
    // A reload: the page's memory is gone, sessionStorage is not.
    resetInPageRunGuard();

    reportInterruptedInPageRuns();
    expect(notificationBus.notify).toHaveBeenCalledWith(
      'warning',
      'Run interrupted',
      expect.stringContaining('Send your message again'),
      expect.any(Number),
    );
    expect(takeInterruptedInPageRuns()).toEqual([]);
  });

  it('does not report runs that finished or are still running here', () => {
    noteInPageRun('done', true);
    noteInPageRun('done', false);
    noteInPageRun('live', true);
    reportInterruptedInPageRuns();
    expect(notificationBus.notify).not.toHaveBeenCalled();
  });
});
