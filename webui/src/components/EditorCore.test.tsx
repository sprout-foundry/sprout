import { describe, it, expect, vi, beforeEach, afterEach, beforeAll, afterAll } from 'vitest';
import { act, createElement, RefObject, MutableRefObject } from 'react';
import { createRoot } from 'react-dom/client';
import type { EditorView as CMEditorView } from '@codemirror/view';

beforeAll(() => {
  // @ts-expect-error — assigning to undeclared globalThis property for React act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

// ── Mocks ────────────────────────────────────────────────────────
// All mocks return null to avoid React-version JSX issues in factories.

vi.mock('../hooks/useEditorViewInit', () => ({
  useEditorViewInit: vi.fn(),
}));

vi.mock('./useEditorReconfigure', () => ({
  useEditorReconfigure: vi.fn(),
}));

vi.mock('./MarkdownPreview', () => ({
  __esModule: true,
  default: () => null,
}));

vi.mock('@sprout/ui', () => ({
  Skeleton: () => null,
}));

vi.mock('lucide-react', () => ({
  AlertTriangle: () => null,
}));

// ── Imports after mocks ──────────────────────────────────────────
import { useEditorViewInit } from '../hooks/useEditorViewInit';
import { useEditorReconfigure } from './useEditorReconfigure';
import EditorCore, { areEditorCorePropsEqual } from './EditorCore';
import type { EditorCoreProps } from './EditorCore';

// ── Helpers ──────────────────────────────────────────────────────
function createBaseProps(overrides?: Partial<EditorCoreProps>): EditorCoreProps {
  return {
    editorRef: { current: null },
    viewRef: { current: null },
    initOptions: {} as EditorCoreProps['initOptions'],
    reconfigureOptions: {} as EditorCoreProps['reconfigureOptions'],
    loading: false,
    error: null,
    onContextMenu: vi.fn(),
    markdownPreviewMode: 'off',
    isMarkdownFile: false,
    localContent: '',
    markdownPreviewBodyRef: { current: null },
    ...overrides,
  };
}

let container: HTMLDivElement;
let root: ReturnType<typeof createRoot>;

beforeEach(() => {
  vi.clearAllMocks();
  container = document.createElement('div');
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

function renderComponent(props: EditorCoreProps) {
  root = createRoot(container);
  act(() => {
    root.render(createElement(EditorCore, props));
  });
}

// ── Tests ────────────────────────────────────────────────────────
describe('EditorCore', () => {
  // ── basic rendering ───
  describe('basic rendering', () => {
    it('renders editor div when not loading and no error', () => {
      renderComponent(createBaseProps());
      expect(container.querySelector('.editor')).toBeTruthy();
    });

    it('renders pane-content wrapper around editor', () => {
      renderComponent(createBaseProps());
      expect(container.querySelector('.pane-content')).toBeTruthy();
    });

    it('renders pane-content-wrapper', () => {
      renderComponent(createBaseProps());
      expect(container.querySelector('.pane-content-wrapper')).toBeTruthy();
    });
  });

  // ── loading state ───
  describe('loading state', () => {
    it('renders loading skeleton container when loading is true', () => {
      renderComponent(createBaseProps({ loading: true }));
      expect(container.querySelector('.editor-skeleton')).toBeTruthy();
    });

    it('does not render loading skeleton when loading is false', () => {
      renderComponent(createBaseProps({ loading: false }));
      expect(container.querySelector('.editor-skeleton')).toBeFalsy();
    });

    it('still renders editor div alongside skeleton (siblings)', () => {
      renderComponent(createBaseProps({ loading: true }));
      expect(container.querySelector('.editor-skeleton')).toBeTruthy();
      expect(container.querySelector('.editor')).toBeTruthy();
    });
  });

  // ── error state ───
  describe('error state', () => {
    it('renders error message element when error is set', () => {
      renderComponent(createBaseProps({ error: 'Oops' }));
      expect(container.querySelector('.error-message')).toBeTruthy();
    });

    it('does not render error message when error is null', () => {
      renderComponent(createBaseProps({ error: null }));
      expect(container.querySelector('.error-message')).toBeFalsy();
    });

    it('renders error text content', () => {
      renderComponent(createBaseProps({ error: 'Something went wrong' }));
      const errorText = container.querySelector('.error-text');
      expect(errorText).toBeTruthy();
      expect(errorText?.textContent).toBe('Something went wrong');
    });
  });

  // ── hook calls ───
  describe('hook calls', () => {
    it('calls useEditorViewInit once', () => {
      renderComponent(createBaseProps());
      expect(useEditorViewInit).toHaveBeenCalledTimes(1);
    });

    it('calls useEditorReconfigure once', () => {
      renderComponent(createBaseProps());
      expect(useEditorReconfigure).toHaveBeenCalledTimes(1);
    });

    it('passes editorRef to useEditorViewInit', () => {
      const props = createBaseProps();
      renderComponent(props);
      const callArgs = vi.mocked(useEditorViewInit).mock.calls[0][0];
      expect(callArgs.editorRef).toBe(props.editorRef);
    });

    it('passes viewRef to useEditorViewInit', () => {
      const props = createBaseProps();
      renderComponent(props);
      const callArgs = vi.mocked(useEditorViewInit).mock.calls[0][0];
      expect(callArgs.viewRef).toBe(props.viewRef);
    });

    it('passes viewRef to useEditorReconfigure', () => {
      const props = createBaseProps();
      renderComponent(props);
      const callArgs = vi.mocked(useEditorReconfigure).mock.calls[0][0];
      expect(callArgs.viewRef).toBe(props.viewRef);
    });

    it('shares lastInitLanguageKey ref between both hooks', () => {
      renderComponent(createBaseProps());
      const initArgs = vi.mocked(useEditorViewInit).mock.calls[0][0];
      const reconfigArgs = vi.mocked(useEditorReconfigure).mock.calls[0][0];
      expect(initArgs.lastInitLanguageKey).toBe(reconfigArgs.lastInitLanguageKey);
    });

    it('spreads initOptions into useEditorViewInit call', () => {
      const initOptions = { paneId: 'test-pane' };
      renderComponent(createBaseProps({ initOptions }));
      const callArgs = vi.mocked(useEditorViewInit).mock.calls[0][0];
      expect(callArgs.paneId).toBe('test-pane');
    });

    it('spreads reconfigureOptions into useEditorReconfigure call', () => {
      const reconfigureOptions = { hotkeys: [] };
      renderComponent(createBaseProps({ reconfigureOptions }));
      const callArgs = vi.mocked(useEditorReconfigure).mock.calls[0][0];
      expect(callArgs.hotkeys).toBe(reconfigureOptions.hotkeys);
    });
  });

  // ── markdown preview ───
  describe('markdown preview', () => {
    it('renders only editor in off mode for markdown files', () => {
      renderComponent(
        createBaseProps({
          isMarkdownFile: true,
          markdownPreviewMode: 'off',
        }),
      );
      expect(container.querySelector('.editor')).toBeTruthy();
      expect(container.querySelector('.pane-md-preview-split')).toBeFalsy();
    });

    it('renders split layout with editor and preview pane', () => {
      renderComponent(
        createBaseProps({
          isMarkdownFile: true,
          markdownPreviewMode: 'split',
          localContent: '# Hello',
        }),
      );
      expect(container.querySelector('.editor')).toBeTruthy();
      expect(container.querySelector('.pane-md-preview-split')).toBeTruthy();
    });

    it('applies md-split class to wrapper in split mode', () => {
      renderComponent(
        createBaseProps({
          isMarkdownFile: true,
          markdownPreviewMode: 'split',
        }),
      );
      expect(container.querySelector('.pane-content-wrapper-md-split')).toBeTruthy();
    });

    it('applies md-editor-side class to pane-content in split mode', () => {
      renderComponent(
        createBaseProps({
          isMarkdownFile: true,
          markdownPreviewMode: 'split',
        }),
      );
      expect(container.querySelector('.pane-content-md-editor-side')).toBeTruthy();
    });

    it('renders full preview mode for markdown files (no editor div)', () => {
      renderComponent(
        createBaseProps({
          isMarkdownFile: true,
          markdownPreviewMode: 'preview',
          localContent: '# Preview',
        }),
      );
      expect(container.querySelector('.editor')).toBeFalsy();
      expect(container.querySelector('.pane-content-md-preview-full')).toBeTruthy();
    });

    it('does NOT render preview when not a markdown file even with preview mode', () => {
      renderComponent(
        createBaseProps({
          isMarkdownFile: false,
          markdownPreviewMode: 'preview',
        }),
      );
      expect(container.querySelector('.editor')).toBeTruthy();
      expect(container.querySelector('.pane-content-md-preview-full')).toBeFalsy();
    });
  });

  // ── context menu ───
  describe('context menu handler', () => {
    it('attaches onContextMenu handler to pane-content div', () => {
      const handler = vi.fn();
      renderComponent(createBaseProps({ onContextMenu: handler }));
      const paneContent = container.querySelector('.pane-content')!;
      const mouseEvent = new MouseEvent('contextmenu', { bubbles: true });
      paneContent.dispatchEvent(mouseEvent);
      expect(handler).toHaveBeenCalledTimes(1);
    });
  });

  // ── editor ref ───
  describe('editor ref', () => {
    it('passes editorRef to the editor div', () => {
      const editorRef: RefObject<HTMLDivElement> = { current: null };
      renderComponent(createBaseProps({ editorRef }));
      expect(editorRef.current).toBeTruthy();
      expect(editorRef.current?.classList.contains('editor')).toBe(true);
    });
  });

  // ── comparator ───
  describe('areEditorCorePropsEqual', () => {
    // Shared refs so that prev/next can be "equal" when using same defaults
    const sharedEditorRef = { current: null } as RefObject<HTMLDivElement | null>;
    const sharedViewRef = { current: null } as MutableRefObject<CMEditorView | null>;
    const sharedMarkdownPreviewBodyRef = { current: null } as RefObject<HTMLDivElement | null>;
    const sharedOnContextMenu = vi.fn();
    const sharedInitOptions = {};
    const sharedReconfigureOptions = {};

    function makeProps(overrides: Partial<EditorCoreProps> = {}): EditorCoreProps {
      return {
        editorRef: sharedEditorRef,
        viewRef: sharedViewRef,
        initOptions: sharedInitOptions,
        reconfigureOptions: sharedReconfigureOptions,
        loading: false,
        error: null,
        onContextMenu: sharedOnContextMenu,
        markdownPreviewMode: 'off',
        isMarkdownFile: false,
        localContent: '',
        markdownPreviewBodyRef: sharedMarkdownPreviewBodyRef,
        ...overrides,
      };
    }

    describe('returns true when props are equivalent', () => {
      it('identical props objects', () => {
        const props = makeProps();
        expect(areEditorCorePropsEqual(props, props)).toBe(true);
      });

      it('two calls with same shared refs return true', () => {
        const prev = makeProps();
        const next = makeProps();
        expect(areEditorCorePropsEqual(prev, next)).toBe(true);
      });

      it('same function reference for onContextMenu (explicit override)', () => {
        const fn = vi.fn();
        const prev = makeProps({ onContextMenu: fn });
        const next = makeProps({ onContextMenu: fn });
        expect(areEditorCorePropsEqual(prev, next)).toBe(true);
      });

      it('same ref object references (explicit override)', () => {
        const editorRef = { current: null };
        const viewRef = { current: null };
        const markdownPreviewBodyRef = { current: null };
        const prev = makeProps({ editorRef, viewRef, markdownPreviewBodyRef });
        const next = makeProps({ editorRef, viewRef, markdownPreviewBodyRef });
        expect(areEditorCorePropsEqual(prev, next)).toBe(true);
      });

      it('same initOptions and reconfigureOptions references (explicit override)', () => {
        const initOptions = { paneId: 'test' };
        const reconfigureOptions = { hotkeys: [] };
        const prev = makeProps({ initOptions, reconfigureOptions });
        const next = makeProps({ initOptions, reconfigureOptions });
        expect(areEditorCorePropsEqual(prev, next)).toBe(true);
      });
    });

    describe('returns false when relevant props differ', () => {
      it('different editorRef', () => {
        const prev = makeProps({ editorRef: { current: null } });
        const next = makeProps({ editorRef: { current: null } });
        expect(areEditorCorePropsEqual(prev, next)).toBe(false);
      });

      it('different viewRef', () => {
        const prev = makeProps({ viewRef: { current: null } });
        const next = makeProps({ viewRef: { current: null } });
        expect(areEditorCorePropsEqual(prev, next)).toBe(false);
      });

      it('different markdownPreviewBodyRef', () => {
        const prev = makeProps({ markdownPreviewBodyRef: { current: null } });
        const next = makeProps({ markdownPreviewBodyRef: { current: null } });
        expect(areEditorCorePropsEqual(prev, next)).toBe(false);
      });

      it('different loading', () => {
        const prev = makeProps({ loading: false });
        const next = makeProps({ loading: true });
        expect(areEditorCorePropsEqual(prev, next)).toBe(false);
      });

      it('different error', () => {
        const prev = makeProps({ error: null });
        const next = makeProps({ error: 'Oops' });
        expect(areEditorCorePropsEqual(prev, next)).toBe(false);
      });

      it('different onContextMenu function', () => {
        const fn1 = vi.fn();
        const fn2 = vi.fn();
        const prev = makeProps({ onContextMenu: fn1 });
        const next = makeProps({ onContextMenu: fn2 });
        expect(areEditorCorePropsEqual(prev, next)).toBe(false);
      });

      it('different markdownPreviewMode', () => {
        const prev = makeProps({ markdownPreviewMode: 'off' });
        const next = makeProps({ markdownPreviewMode: 'split' });
        expect(areEditorCorePropsEqual(prev, next)).toBe(false);
      });

      it('different isMarkdownFile', () => {
        const prev = makeProps({ isMarkdownFile: false });
        const next = makeProps({ isMarkdownFile: true });
        expect(areEditorCorePropsEqual(prev, next)).toBe(false);
      });

      it('different localContent', () => {
        const prev = makeProps({ localContent: '' });
        const next = makeProps({ localContent: 'hello' });
        expect(areEditorCorePropsEqual(prev, next)).toBe(false);
      });

      it('different initOptions reference', () => {
        const prev = makeProps({ initOptions: { paneId: 'a' } });
        const next = makeProps({ initOptions: { paneId: 'a' } });
        // Same values but different object references — should be false
        expect(areEditorCorePropsEqual(prev, next)).toBe(false);
      });

      it('different reconfigureOptions reference', () => {
        const prev = makeProps({ reconfigureOptions: { hotkeys: [] } });
        const next = makeProps({ reconfigureOptions: { hotkeys: [] } });
        // Same values but different object references — should be false
        expect(areEditorCorePropsEqual(prev, next)).toBe(false);
      });
    });
  });
});
