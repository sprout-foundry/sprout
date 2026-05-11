/**
 * useEditorToolbarActions — builds toolbar right actions array.
 *
 * Extracts toolbar right actions logic from EditorPane:
 * - livePreview and livePreviewSplit actions for SVG/HTML files
 * - markdown preview toggle actions for .md files
 * - relative line numbers toggle
 *
 * Target: ~100 lines
 */

import { useMemo, type ReactNode } from 'react';
import { Eye, Columns2, ListOrdered, Paintbrush, SaveAll } from 'lucide-react';

import type { ToolbarAction } from './EditorToolbarActions';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface UseEditorToolbarActionsOptions {
  isSvgFile: boolean;
  isHtmlFile: boolean;
  isMarkdownFile: boolean;
  markdownPreviewMode: 'off' | 'split' | 'preview';
  relativeLineNumbersEnabled: boolean;
  setMarkdownPreviewMode: (mode: 'off' | 'split' | 'preview') => void;
  onToggleRelativeLineNumbers: () => void;
  onOpenLivePreview?: () => void;
  onOpenLivePreviewInSplit?: () => void;
  onFormatDocument?: () => void;
  formatOnSaveEnabled?: boolean;
  onToggleFormatOnSave?: () => void;
}

export interface UseEditorToolbarActionsReturn {
  rightActions: ToolbarAction[];
}

/**
 * Hook that builds toolbar right actions array.
 *
 * @param options - Configuration options
 * @returns Toolbar actions array
 */
export function useEditorToolbarActions(options: UseEditorToolbarActionsOptions): UseEditorToolbarActionsReturn {
  const {
    isSvgFile,
    isHtmlFile,
    isMarkdownFile,
    markdownPreviewMode,
    relativeLineNumbersEnabled,
    setMarkdownPreviewMode,
    onToggleRelativeLineNumbers,
    onOpenLivePreview,
    onOpenLivePreviewInSplit,
    onFormatDocument,
    formatOnSaveEnabled,
    onToggleFormatOnSave,
  } = options;

  const rightActions = useMemo<ToolbarAction[]>(() => {
    const actions: ToolbarAction[] = [];

    // Format document action
    if (onFormatDocument) {
      actions.push({
        id: 'format-document',
        title: 'Format document (Shift+Alt+F)',
        icon: <Paintbrush size={16} />,
        onClick: onFormatDocument,
      });
    }

    // Format on save toggle
    if (onToggleFormatOnSave && formatOnSaveEnabled !== undefined) {
      actions.push({
        id: 'format-on-save',
        title: formatOnSaveEnabled ? 'Disable format on save' : 'Enable format on save',
        icon: <SaveAll size={16} />,
        onClick: onToggleFormatOnSave,
        active: formatOnSaveEnabled,
      });
    }

    // Live preview actions for SVG/HTML files
    if (isSvgFile || isHtmlFile) {
      if (onOpenLivePreview) {
        actions.push({
          id: 'live-preview',
          title: isSvgFile ? 'Open SVG live preview' : 'Open HTML live preview',
          icon: <Eye size={16} />,
          onClick: onOpenLivePreview,
        });
      }
      if (onOpenLivePreviewInSplit) {
        actions.push({
          id: 'live-preview-split',
          title: isSvgFile ? 'Open SVG live preview in split' : 'Open HTML live preview in split',
          icon: <Columns2 size={16} />,
          onClick: onOpenLivePreviewInSplit,
        });
      }
    }

    // Markdown preview actions
    if (isMarkdownFile) {
      actions.push({
        id: 'md-toggle',
        title: markdownPreviewMode === 'off' ? 'Toggle markdown preview' : 'Close markdown preview',
        icon: <Eye size={16} />,
        onClick: () =>
          setMarkdownPreviewMode(
            markdownPreviewMode === 'off' ? 'split' : markdownPreviewMode === 'split' ? 'preview' : 'off',
          ),
        active: markdownPreviewMode !== 'off',
      });

      if (markdownPreviewMode !== 'off') {
        actions.push({
          id: 'md-split',
          title: 'Side-by-side view',
          icon: <Columns2 size={16} />,
          onClick: () => setMarkdownPreviewMode('split'),
          active: markdownPreviewMode === 'split',
        });
      }
    }

    // Relative line numbers toggle
    actions.push({
      id: 'relative-line-numbers',
      title: 'Toggle relative line numbers',
      icon: <ListOrdered size={16} />,
      onClick: onToggleRelativeLineNumbers,
      active: relativeLineNumbersEnabled,
    });

    return actions;
  }, [
    isSvgFile,
    isHtmlFile,
    isMarkdownFile,
    markdownPreviewMode,
    relativeLineNumbersEnabled,
    setMarkdownPreviewMode,
    onToggleRelativeLineNumbers,
    onOpenLivePreview,
    onOpenLivePreviewInSplit,
    onFormatDocument,
    formatOnSaveEnabled,
    onToggleFormatOnSave,
  ]);

  return { rightActions };
}
