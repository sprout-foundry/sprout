/**
 * EditorPaneFooter — renders editor footer with stats and language switcher.
 *
 * Extracted from EditorPane footer render section.
 *
 * Target: ~100 lines
 */

import { AlertCircle, Check, Loader2 } from 'lucide-react';
import React, { useMemo, type FC, type KeyboardEvent } from 'react';
import type { WhitespaceRenderingMode } from '../extensions/whitespaceRendering';
import type { EditorBuffer } from '../types/editor';
import LanguageSwitcher from './LanguageSwitcher';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const FONT_SIZE_DEFAULT = 14;

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface EditorPaneFooterProps {
  buffer: EditorBuffer | null | undefined;
  selectionInfo: { selectionCount?: number; charCount?: number } | null;
  whitespaceRenderingMode: WhitespaceRenderingMode;
  settings: {
    editorFontSize: number;
    editorTabSize: number;
    editorUsesTabs: boolean;
    lineEnding: string;
    onCycleTabSize: () => void;
    onCycleWhitespaceRendering: () => WhitespaceRenderingMode;
    onZoomIn: () => void;
    onZoomOut: () => void;
    onResetZoom: () => void;
  };
  lsp: {
    lspLanguage: string | null;
    lspState: string;
    languageInfo: {
      languageId: string | null;
      isAutoDetected: boolean;
    };
    handleLanguageChange: (languageId: string | null) => void;
  };
  setWhitespaceRenderingMode: (mode: WhitespaceRenderingMode) => void;
}

/**
 * Shallow equality helper for EditorPaneFooter props.
 * Compares primitive props directly and does shallow comparison on the
 * `settings` and `lsp` objects (including their function properties) so that
 * React.memo skips re-renders when the parent recreates these wrapper objects
 * with the same underlying values/functions.
 */
export function areEditorPaneFooterPropsEqual(
  prev: EditorPaneFooterProps,
  next: EditorPaneFooterProps,
): boolean {
  if (prev.buffer !== next.buffer) return false;
  if (prev.selectionInfo !== next.selectionInfo) return false;
  if (prev.whitespaceRenderingMode !== next.whitespaceRenderingMode) return false;
  if (prev.setWhitespaceRenderingMode !== next.setWhitespaceRenderingMode) return false;

  // Shallow compare settings object properties
  const ps = prev.settings;
  const ns = next.settings;
  if (
    ps.editorFontSize !== ns.editorFontSize ||
    ps.editorTabSize !== ns.editorTabSize ||
    ps.editorUsesTabs !== ns.editorUsesTabs ||
    ps.lineEnding !== ns.lineEnding ||
    ps.onCycleTabSize !== ns.onCycleTabSize ||
    ps.onCycleWhitespaceRendering !== ns.onCycleWhitespaceRendering ||
    ps.onZoomIn !== ns.onZoomIn ||
    ps.onZoomOut !== ns.onZoomOut ||
    ps.onResetZoom !== ns.onResetZoom
  ) {
    return false;
  }

  // Shallow compare lsp object properties (languageInfo is nested one level)
  const pl = prev.lsp;
  const nl = next.lsp;
  if (
    pl.lspLanguage !== nl.lspLanguage ||
    pl.lspState !== nl.lspState ||
    pl.handleLanguageChange !== nl.handleLanguageChange ||
    pl.languageInfo?.languageId !== nl.languageInfo?.languageId ||
    pl.languageInfo?.isAutoDetected !== nl.languageInfo?.isAutoDetected
  ) {
    return false;
  }

  return true;
}

/**
 * Component that renders the editor footer with file stats and language switcher.
 */
