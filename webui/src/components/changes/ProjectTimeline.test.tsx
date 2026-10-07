/**
 * ProjectTimeline tests — the timeline view renders change sets,
 * deploys, and checkpoints, and the restore action on a checkpoint is a
 * two-step confirm: a single click never restores, confirming does.
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import ProjectTimeline from './ProjectTimeline';
import type { ProjectTimelineEntry } from './timelineModel';

vi.mock('lucide-react', () => {
  const { createElement: h } = require('react');
  const icons = ['Bookmark', 'ChevronRight', 'Clock', 'ExternalLink', 'GitCommit', 'Inbox', 'Rocket', 'Undo2'];
  const result: Record<string, (props: unknown) => JSX.Element> = {};
  for (const name of icons) {
    result[name] = (props: unknown) => h('svg', { 'data-testid': `icon-${name.toLowerCase()}`, ...(props as object) });
  }
  return result;
});

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  // @ts-expect-error test env flag
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container.remove();
  vi.clearAllMocks();
});

function render(props: Parameters<typeof ProjectTimeline>[0]) {
  // eslint-disable-next-line testing-library/no-unnecessary-act
  act(() => {
    root.render(createElement(ProjectTimeline, props));
  });
}

const changeSet: ProjectTimelineEntry = {
  kind: 'change_set',
  id: 'rev-1',
  timestamp: '2026-01-01T00:00:00Z',
  summary: 'changed 2 files: a.go, b.go (+12/-3)',
  files: ['a.go', 'b.go'],
  scopeIds: ['scope-a'],
};

const deploy: ProjectTimelineEntry = {
  kind: 'deploy',
  id: 'dep-1',
  timestamp: '2026-01-02T00:00:00Z',
  summary: 'deployed 1.2.3 to production',
  deployKind: 'production',
  version: '1.2.3',
  url: 'https://example.test',
};

const checkpoint: ProjectTimelineEntry = {
  kind: 'checkpoint',
  id: 'cp-1',
  timestamp: '2026-01-03T00:00:00Z',
  summary: 'checkpoint (verification): changed 2 files @ rev-1',
  origin: 'verification',
  revisionId: 'rev-1',
};

const noRevisionCheckpoint: ProjectTimelineEntry = {
  kind: 'checkpoint',
  id: 'cp-empty',
  timestamp: '2026-01-04T00:00:00Z',
  summary: 'checkpoint (manual): no files',
  origin: 'manual',
  revisionId: undefined,
};

describe('ProjectTimeline rendering', () => {
  it('renders an empty state with no entries', () => {
    render({ entries: [] });
    expect(container.querySelector('[data-testid="project-timeline"]')).not.toBeNull();
    expect(container.querySelector('.ptl-empty')!.textContent).toContain('No timeline entries yet.');
  });

  it('renders change set, deploy, and checkpoint entries with their summaries', () => {
    render({ entries: [changeSet, deploy, checkpoint] });
    const rows = container.querySelectorAll('[data-testid="ptl-row"]');
    expect(rows.length).toBe(3);
    const text = container.textContent ?? '';
    expect(text).toContain('changed 2 files: a.go, b.go (+12/-3)');
    expect(text).toContain('deployed 1.2.3 to production');
    expect(text).toContain('checkpoint (verification): changed 2 files @ rev-1');
    expect(text).toContain('Change set');
    expect(text).toContain('Deploy');
    expect(text).toContain('Checkpoint');
  });

  it('lists change-set files, collapsing the remainder', () => {
    render({
      entries: [{ ...changeSet, files: ['a.go', 'b.go', 'c.go', 'd.go'] } as ProjectTimelineEntry],
    });
    expect(container.querySelector('.ptl-files')!.textContent).toContain('a.go, b.go, c.go');
    expect(container.querySelector('.ptl-files')!.textContent).toContain('+1 more');
  });

  it('renders a deploy URL as a link', () => {
    render({ entries: [deploy] });
    const link = container.querySelector('.ptl-url') as HTMLAnchorElement;
    expect(link).not.toBeNull();
    expect(link.getAttribute('href')).toBe('https://example.test');
  });

  it('orders entries newest-first by default', () => {
    render({ entries: [changeSet, deploy, checkpoint] });
    const rows = Array.from(container.querySelectorAll('[data-testid="ptl-row"]'));
    expect(rows[0].className).toContain('ptl-row--checkpoint');
    expect(rows[2].className).toContain('ptl-row--change_set');
  });

  it('honors oldest-first ordering', () => {
    render({ entries: [changeSet, deploy, checkpoint], order: 'oldest_first' });
    const rows = Array.from(container.querySelectorAll('[data-testid="ptl-row"]'));
    expect(rows[0].className).toContain('ptl-row--change_set');
    expect(rows[2].className).toContain('ptl-row--checkpoint');
  });

  it('offers a restore action only on a checkpoint with a captured revision', () => {
    render({ entries: [changeSet, deploy, checkpoint, noRevisionCheckpoint], onRestore: vi.fn() });
    const restoreButtons = container.querySelectorAll('[data-testid="ptl-restore"]');
    expect(restoreButtons.length).toBe(1);
  });
});

describe('ProjectTimeline restore confirmation', () => {
  it('does not call onRestore on a single click of Restore', () => {
    const onRestore = vi.fn();
    render({ entries: [checkpoint], onRestore });

    const btn = container.querySelector('[data-testid="ptl-restore"]') as HTMLButtonElement;
    act(() => {
      btn.click();
    });

    expect(onRestore).not.toHaveBeenCalled();
    // The confirm step is now visible.
    expect(container.querySelector('[data-testid="ptl-restore-confirm"]')).not.toBeNull();
  });

  it('calls onRestore once the confirm step is clicked', () => {
    const onRestore = vi.fn();
    render({ entries: [checkpoint], onRestore });

    act(() => {
      (container.querySelector('[data-testid="ptl-restore"]') as HTMLButtonElement).click();
    });
    act(() => {
      (container.querySelector('[data-testid="ptl-restore-confirm-yes"]') as HTMLButtonElement).click();
    });

    expect(onRestore).toHaveBeenCalledTimes(1);
    expect(onRestore).toHaveBeenCalledWith('cp-1');
    // The confirm step is dismissed after confirming.
    expect(container.querySelector('[data-testid="ptl-restore-confirm"]')).toBeNull();
  });

  it('cancelling the confirm never calls onRestore', () => {
    const onRestore = vi.fn();
    render({ entries: [checkpoint], onRestore });

    act(() => {
      (container.querySelector('[data-testid="ptl-restore"]') as HTMLButtonElement).click();
    });
    act(() => {
      (container.querySelector('[data-testid="ptl-restore-cancel"]') as HTMLButtonElement).click();
    });

    expect(onRestore).not.toHaveBeenCalled();
    // Back to the un-armed state.
    expect(container.querySelector('[data-testid="ptl-restore-confirm"]')).toBeNull();
    expect(container.querySelector('[data-testid="ptl-restore"]')).not.toBeNull();
  });

  it('does not render the restore action when no onRestore callback is supplied', () => {
    render({ entries: [checkpoint] });
    expect(container.querySelector('[data-testid="ptl-restore"]')).toBeNull();
  });
});

describe('ProjectTimeline open callback', () => {
  it('calls onOpen with the entry when the summary is clicked', () => {
    const onOpen = vi.fn();
    render({ entries: [changeSet], onOpen });
    const btn = container.querySelector('.ptl-summary--button') as HTMLButtonElement;
    act(() => {
      btn.click();
    });
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(onOpen).toHaveBeenCalledWith(changeSet);
  });

  it('renders the summary as plain text when no onOpen is supplied', () => {
    render({ entries: [changeSet] });
    expect(container.querySelector('.ptl-summary--button')).toBeNull();
  });
});
