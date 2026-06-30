import { ContextMenu } from '@sprout/ui';
import type { FileSection, GitFile } from '../../types/git-types';
import { copyToClipboard } from '../../utils/clipboard';

export interface GitContextMenuProps {
  contextMenu: { x: number; y: number; section: FileSection; file: GitFile } | null;
  workspaceRoot?: string;
  onPreviewFile: (section: FileSection, path: string) => void;
  onOpenFile?: (path: string) => void;
  onStageFile: (path: string) => void;
  onUnstageFile: (path: string) => void;
  onDiscardFile: (path: string) => void;
  onClose: () => void;
}

function GitContextMenu({
  contextMenu,
  workspaceRoot,
  onPreviewFile,
  onOpenFile,
  onStageFile,
  onUnstageFile,
  onDiscardFile,
  onClose,
}: GitContextMenuProps) {
  if (!contextMenu) return null;

  return (
    <ContextMenu isOpen x={contextMenu.x} y={contextMenu.y} onClose={onClose}>
      <button
        className="context-menu-item"
        onClick={() => {
          onClose();
          onPreviewFile(contextMenu.section, contextMenu.file.path);
        }}
      >
        Preview diff
      </button>
      {onOpenFile && contextMenu.section !== 'deleted' && (
        <button
          className="context-menu-item"
          onClick={() => {
            onClose();
            onOpenFile(contextMenu.file.path);
          }}
        >
          Open in editor
        </button>
      )}
      {contextMenu.section !== 'deleted' && (
        <>
          <div className="context-menu-divider" />
          <button
            className="context-menu-item"
            onClick={() => {
              copyToClipboard(contextMenu.file.path);
              onClose();
            }}
          >
            Copy relative path
          </button>
          {workspaceRoot && (
            <button
              className="context-menu-item"
              onClick={() => {
                copyToClipboard(`${workspaceRoot.replace(/\/+$/, '')}/${contextMenu.file.path}`);
                onClose();
              }}
            >
              Copy absolute path
            </button>
          )}
        </>
      )}
      <div className="context-menu-divider" />
      {contextMenu.section === 'staged' ? (
        <button
          className="context-menu-item"
          onClick={() => {
            onClose();
            onUnstageFile(contextMenu.file.path);
          }}
        >
          Unstage
        </button>
      ) : (
        <button
          className="context-menu-item"
          onClick={() => {
            onClose();
            onStageFile(contextMenu.file.path);
          }}
        >
          Stage
        </button>
      )}
      <button
        className="context-menu-item danger"
        onClick={() => {
          onClose();
          onDiscardFile(contextMenu.file.path);
        }}
      >
        {contextMenu.section === 'deleted' ? 'Restore' : 'Delete'}
      </button>
    </ContextMenu>
  );
}

export default GitContextMenu;