const EditorPaneFooterImpl: FC<EditorPaneFooterProps> = ({
  buffer,
  selectionInfo,
  whitespaceRenderingMode,
  settings,
  lsp,
  setWhitespaceRenderingMode,
}) => {
  const handleKeyDown = (e: KeyboardEvent<HTMLSpanElement>, callback: () => void) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      callback();
    }
  };

  const handleWhitespaceToggle = () => {
    const next = settings.onCycleWhitespaceRendering();
    if (next) {
      setWhitespaceRenderingMode(next);
    }
  };

  const handleResetZoom = () => {
    settings.onResetZoom();
  };

  // Memoize line/char counts so we don't re-split a 50k-line buffer on every
  // cursor movement, focus event, or unrelated parent re-render.
  const content = buffer?.content || '';
  const { lineCount, charCount } = useMemo(() => {
    let lines = 1;
    for (let i = 0; i < content.length; i++) {
      if (content.charCodeAt(i) === 10) lines++;
    }
    return { lineCount: content.length === 0 ? 0 : lines, charCount: content.length };
  }, [content]);

  return (
    <div className="pane-footer">
      <div className="editor-stats">
        <span className="line-count">Lines: {lineCount}</span>
        <span className="char-count">Chars: {charCount}</span>
        <span className="cursor-position">
          Ln {buffer?.cursorPosition?.line !== undefined ? buffer.cursorPosition.line : 0}, Col{' '}
          {buffer?.cursorPosition?.column !== undefined ? buffer.cursorPosition.column + 1 : 0}
          {selectionInfo &&
            selectionInfo.selectionCount !== undefined &&
            selectionInfo.selectionCount > 1 &&
            ` (${selectionInfo.selectionCount} selections)`}
          {selectionInfo &&
            selectionInfo.selectionCount !== undefined &&
            selectionInfo.selectionCount === 1 &&
            ` (${selectionInfo.charCount ?? 0} selected)`}
        </span>
        <span
          className="zoom-control"
          role="button"
          tabIndex={0}
          onClick={settings.onZoomOut}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault();
              settings.onZoomOut();
            }
          }}
          title="Zoom out (decrease font size)"
        >
          −
        </span>
        <span
          className="zoom-level"
          role="button"
          tabIndex={0}
          onClick={handleResetZoom}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault();
              handleResetZoom();
            }
          }}
          title="Reset zoom to default"
        >
          {Math.round((settings.editorFontSize / FONT_SIZE_DEFAULT) * 100)}%
        </span>
        <span
          className="zoom-control"
          role="button"
          tabIndex={0}
          onClick={settings.onZoomIn}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault();
              settings.onZoomIn();
            }
          }}
          title="Zoom in (increase font size)"
        >
          +
        </span>
        <span
          className="tab-size"
          role="button"
          tabIndex={0}
          onClick={settings.onCycleTabSize}
          onKeyDown={(e) => handleKeyDown(e, settings.onCycleTabSize)}
          title="Click to change tab size (Spaces: 2, 4, 8 / Tabs)"
        >
          {settings.editorUsesTabs ? 'Tabs' : `Spaces: ${settings.editorTabSize}`}
        </span>
        <span className="encoding-indicator" title="Line ending convention used when saving">
          {settings.lineEnding}
        </span>
        <span
          className="whitespace-mode"
          role="button"
          tabIndex={0}
          onClick={handleWhitespaceToggle}
          onKeyDown={(e) => handleKeyDown(e, handleWhitespaceToggle)}
          title="Click to change whitespace rendering (none → boundary → all)"
        >
          WS: {whitespaceRenderingMode === 'none' ? 'off' : whitespaceRenderingMode}
        </span>
        {lsp.lspLanguage && (
          <span className={`cm-footer-lsp cm-footer-lsp--${lsp.lspState}`} title={`LSP: ${lsp.lspState}`}>
            <span>LSP</span>
            {lsp.lspState === 'connected' ? (
              <Check size={11} aria-hidden="true" />
            ) : lsp.lspState === 'connecting' || lsp.lspState === 'reconnecting' ? (
              <Loader2 size={11} aria-hidden="true" className="cm-footer-lsp-spin" />
            ) : (
              <AlertCircle size={11} aria-hidden="true" />
            )}
          </span>
        )}
      </div>
      <LanguageSwitcher
        currentLanguageId={lsp.languageInfo.languageId}
        isAutoDetected={lsp.languageInfo.isAutoDetected}
        onLanguageChange={lsp.handleLanguageChange}
      />
    </div>
  );
};

export const EditorPaneFooter = React.memo(EditorPaneFooterImpl, areEditorPaneFooterPropsEqual);

export default EditorPaneFooter;
