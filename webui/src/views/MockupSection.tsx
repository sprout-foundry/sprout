/**
 * MockupSection — the Design lane's sidebar drill: the mockup and its file.
 *
 * A mockup is one document (`design/mockup.html`), so the section shows what
 * the workspace has and opens it in the editor. Everything else about the
 * mockup happens in the chat.
 */

import { FileCode2 } from 'lucide-react';
import type { JSX } from 'react';
import { MOCKUP_PATH } from '../components/design/MockupPane';

export interface MockupSectionProps {
  workspaceRoot?: string;
  onOpenFile: (path: string, line?: number) => void;
}

export function MockupSection({ workspaceRoot, onOpenFile }: MockupSectionProps): JSX.Element {
  return (
    <div className="sprout-workspace-shell__mockup" data-testid="workspace-mockup-section">
      <button
        type="button"
        className="project-nav-item"
        onClick={() => onOpenFile(MOCKUP_PATH)}
        title={workspaceRoot ? `${workspaceRoot}/${MOCKUP_PATH}` : MOCKUP_PATH}
      >
        <FileCode2 size={15} aria-hidden="true" />
        <span className="project-nav-item-label">mockup.html</span>
      </button>
      <p className="sprout-workspace-shell__mockup-hint">
        Ask for a screen in the chat — it is drawn here and in the Design surface.
      </p>
    </div>
  );
}
