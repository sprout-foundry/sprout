import '@testing-library/jest-dom';
import { vi } from 'vitest';

// ── Jest compatibility shim ────────────────────────────────────────────────
// The test files use `jest.mock()` and `jest.fn()` — vitest doesn't provide
// a `jest` global by default. This shim maps them to vitest equivalents so
// existing test code continues to work without modification.

// @ts-expect-error — jest is a compatibility global
global.jest = {
  fn: vi.fn,
  mock: vi.mock,
  spyOn: vi.spyOn,
  clearAllMocks: vi.clearAllMocks,
};

// Mock window.matchMedia
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: vi.fn().mockImplementation(query => ({
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
const originalCreateRange = document.createRange;
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
const originalGetClientRects = Element.prototype.getClientRects;
Element.prototype.getClientRects = vi.fn(function() {
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
const originalGetBoundingClientRect = Element.prototype.getBoundingClientRect;
Element.prototype.getBoundingClientRect = vi.fn(function() {
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

// Mock requestAnimationFrame
global.requestAnimationFrame = vi.fn(callback => {
  return setTimeout(callback, 0);
});

// Mock cancelAnimationFrame
global.cancelAnimationFrame = vi.fn(id => {
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

// Reset all mocks before each test
beforeEach(() => {
  vi.clearAllMocks();
});

// Restore original implementations after each test
afterEach(() => {
  document.createRange = originalCreateRange;
  Element.prototype.getClientRects = originalGetClientRects;
  Element.prototype.getBoundingClientRect = originalGetBoundingClientRect;
});
