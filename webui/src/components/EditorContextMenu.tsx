/**
 * EditorContextMenu — renders the editor context menu.
 *
 * Extracted from EditorPane context menu render section.
 *
 * Target: ~80 lines
 */

import React from 'react';
import { Copy, Navigation, Eye, FolderOpen, ClipboardCopy } from 'lucide-react';
import type { FC } from 'react';
import { ContextMenu } from '@sprout/ui';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface EditorContextMenuProps {
  contextMenu: {
    contextMenu: { x: number; y: number; hasSelection: boolean; languageId?: string } | null;
    workspaceRoot: string | null;
    hideContextMenu: () => void;
    handleCopySelection: () => void;
    handleGoToDefinitionFromMenu: () => void;
    handleFindAllReferencesFromMenu: () => void;
    handleRevealInExplorer: () => void;
    handleCopyRelativePath: () => void;
    handleCopyAbsolutePath: () => void;
  };
  isSemanticLanguage: (languageId: string) => boolean;
}

/**
 * Custom equality check for EditorContextMenu.
 * The `contextMenu` prop is a bag-of-functions object recreated by the parent on
 * every render, so we must compare each property individually.
 */
export function areContextMenuEqual(prev: EditorContextMenuProps, next: EditorContextMenuProps): boolean {
  const pc = prev.contextMenu;
  const nc = next.contextMenu;

  if (pc.contextMenu !== nc.contextMenu) return false;
  if (pc.workspaceRoot !== nc.workspaceRoot) return false;
  if (pc.hideContextMenu !== nc.hideContextMenu) return false;
  if (pc.handleCopySelection !== nc.handleCopySelection) return false;
  if (pc.handleGoToDefinitionFromMenu !== nc.handleGoToDefinitionFromMenu) return false;
  if (pc.handleFindAllReferencesFromMenu !== nc.handleFindAllReferencesFromMenu) return false;
  if (pc.handleRevealInExplorer !== nc.handleRevealInExplorer) return false;
  if (pc.handleCopyRelativePath !== nc.handleCopyRelativePath) return false;
  if (pc.handleCopyAbsolutePath !== nc.handleCopyAbsolutePath) return false;

  if (prev.isSemanticLanguage !== next.isSemanticLanguage) return false;

  return true;
}

/**
 * Component that renders the editor context menu.
 */
const EditorContextMenuImpl: FC<EditorContextMenuProps> = ({ contextMenu: ctx, isSemanticLanguage }) => {
  const { contextMenu, workspaceRoot } = ctx;

  return (
    <ContextMenu
      isOpen={contextMenu !== null}
      x={contextMenu?.x ?? 0}
      y={contextMenu?.y ?? 0}
      onClose={ctx.hideContextMenu}
    >
      {contextMenu?.hasSelection && (
        <button className="context-menu-item" onClick={ctx.handleCopySelection} type="button">
          <Copy size={13} />
          <span className="menu-item-label">Copy</span>
        </button>
      )}
      {contextMenu?.languageId && isSemanticLanguage(contextMenu.languageId) && (
        <button className="context-menu-item" onClick={ctx.handleGoToDefinitionFromMenu} type="button">
          <Navigation size={13} />
          <span className="menu-item-label">Go to Definition</span>
        </button>
      )}
      {contextMenu?.languageId && isSemanticLanguage(contextMenu.languageId) && (
        <button className="context-menu-item" onClick={ctx.handleFindAllReferencesFromMenu} type="button">
          <Eye size={13} />
          <span className="menu-item-label">Find All References</span>
        </button>
      )}
      {(contextMenu?.hasSelection || (contextMenu?.languageId && isSemanticLanguage(contextMenu.languageId))) && (
        <div className="context-menu-divider" />
      )}
      <button className="context-menu-item" onClick={ctx.handleRevealInExplorer} type="button">
        <FolderOpen size={13} />
        <span className="menu-item-label">Reveal in Explorer</span>
      </button>
      <button className="context-menu-item" onClick={ctx.handleCopyRelativePath} type="button">
        <ClipboardCopy size={13} />
        <span className="menu-item-label">Copy relative path</span>
      </button>
      {workspaceRoot && (
        <button className="context-menu-item" onClick={ctx.handleCopyAbsolutePath} type="button">
          <ClipboardCopy size={13} />
          <span className="menu-item-label">Copy absolute path</span>
        </button>
      )}
    </ContextMenu>
  );
};

export const EditorContextMenu = React.memo(EditorContextMenuImpl, areContextMenuEqual);

export default EditorContextMenu;
