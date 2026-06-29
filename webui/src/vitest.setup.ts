import '@testing-library/jest-dom';
import { vi } from 'vitest';

// ── Vitest globals ──────────────────────────────────────────────────────────
// The test files now use `vi.mock()`, `vi.fn()`, etc. directly.
// This shim is kept as a safety net for any remaining `jest.*` references
// in comments or edge cases. New test files should use `vi.*` exclusively.

// Skip DOM-specific mocks when running in node environment (e.g., testids.test.ts).
// SP-087-3: test/webui/testids.test.ts uses the @vitest-environment node pragma
// and must not load any jsdom-only globals like window/document.
const isNodeEnv = typeof window === 'undefined';

// Hoisted originals so afterEach can restore them regardless of env.
// They stay undefined in node env; the afterEach guard handles that.
let originalCreateRange: typeof document.createRange | undefined;
let originalGetClientRects: typeof Element.prototype.getClientRects | undefined;
let originalGetBoundingClientRect: typeof Element.prototype.getBoundingClientRect | undefined;
let originalGetContext: typeof HTMLCanvasElement.prototype.getContext | undefined;

// @ts-expect-error — jest is a compatibility global (deprecated shim)
global.jest = {
  fn: vi.fn,
  mock: vi.mock,
  spyOn: vi.spyOn,
  clearAllMocks: vi.clearAllMocks,
  restoreAllMocks: vi.restoreAllMocks,
  useFakeTimers: vi.useFakeTimers,
  useRealTimers: vi.useRealTimers,
  advanceTimersByTime: vi.advanceTimersByTime,
  runAllTimers: vi.advanceTimersToNextTimer,
  resetModules: vi.resetModules,
  requireActual: vi.importActual,
  requireMock: vi.importMock,
};

// Mock window.matchMedia
if (!isNodeEnv) {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation((query) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  });

  // Mock ResizeObserver
  global.ResizeObserver = vi.fn().mockImplementation(() => ({
    observe: vi.fn(),
    unobserve: vi.fn(),
    disconnect: vi.fn(),
  }));

  // Mock IntersectionObserver
  global.IntersectionObserver = vi.fn().mockImplementation(() => ({
    observe: vi.fn(),
    unobserve: vi.fn(),
    disconnect: vi.fn(),
  }));

  // Mock File API
  global.File = class MockFile extends File {
    constructor(parts: any[], filename: string, properties?: any) {
      super(parts, filename, properties);
      Object.defineProperty(this, 'name', { value: filename, writable: false });
    }
  };

  // Mock Worker for CodeMirror
  global.Worker = vi.fn().mockImplementation(() => ({
    postMessage: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    terminate: vi.fn(),
  }));

  // Mock getClientRects for text ranges (CodeMirror requirement)
  originalCreateRange = document.createRange;
  document.createRange = vi.fn(() => {
    const range = originalCreateRange.call(document);
    return {
      ...range,
    getClientRects: vi.fn(() => ({
      length: 0,
      item: vi.fn(() => null),
      [Symbol.iterator]: vi.fn(function* () {}),
    })),
    getBoundingClientRect: vi.fn(() => ({
      left: 0,
      top: 0,
      right: 0,
      bottom: 0,
      width: 0,
      height: 0,
      x: 0,
      y: 0,
      toJSON: vi.fn(() => ({})),
    })),
    startContainer: null,
    endContainer: null,
    startOffset: 0,
    endOffset: 0,
    commonAncestorContainer: null,
    setStart: vi.fn(),
    setEnd: vi.fn(),
    setStartBefore: vi.fn(),
    setEndBefore: vi.fn(),
    setStartAfter: vi.fn(),
    setEndAfter: vi.fn(),
    collapse: vi.fn(),
    selectNode: vi.fn(),
    selectNodeContents: vi.fn(),
    deleteContents: vi.fn(),
    extractContents: vi.fn(),
    cloneContents: vi.fn(),
    insertNode: vi.fn(),
    surroundContents: vi.fn(),
    cloneRange: vi.fn(() => ({})),
    toString: vi.fn(() => ''),
    createContextualFragment: vi.fn(() => ({})),
  };
}) as () => Range;

// Mock Element.getClientRects
  originalGetClientRects = Element.prototype.getClientRects;
Element.prototype.getClientRects = vi.fn(function () {
  // For text nodes, return empty list
  if (this.nodeType === Node.TEXT_NODE) {
    return {
      length: 0,
      item: vi.fn(() => null),
      [Symbol.iterator]: vi.fn(function* () {}),
    };
  }
  return originalGetClientRects.call(this);
});

// Mock Element.getBoundingClientRect
  originalGetBoundingClientRect = Element.prototype.getBoundingClientRect;
Element.prototype.getBoundingClientRect = vi.fn(function () {
  if (this.nodeType === Node.TEXT_NODE) {
    return {
      left: 0,
      top: 0,
      right: 0,
      bottom: 0,
      width: 0,
      height: 0,
      x: 0,
      y: 0,
      toJSON: vi.fn(() => ({})),
    };
  }
  return originalGetBoundingClientRect.call(this);
});

