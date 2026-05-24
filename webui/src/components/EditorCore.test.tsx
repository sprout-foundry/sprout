import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import React from 'react';

// ── Mocks (hoisted by vitest) ───────────────────────────────────────────────
vi.mock('../hooks/useEditorViewInit', () => ({
  useEditorViewInit: vi.fn(),
}));

vi.mock('./useEditorReconfigure', () => ({
  useEditorReconfigure: vi.fn(),
}));

vi.mock('./MarkdownPreview', () => ({
  __esModule: true,
  // Return a plain string to avoid React version mismatch from JSX in mock factory.
  // Strings are valid React children and can be queried with getByText().
  default: () => 'markdown-preview-mock',
}));

// ── Import mocked modules ──────────────────────────────────────────────────
import { useEditorViewInit } from '../hooks/useEditorViewInit';
import { useEditorReconfigure } from './useEditorReconfigure';
import EditorCore from './EditorCore';

// ── Helpers ─────────────────────────────────────────────────────────────────
const flushPromises = () => new Promise((resolve) => setTimeout(resolve, 0));

function createBaseProps() {
  return {
    hookOptions: {
      ref: { current: null },
      initialContent: '',
      initialMode: 'text/plain',
    },
    hookReconfigureOptions: {
      ref: { current: null },
      content: '',
      mode: 'text/plain',
    },
  };
}

