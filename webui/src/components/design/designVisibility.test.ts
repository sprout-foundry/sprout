/**
 * SP-140-3 item 3.3 — DesignView visibility rule (pure functions).
 *
 * The rule ("visible only when design/ exists") is shared by the Sidebar nav
 * affordance and the EditorWorkspace route, so it is pinned here directly
 * rather than through either consumer.
 */

import { describe, expect, it } from 'vitest';
import { DESIGN_DIR, hasDesignDir, isDesignPath } from './designVisibility';
import type { FilesResponse } from '../../services/api';

function files(paths: string[]): FilesResponse {
  return { message: 'ok', files: paths.map((path) => ({ path, modified: false })) };
}

describe('DESIGN_DIR', () => {
  it('is the workspace design directory', () => {
    expect(DESIGN_DIR).toBe('design');
  });
});

describe('isDesignPath', () => {
  it.each([
    'design/',
    'design',
    'design/README.md',
    'design/tokens/color.tokens.json',
    './design/flows/login.mmd',
    '/design/screens/login.html',
    'workspaces/demo/design/wireframes/login.svg',
    'nested\\design\\wireframes\\login.svg',
  ])('matches %s', (path) => {
    expect(isDesignPath(path)).toBe(true);
  });

  it.each([
    '',
    'designs/README.md',
    'design.md',
    'src/designer/foo.ts',
    'README.md',
    'docs/design-system.md',
    'assets/designs/login.svg',
  ])('does not match %s', (path) => {
    expect(isDesignPath(path)).toBe(false);
  });
});

describe('hasDesignDir', () => {
  it('returns false for a missing or empty file list', () => {
    expect(hasDesignDir(null)).toBe(false);
    expect(hasDesignDir(undefined)).toBe(false);
    expect(hasDesignDir({ message: 'ok', files: [] })).toBe(false);
    // Defensive: malformed payloads must not throw.
    expect(hasDesignDir({ message: 'ok' } as FilesResponse)).toBe(false);
  });

  it('returns true when the directory entry itself is listed', () => {
    expect(hasDesignDir(files(['src/App.tsx', 'design/']))).toBe(true);
    expect(hasDesignDir(files(['design']))).toBe(true);
  });

  it('returns true when only nested files are listed', () => {
    expect(hasDesignDir(files(['README.md', 'design/wireframes/login.svg']))).toBe(true);
  });

  it('returns false when no design/ directory exists', () => {
    expect(hasDesignDir(files(['README.md', 'src/designer/foo.ts', 'docs/design-system.md']))).toBe(false);
    expect(hasDesignDir(files(['src/App.tsx', 'package.json']))).toBe(false);
  });

  it('ignores entries without a usable path', () => {
    const payload = { message: 'ok', files: [{ path: '', modified: false }] } as FilesResponse;
    expect(hasDesignDir(payload)).toBe(false);
  });
});
