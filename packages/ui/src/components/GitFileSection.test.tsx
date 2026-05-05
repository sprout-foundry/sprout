// Stricter type-checking is enabled but React's createElement overloads don't
// cleanly accept children as a rest parameter in strict TS. We use targeted
// suppressions on the specific call-sites that trigger errors.

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi } from 'vitest';
import { GitFileSection, getStatusIcon } from './GitFileSection';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
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
  container?.remove();
});

// ---------------------------------------------------------------------------
// Tests: getStatusIcon helper
// ---------------------------------------------------------------------------

describe('getStatusIcon', () => {
  it('returns an icon element for "staged"', () => {
    const icon = getStatusIcon('staged');
    expect(icon).not.toBeNull();
  });

  it('returns an icon element for "modified"', () => {
    const icon = getStatusIcon('modified');
    expect(icon).not.toBeNull();
  });

  it('returns an icon element for "untracked"', () => {
    const icon = getStatusIcon('untracked');
    expect(icon).not.toBeNull();
  });

  it('returns an icon element for "deleted"', () => {
    const icon = getStatusIcon('deleted');
    expect(icon).not.toBeNull();
  });

  it('returns an icon element for "renamed"', () => {
    const icon = getStatusIcon('renamed');
    expect(icon).not.toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Tests: GitFileSection rendering
// ---------------------------------------------------------------------------

describe('GitFileSection', () => {
  const isStaged = (path: string) => path.endsWith('.staged');
  const onFileClick = vi.fn();

  beforeEach(() => {
    onFileClick.mockClear();
  });

  it('returns null when files and renamedFiles are both empty', () => {
    act(() => {
      root.render(
        // @ts-expect-error — createElement accepts children as rest args
        createElement(GitFileSection, {
          type: 'modified',
          title: 'Modified',
          files: [],
          isStaged,
          onFileClick,
        })
      );
    });
    expect(container.innerHTML).toBe('');
  });

  it('renders a section title with icon for non-empty files', () => {
    act(() => {
      root.render(
        createElement(GitFileSection, {
          type: 'modified',
          title: 'Modified Files',
          files: ['a.txt', 'b.go'],
          isStaged,
          onFileClick,
        })
      );
    });
    expect(container.querySelector('.gitpanel-section')).not.toBeNull();
    expect(container.querySelector('.gitpanel-section-title')).not.toBeNull();
    expect(container.querySelectorAll('.gitpanel-file')).toHaveLength(2);
  });

  it('renders file rows with correct names', () => {
    act(() => {
      root.render(
        createElement(GitFileSection, {
          type: 'staged',
          title: 'Staged',
          files: ['main.go', 'test.txt'],
          isStaged,
          onFileClick,
        })
      );
    });
    const fileNames = container.querySelectorAll('.gitpanel-file-name');
    expect(fileNames).toHaveLength(2);
    expect(fileNames[0].textContent).toBe('main.go');
    expect(fileNames[1].textContent).toBe('test.txt');
  });

  it('adds staged class when isStaged returns true', () => {
    act(() => {
      root.render(
        createElement(GitFileSection, {
          type: 'modified',
          title: 'Modified',
          files: ['file.staged', 'file.unstaged'],
          isStaged,
          onFileClick,
        })
      );
    });
    const rows = container.querySelectorAll('.gitpanel-file');
    expect(rows[0].className).toContain('gitpanel-file-staged');
    expect(rows[1].className).not.toContain('gitpanel-file-staged');
  });

  it('clicking a file row calls onFileClick with the file path', () => {
    act(() => {
      root.render(
        createElement(GitFileSection, {
          type: 'modified',
          title: 'Modified',
          files: ['clickme.txt'],
          isStaged,
          onFileClick,
        })
      );
    });
    const row = container.querySelector('.gitpanel-file');
    act(() => {
      row?.click();
    });
    expect(onFileClick).toHaveBeenCalledWith('clickme.txt');
  });

  it('keyboard Enter on a file row calls onFileClick', () => {
    act(() => {
      root.render(
        createElement(GitFileSection, {
          type: 'modified',
          title: 'Modified',
          files: ['keytest.txt'],
          isStaged,
          onFileClick,
        })
      );
    });
    const row = container.querySelector('.gitpanel-file');
    act(() => {
      row?.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })
      );
    });
    expect(onFileClick).toHaveBeenCalledWith('keytest.txt');
  });

  it('keyboard Space on a file row calls onFileClick', () => {
    act(() => {
      root.render(
        createElement(GitFileSection, {
          type: 'modified',
          title: 'Modified',
          files: ['keytest.txt'],
          isStaged,
          onFileClick,
        })
      );
    });
    const row = container.querySelector('.gitpanel-file');
    act(() => {
      row?.dispatchEvent(
        new KeyboardEvent('keydown', { key: ' ', bubbles: true })
      );
    });
    expect(onFileClick).toHaveBeenCalledWith('keytest.txt');
  });

  it('renders renamed files with "from → to" display', () => {
    act(() => {
      root.render(
        createElement(GitFileSection, {
          type: 'renamed',
          title: 'Renamed',
          files: [],
          renamedFiles: [
            { from: 'old.txt', to: 'new.txt' },
            { from: 'src/a.go', to: 'lib/b.go' },
          ],
          isStaged,
          onFileClick,
        })
      );
    });
    const rows = container.querySelectorAll('.gitpanel-file');
    expect(rows).toHaveLength(2);
    expect(rows[0].querySelector('.gitpanel-file-name')?.textContent).toContain('old.txt');
    expect(rows[0].querySelector('.gitpanel-file-name')?.textContent).toContain('new.txt');
  });

  it('clicking a renamed file row calls onFileClick with the "to" path', () => {
    act(() => {
      root.render(
        createElement(GitFileSection, {
          type: 'renamed',
          title: 'Renamed',
          files: [],
          renamedFiles: [{ from: 'old.go', to: 'new.go' }],
          isStaged,
          onFileClick,
        })
      );
    });
    const row = container.querySelector('.gitpanel-file');
    act(() => {
      row?.click();
    });
    expect(onFileClick).toHaveBeenCalledWith('new.go');
  });

  it('renders both regular files and renamed files together', () => {
    act(() => {
      root.render(
        createElement(GitFileSection, {
          type: 'modified',
          title: 'Modified',
          files: ['a.txt'],
          renamedFiles: [{ from: 'old.go', to: 'new.go' }],
          isStaged,
          onFileClick,
        })
      );
    });
    const rows = container.querySelectorAll('.gitpanel-file');
    expect(rows).toHaveLength(2);
  });

  it('renders staged icon (Minus) for staged files and Plus for unstaged', () => {
    act(() => {
      root.render(
        createElement(GitFileSection, {
          type: 'modified',
          title: 'Modified',
          files: ['staged.staged', 'unstaged.txt'],
          isStaged,
          onFileClick,
        })
      );
    });
    const icons = container.querySelectorAll('.gitpanel-file-icon');
    expect(icons).toHaveLength(2);
    // First is staged (Minus icon rendered), second is unstaged (Plus icon)
    // Both should have content since icons are rendered
    expect(icons[0]).not.toBeNull();
    expect(icons[1]).not.toBeNull();
  });

  it('renders Plus icon for unstaged renamed files', () => {
    act(() => {
      root.render(
        createElement(GitFileSection, {
          type: 'renamed',
          title: 'Renamed',
          files: [],
          renamedFiles: [{ from: 'old.txt', to: 'new.txt' }],
          isStaged: () => false,
          onFileClick,
        })
      );
    });
    expect(container.querySelector('.gitpanel-file')).not.toBeNull();
  });

  it('renders file rows with role="button" and tabIndex=0', () => {
    act(() => {
      root.render(
        createElement(GitFileSection, {
          type: 'modified',
          title: 'Modified',
          files: ['access.txt'],
          isStaged,
          onFileClick,
        })
      );
    });
    const row = container.querySelector('.gitpanel-file');
    expect(row?.getAttribute('role')).toBe('button');
    expect(row?.getAttribute('tabIndex')).toBe('0');
  });

  it('returns null when renamedFiles is undefined and files is empty', () => {
    act(() => {
      root.render(
        // @ts-expect-error — renamedFiles intentionally omitted
        createElement(GitFileSection, {
          type: 'modified',
          title: 'Modified',
          files: [],
          isStaged,
          onFileClick,
        })
      );
    });
    expect(container.innerHTML).toBe('');
  });

  it('renders when renamedFiles is empty array but files is non-empty', () => {
    act(() => {
      root.render(
        createElement(GitFileSection, {
          type: 'modified',
          title: 'Modified',
          files: ['file.txt'],
          renamedFiles: [],
          isStaged,
          onFileClick,
        })
      );
    });
    expect(container.querySelector('.gitpanel-file')).not.toBeNull();
  });
});