// ── Tests ───────────────────────────────────────────────────────────────────
describe('EditorCore', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // ── Basic rendering ───────────────────────────────────────────────────
  describe('basic rendering', () => {
    it('renders editor div when not loading and no error', () => {
      const { container } = render(<EditorCore {...createBaseProps()} />);

      const editor = container.querySelector('.editor');
      expect(editor).toBeInTheDocument();
    });

    it('renders pane-content wrapper around editor', () => {
      const { container } = render(<EditorCore {...createBaseProps()} />);

      const paneContent = container.querySelector('.pane-content');
      expect(paneContent).toBeInTheDocument();
      expect(paneContent?.querySelector('.editor')).toBeInTheDocument();
    });
  });

  // ── Loading state ─────────────────────────────────────────────────────
  describe('loading state', () => {
    it('renders loading skeleton when loading is true', () => {
      const { container } = render(
        <EditorCore {...createBaseProps()} loading />
      );

      const skeleton = container.querySelector('.editor-skeleton');
      expect(skeleton).toBeInTheDocument();
    });

    it('renders three skeleton lines inside the skeleton container', () => {
      const { container } = render(
        <EditorCore {...createBaseProps()} loading />
      );

      const lines = container.querySelectorAll('.editor-skeleton-line');
      expect(lines).toHaveLength(3);
    });

    it('does not render editor div when loading', () => {
      const { container } = render(
        <EditorCore {...createBaseProps()} loading />
      );

      expect(container.querySelector('.editor')).not.toBeInTheDocument();
    });

    it('does not render pane-content when loading', () => {
      const { container } = render(
        <EditorCore {...createBaseProps()} loading />
      );

      expect(container.querySelector('.pane-content')).not.toBeInTheDocument();
    });
  });

  // ── Error state ───────────────────────────────────────────────────────
  describe('error state', () => {
    it('renders error message element when error is set', () => {
      const { container } = render(
        <EditorCore {...createBaseProps()} error="Something went wrong" />
      );

      const errorEl = container.querySelector('.error-message');
      expect(errorEl).toBeInTheDocument();
    });

    it('displays the error message text', () => {
      const { container } = render(
        <EditorCore {...createBaseProps()} error="File not found" />
      );

      const errorEl = container.querySelector('.error-message');
      expect(errorEl).toHaveTextContent('File not found');
    });

    it('does not render editor div when error is set', () => {
      const { container } = render(
        <EditorCore {...createBaseProps()} error="Error" />
      );

      expect(container.querySelector('.editor')).not.toBeInTheDocument();
    });

    it('does not render pane-content when error is set', () => {
      const { container } = render(
        <EditorCore {...createBaseProps()} error="Error" />
      );

      expect(container.querySelector('.pane-content')).not.toBeInTheDocument();
    });

    it('loading takes precedence over error', () => {
      const { container } = render(
        <EditorCore
          {...createBaseProps()}
          loading
          error="This should not show"
        />
      );

      expect(container.querySelector('.editor-skeleton')).toBeInTheDocument();
      expect(container.querySelector('.error-message')).not.toBeInTheDocument();
    });
  });

  // ── Hook calls ────────────────────────────────────────────────────────
  describe('hook calls', () => {
    it('calls useEditorViewInit once on mount', () => {
      render(<EditorCore {...createBaseProps()} />);

      expect(useEditorViewInit).toHaveBeenCalledTimes(1);
    });

    it('calls useEditorReconfigure once on mount', () => {
      render(<EditorCore {...createBaseProps()} />);

      expect(useEditorReconfigure).toHaveBeenCalledTimes(1);
    });

    it('calls useEditorViewInit with merged options including internal ref', () => {
      const props = createBaseProps();
      render(<EditorCore {...props} />);

      expect(useEditorViewInit).toHaveBeenCalledWith(
        expect.objectContaining({
          initialContent: '',
          initialMode: 'text/plain',
        })
      );

      const callArgs = (useEditorViewInit as vi.Mock).mock.calls[0][0];
      expect(callArgs.ref).toBeDefined();
      // The ref passed to the hook should be the internal editorRef, not the one from props
      expect(callArgs.ref).not.toBe(props.hookOptions.ref);
    });

    it('calls useEditorReconfigure with merged options including internal ref', () => {
      const props = createBaseProps();
      render(<EditorCore {...props} />);

      expect(useEditorReconfigure).toHaveBeenCalledWith(
        expect.objectContaining({
          content: '',
          mode: 'text/plain',
        })
      );

      const callArgs = (useEditorReconfigure as vi.Mock).mock.calls[0][0];
      expect(callArgs.ref).toBeDefined();
      // The ref passed to the hook should be the internal editorRef, not the one from props
      expect(callArgs.ref).not.toBe(props.hookReconfigureOptions.ref);
    });

    it('passes the same internal ref to both hooks', () => {
      render(<EditorCore {...createBaseProps()} />);

      const initRef = (useEditorViewInit as vi.Mock).mock.calls[0][0].ref;
      const reconfigureRef = (useEditorReconfigure as vi.Mock).mock.calls[0][0].ref;
      expect(initRef).toBe(reconfigureRef);
    });

    it('preserves additional hook options when merging', () => {
      const onMount = vi.fn();
      const onSave = vi.fn();
      const props = {
        ...createBaseProps(),
        hookOptions: {
          ...createBaseProps().hookOptions,
          initialContent: 'hello',
          onMount,
        },
        hookReconfigureOptions: {
          ...createBaseProps().hookReconfigureOptions,
          content: 'hello',
          readOnly: true,
          onSave,
        },
      };

      render(<EditorCore {...props} />);

      const initCall = (useEditorViewInit as vi.Mock).mock.calls[0][0];
      expect(initCall.initialContent).toBe('hello');
      expect(initCall.onMount).toBe(onMount);

      const reconfigureCall = (useEditorReconfigure as vi.Mock).mock.calls[0][0];
      expect(reconfigureCall.content).toBe('hello');
      expect(reconfigureCall.readOnly).toBe(true);
      expect(reconfigureCall.onSave).toBe(onSave);
    });
  });

  // ── Markdown preview modes ────────────────────────────────────────────
  describe('markdown preview modes', () => {
    it('renders MarkdownPreview in full preview mode', () => {
      render(
        <EditorCore
          {...createBaseProps()}
          isMarkdownFile
          markdownPreviewMode="preview"
          markdownContent="# Hello"
        />
      );

      expect(screen.getByText('markdown-preview-mock')).toBeInTheDocument();
    });

    it('does not render editor div in full preview mode', () => {
      const { container } = render(
        <EditorCore
          {...createBaseProps()}
          isMarkdownFile
          markdownPreviewMode="preview"
        />
      );

      expect(container.querySelector('.editor')).not.toBeInTheDocument();
    });

    it('wraps preview in markdown-preview-container', () => {
      const { container } = render(
        <EditorCore
          {...createBaseProps()}
          isMarkdownFile
          markdownPreviewMode="preview"
        />
      );

      expect(container.querySelector('.markdown-preview-container')).toBeInTheDocument();
    });

    it('renders split layout with both editor and preview', () => {
      const { container } = render(
        <EditorCore
          {...createBaseProps()}
          isMarkdownFile
          markdownPreviewMode="split"
          markdownContent="# Hello"
        />
      );

      expect(container.querySelector('.editor')).toBeInTheDocument();
      expect(container.querySelector('.split-preview')).toBeInTheDocument();
      expect(screen.getByText('markdown-preview-mock')).toBeInTheDocument();
    });

    it('renders editor only when markdownPreviewMode is none', () => {
      const { container } = render(
        <EditorCore
          {...createBaseProps()}
          isMarkdownFile
          markdownPreviewMode="none"
        />
      );

      expect(container.querySelector('.editor')).toBeInTheDocument();
      expect(screen.queryByText('markdown-preview-mock')).not.toBeInTheDocument();
    });

    it('does not render preview when not a markdown file even with preview mode', () => {
      const { container } = render(
        <EditorCore
          {...createBaseProps()}
          isMarkdownFile={false}
          markdownPreviewMode="preview"
        />
      );

      expect(container.querySelector('.editor')).toBeInTheDocument();
      expect(screen.queryByText('markdown-preview-mock')).not.toBeInTheDocument();
    });

    it('does not render preview when not a markdown file even with split mode', () => {
      const { container } = render(
        <EditorCore
          {...createBaseProps()}
          isMarkdownFile={false}
          markdownPreviewMode="split"
        />
      );

      expect(container.querySelector('.editor')).toBeInTheDocument();
      expect(screen.queryByText('markdown-preview-mock')).not.toBeInTheDocument();
      expect(container.querySelector('.split-preview')).not.toBeInTheDocument();
    });
  });

  // ── Context menu handler ──────────────────────────────────────────────
  describe('context menu handler', () => {
    it('passes onContextMenu handler to pane-content div', () => {
      const handler = vi.fn();
      const { container } = render(
        <EditorCore
          {...createBaseProps()}
          onMarkdownContextMenu={handler}
        />
      );

      const paneContent = container.querySelector('.pane-content');
      fireEvent.contextMenu(paneContent!);

      expect(handler).toHaveBeenCalledTimes(1);
    });

    it('does not throw when onContextMenu is not provided', () => {
      const { container } = render(<EditorCore {...createBaseProps()} />);

      const paneContent = container.querySelector('.pane-content');
      // Should not throw even without a handler
      expect(() => {
        fireEvent.contextMenu(paneContent!);
      }).not.toThrow();
    });
  });

  // ── Editor ref forwarding ─────────────────────────────────────────────
  describe('editor ref forwarding', () => {
    it('attaches ref to the editor div element', async () => {
      const { container } = render(<EditorCore {...createBaseProps()} />);
      await flushPromises();

      const editor = container.querySelector('.editor');
      expect(editor).toBeInTheDocument();
      expect(editor?.tagName).toBe('DIV');
    });

    it('forwards external ref to the editor div via forwardRef', () => {
      const externalRef = React.createRef<HTMLDivElement>();

      render(
        <EditorCore {...createBaseProps()} ref={externalRef} />
      );

      expect(externalRef.current).toBeDefined();
      expect(externalRef.current?.className).toBe('editor');
    });

    it('forwards external ref in split preview mode', () => {
      const externalRef = React.createRef<HTMLDivElement>();

      render(
        <EditorCore
          {...createBaseProps()}
          ref={externalRef}
          isMarkdownFile
          markdownPreviewMode="split"
        />
      );

      expect(externalRef.current).toBeDefined();
      expect(externalRef.current?.className).toBe('editor');
    });
  });

  // ── Default values ────────────────────────────────────────────────────
  describe('default values', () => {
    it('defaults loading to false', () => {
      const { container } = render(<EditorCore {...createBaseProps()} />);

      expect(container.querySelector('.editor')).toBeInTheDocument();
      expect(container.querySelector('.editor-skeleton')).not.toBeInTheDocument();
    });

    it('defaults markdownPreviewMode to none', () => {
      const { container } = render(
        <EditorCore {...createBaseProps()} isMarkdownFile />
      );

      expect(container.querySelector('.editor')).toBeInTheDocument();
      expect(screen.queryByText('markdown-preview-mock')).not.toBeInTheDocument();
    });

    it('defaults isMarkdownFile to false', () => {
      const { container } = render(
        <EditorCore {...createBaseProps()} markdownPreviewMode="preview" />
      );

      expect(container.querySelector('.editor')).toBeInTheDocument();
      expect(screen.queryByText('markdown-preview-mock')).not.toBeInTheDocument();
    });
  });
});
