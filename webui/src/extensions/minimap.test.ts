/**
 * minimap.test.ts — Unit tests for the minimap extension.
 *
 * Since CodeMirror 6 modules use ESM and cannot load in Jest 27.x,
 * all CM imports are mocked.
 */

jest.mock('@codemirror/view', () => ({
  EditorView: { baseTheme: jest.fn(() => []) },
}));

jest.mock('@codemirror/state', () => ({}));

const mockComputeResult = Symbol('showMinimap.compute.result');
jest.mock('@replit/codemirror-minimap', () => ({
  showMinimap: {
    compute: jest.fn(() => mockComputeResult),
  },
}));

import { minimapExtension, showMinimap } from './minimap';
import { showMinimap as showMinimapOriginal } from '@replit/codemirror-minimap';

describe('minimap', () => {
  it('returns an array with 2 elements', () => {
    const ext = minimapExtension();
    expect(Array.isArray(ext)).toBe(true);
    expect(ext).toHaveLength(2);
  });

  it('calls showMinimap.compute with ["doc"] key', () => {
    minimapExtension();
    expect(showMinimapOriginal.compute).toHaveBeenCalledWith(['doc'], expect.any(Function));
  });

  it('re-exports showMinimap', () => {
    expect(showMinimap).toBe(showMinimapOriginal);
  });

  it('the compute callback returns config with blocks display and always overlay', () => {
    minimapExtension();
    const computeCall = (showMinimapOriginal.compute as jest.Mock).mock.calls[0];
    const callbackFn = computeCall[1];
    const config = callbackFn(null as any);
    expect(config.displayText).toBe('blocks');
    expect(config.showOverlay).toBe('always');
    expect(typeof config.create).toBe('function');
  });

  it('the create callback returns a div with class cm-minimap-container', () => {
    minimapExtension();
    const computeCall = (showMinimapOriginal.compute as jest.Mock).mock.calls[0];
    const callbackFn = computeCall[1];
    const config = callbackFn(null as any);
    const result = config.create({} as any);
    expect(result.dom).toBeInstanceOf(HTMLDivElement);
    expect(result.dom.className).toBe('cm-minimap-container');
  });
});
