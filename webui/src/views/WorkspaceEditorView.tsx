import { File, X } from 'lucide-react';
import EditorPane from '../components/EditorPane';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { useWorkspaceEditor } from './WorkspaceEditorContext';
import '../components/EditorTabs.css';
import './WorkspaceEditorView.css';

export interface WorkspaceEditorViewProps {
  /** Additional class on the view root, alongside `sprout-workspace-editor`. */
  className?: string;
}

/**
 * The assembled editor view: a tab per open file over the active pane's
 * editor. It is what an arranged `editor` view renders in an own-mode
 * `SproutWorkspace` (unless the host supplies `viewProps.editor`, which keeps
 * the bare `Editor` primitive). Files reach it through the editor unit
 * (`useWorkspaceEditor`) or the file tree; loading, editing and saving
 * (Cmd/Ctrl+S, auto-save) run through the same buffer plumbing as the
 * standalone app's editor pane.
 *
 * Only file buffers show here. The buffer manager also holds the app's chat,
 * diff and review tabs, which belong to the app's own workspace panes (they
 * take the chat's view props), so they get no tab in this view.
 *
 * Must be mounted under `WorkspaceEditorProvider`.
 */
export function WorkspaceEditorView({ className }: WorkspaceEditorViewProps): JSX.Element {
  const { openFiles, activeFile, openFile, closeFile } = useWorkspaceEditor();
  const { panes, buffers, activePaneId } = useEditorManager();
  const pane = panes.find((p) => p.id === activePaneId) ?? panes[0];
  const paneBuffer = pane?.bufferId ? buffers.get(pane.bufferId) : undefined;
  const rootClass = className ? `sprout-workspace-editor ${className}` : 'sprout-workspace-editor';

  return (
    <div className={rootClass} data-testid="workspace-editor">
      {openFiles.length > 0 && (
        <div className="editor-tabs compact">
          <div className="tabs-container">
            <div className="tabs-list" role="tablist">
              {openFiles.map((file) => {
                const active = file.bufferId === activeFile?.bufferId;
                return (
                  <div
                    key={file.bufferId}
                    className={`tab${active ? ' active focused' : ''}`}
                    role="tab"
                    aria-selected={active}
                    title={file.workspacePath}
                    onClick={() => openFile(file.path)}
                  >
                    <span className="tab-name">{file.name}</span>
                    {file.isModified && <span className="tab-modified" aria-label="Unsaved changes" />}
                    <button
                      type="button"
                      className="tab-close"
                      aria-label={`Close ${file.name}`}
                      onClick={(event) => {
                        event.stopPropagation();
                        void closeFile(file.path);
                      }}
                    >
                      <X size={12} />
                    </button>
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      )}
      <div className="sprout-workspace-editor__pane">
        {pane && paneBuffer?.kind === 'file' ? (
          <EditorPane paneId={pane.id} />
        ) : (
          <div className="editor-pane empty">
            <div className="no-file-selected">
              <div className="no-file-icon">
                <File size={32} />
              </div>
              <div className="no-file-text">Open a file</div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

export default WorkspaceEditorView;
