/**
 * EditorPaneFooter — renders editor footer with stats and language switcher.
 *
 * Extracted from EditorPane footer render section.
 *
 * Target: ~100 lines
 */

import type { FC, KeyboardEvent } from 'react';

import LanguageSwitcher from './LanguageSwitcher';
import type { EditorBuffer } from '../types/editor';
import type { WhitespaceRenderingMode } from '../extensions/whitespaceRendering';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface EditorPaneFooterProps {
  buffer: EditorBuffer | null | undefined;
  selectionInfo: { selectionCount?: number; charCount?: number } | null;
  settings: {
    editorFontSize: number;
    editorTabSize: number;
    editorUsesTabs: boolean;
    lineEnding: string;
    whitespaceRenderingModeRef: { current: WhitespaceRenderingMode };
    onCycleTabSize: () => void;
    onCycleWhitespaceRendering: () => WhitespaceRenderingMode;
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
 * Component that renders the editor footer with file stats and language switcher.
 */
export const EditorPaneFooter: FC<EditorPaneFooterProps> = ({
  buffer,
  selectionInfo,
  settings,
  lsp,
  setWhitespaceRenderingMode,
}) => {
  const handleKeyDown = (
    e: KeyboardEvent<HTMLSpanElement>,
    callback: () => void,
  ) => {
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

  return (
    <div className="pane-footer">
      <div className="editor-stats">
        <span className="line-count">Lines: {(buffer?.content || '').split('\n').length}</span>
        <span className="char-count">Chars: {(buffer?.content || '').length}</span>
        <span className="cursor-position">
          Ln {buffer?.cursorPosition?.line !== undefined ? buffer.cursorPosition.line + 1 : 0}, Col {buffer?.cursorPosition?.column !== undefined ? buffer.cursorPosition.column + 1 : 0}
          {selectionInfo && selectionInfo.selectionCount !== undefined && selectionInfo.selectionCount > 1 && ` (${selectionInfo.selectionCount} selections)`}
          {selectionInfo && selectionInfo.selectionCount !== undefined && selectionInfo.selectionCount === 1 && ` (${selectionInfo.charCount ?? 0} selected)`}
        </span>
        {settings.editorFontSize !== 13 && (
          <span className="zoom-level">
            Zoom: {Math.round((settings.editorFontSize / 13) * 100)}%
          </span>
        )}
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
        <span className="encoding-indicator" title="File encoding and line endings">
          UTF-8 · {settings.lineEnding}
        </span>
        {settings.whitespaceRenderingModeRef.current !== 'none' && (
          <span
            className="whitespace-mode"
            role="button"
            tabIndex={0}
            onClick={handleWhitespaceToggle}
            onKeyDown={(e) => handleKeyDown(e, handleWhitespaceToggle)}
            title="Click to change whitespace rendering (none → boundary → all)"
          >
            {settings.whitespaceRenderingModeRef.current === 'boundary' ? 'WS: boundary' : 'WS: all'}
          </span>
        )}
        {lsp.lspLanguage && (
          <span
            className="cm-footer-lsp"
            title={`LSP: ${lsp.lspState}`}
            style={{
              color:
                lsp.lspState === 'connected'
                  ? 'var(--cm-status-ok, #4caf50)'
                  : lsp.lspState === 'disconnected'
                    ? 'var(--cm-status-error, #666)'
                    : 'var(--cm-status-warning, #c90)',
            }}
          >
            LSP:{lsp.lspState === 'connected' ? '✓' : lsp.lspState === 'connecting' || lsp.lspState === 'reconnecting' ? '…' : '✗'}
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

export default EditorPaneFooter;