// Mock canvas.getContext (used by @sprout/ui for icon rendering)
// jsdom doesn't implement canvas; without this mock, any component that
// calls canvas.getContext() throws and silently kills the React render.
  originalGetContext = HTMLCanvasElement.prototype.getContext;
HTMLCanvasElement.prototype.getContext = vi.fn(function (contextId: string) {
  if (contextId === '2d') {
    return {
      fillRect: vi.fn(),
      strokeRect: vi.fn(),
      clearRect: vi.fn(),
      fillText: vi.fn(),
      strokeText: vi.fn(),
      drawImage: vi.fn(),
      getImageData: vi.fn(() => ({ data: new Uint8ClampedArray(4) })),
      putImageData: vi.fn(),
      createImageData: vi.fn(() => ({ data: new Uint8ClampedArray(4), width: 0, height: 0 })),
      setTransform: vi.fn(),
      getTransform: vi.fn(() => ({ m11: 1, m12: 0, m21: 0, m22: 1, m41: 0, m42: 0, toString: () => '' })),
      save: vi.fn(),
      restore: vi.fn(),
      scale: vi.fn(),
      rotate: vi.fn(),
      translate: vi.fn(),
      transform: vi.fn(),
      beginPath: vi.fn(),
      moveTo: vi.fn(),
      lineTo: vi.fn(),
      arc: vi.fn(),
      closePath: vi.fn(),
      fill: vi.fn(),
      stroke: vi.fn(),
      clip: vi.fn(),
      measureText: vi.fn(() => ({ width: 0 })),
      createLinearGradient: vi.fn(() => ({ addColorStop: vi.fn() })),
      createRadialGradient: vi.fn(() => ({ addColorStop: vi.fn() })),
      rect: vi.fn(),
      lineWidth: 0,
      font: '',
      fillStyle: '',
      strokeStyle: '',
    };
  }
  return null;
}) as typeof originalGetContext;

// Mock OffscreenCanvas (used by @replit/codemirror-minimap for rendering)
// jsdom doesn't implement OffscreenCanvas; without this mock, minimap
// initialization throws and can crash vitest workers under memory pressure.
global.OffscreenCanvas = vi.fn().mockImplementation((width: number, height: number) => ({
  width,
  height,
  getContext: vi.fn((contextId: string) => {
    if (contextId === '2d') {
      return {
        fillRect: vi.fn(),
        strokeRect: vi.fn(),
        clearRect: vi.fn(),
        fillText: vi.fn(),
        strokeText: vi.fn(),
        drawImage: vi.fn(),
        getImageData: vi.fn(() => ({ data: new Uint8ClampedArray(4), width: 0, height: 0 })),
        putImageData: vi.fn(),
        createImageData: vi.fn(() => ({ data: new Uint8ClampedArray(4), width: 0, height: 0 })),
        setTransform: vi.fn(),
        getTransform: vi.fn(() => ({ m11: 1, m12: 0, m21: 0, m22: 1, m41: 0, m42: 0, toString: () => '' })),
        save: vi.fn(),
        restore: vi.fn(),
        scale: vi.fn(),
        rotate: vi.fn(),
        translate: vi.fn(),
        transform: vi.fn(),
        beginPath: vi.fn(),
        moveTo: vi.fn(),
        lineTo: vi.fn(),
        arc: vi.fn(),
        closePath: vi.fn(),
        fill: vi.fn(),
        stroke: vi.fn(),
        clip: vi.fn(),
        measureText: vi.fn(() => ({ width: 0 })),
        createLinearGradient: vi.fn(() => ({ addColorStop: vi.fn() })),
        createRadialGradient: vi.fn(() => ({ addColorStop: vi.fn() })),
        rect: vi.fn(),
        lineWidth: 0,
        font: '',
        fillStyle: '',
        strokeStyle: '',
      };
    }
    return null;
  }),
  transferToImageBitmap: vi.fn(),
})) as unknown as typeof OffscreenCanvas;

// Mock requestAnimationFrame
global.requestAnimationFrame = vi.fn((callback) => {
  return setTimeout(callback, 0);
});

// Mock cancelAnimationFrame
global.cancelAnimationFrame = vi.fn((id) => {
  clearTimeout(id);
});

// Mock scrollIntoView
Element.prototype.scrollIntoView = vi.fn();

// Mock focus
Element.prototype.focus = vi.fn();

// Mock blur
Element.prototype.blur = vi.fn();

// Mock click
Element.prototype.click = vi.fn();
} // end if (!isNodeEnv)

// Reset all mocks before each test
beforeEach(() => {
  vi.clearAllMocks();
});

// Restore original implementations after each test
afterEach(() => {
  if (isNodeEnv) return;
  document.createRange = originalCreateRange;
  Element.prototype.getClientRects = originalGetClientRects;
  Element.prototype.getBoundingClientRect = originalGetBoundingClientRect;
  HTMLCanvasElement.prototype.getContext = originalGetContext;
});
