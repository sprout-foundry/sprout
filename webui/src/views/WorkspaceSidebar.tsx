/**
 * The composed space shell's sidebar: the standalone app's project sidebar
 * (`ProjectNav`, the layered layout's L2) with its sections drilled into the
 * workspace's own views — Files (the file-tree unit), Search, Source control,
 * the Design sections — instead of the app's sidebar panels, which reach
 * app-only state.
 *
 * The host owns which project and which space is mounted, so the app's
 * project rail (L1) is not rendered and the entries are the mounted space's
 * own: Code lists Files / Search / Source control (when the host declares
 * `capabilities.git`) and, for an in-browser terminal, Terminal; Design lists
 * its sections. Collapsing leaves a slim rail that expands it again.
 *
 * Its look is the layered layout's stylesheet (`Layered.css`), which the app
 * loads with its own sidebar and the package loads with the views; it is not
 * imported here, so mounting the views does not move it in the app's cascade.
 */

import type { FileInfo, FileTreeHandle } from '@sprout/ui';
import { PanelLeftOpen } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import DesignAssetsPane from '../components/design/DesignAssetsPane';
import type { DesignTab } from '../components/design/DesignView';
import ProjectNav, { NAV_ICONS } from '../components/layered/ProjectNav';
import type { ProjectNavConversations, ProjectNavTarget } from '../components/layered/ProjectNav';
import ResizeHandle from '../components/ResizeHandle';
import { useWorkspaceKeyDown } from '../contexts/HotkeyContext';
import { SIDEBAR_DEFAULT_WIDTH, clampSidebarWidth } from '../hooks/useSidebarState';
import { useCommandHandler } from '../services/commandHandlers';
import { useWorkspaceFileTree } from './useWorkspaceFileTree';
import type { WorkspaceGitState } from './useWorkspaceGitState';
import { WorkspaceFileTreeView } from './WorkspaceFileTreeView';
import { WorkspaceGitStatusPanel } from './WorkspaceGitView';
import { WorkspaceSearchView } from './WorkspaceSearchView';
import '../components/Sidebar.css';

const CODE_SIDEBAR_COMMANDS: readonly string[] = ['open_search', 'toggle_sidebar', 'toggle_explorer'];
const DESIGN_SIDEBAR_COMMANDS: readonly string[] = ['toggle_sidebar'];

const SECTION_TITLES: Record<string, string> = {
  files: 'Files',
  search: 'Search',
  git: 'Source control',
  screens: 'Design',
  flows: 'Design',
  tokens: 'Design',
  feedback: 'Design',
};

const DESIGN_SECTIONS: ReadonlyArray<{ id: DesignTab; label: string }> = [
  { id: 'screens', label: 'Screens' },
  { id: 'flows', label: 'Flows' },
  { id: 'tokens', label: 'Tokens' },
  { id: 'feedback', label: 'Feedback' },
];

export interface WorkspaceSidebarProps {
  lane: 'code' | 'design';
  /** The project's display name, the sidebar's title. */
  title: string;
  collapsed: boolean;
  onCollapsedChange: (collapsed: boolean) => void;
  conversations: ProjectNavConversations;
  /** The git state, or null when the host has no git (no Source control entry). */
  git: WorkspaceGitState | null;
  /** The workspace's absolute root, for the tree's absolute-path copies. */
  workspaceRoot?: string;
  /** The path of the file the editor shows, selected in the tree. */
  selectedFile?: string;
  /** Open a workspace file in the editor, optionally at a line. */
  onOpenFile: (path: string, line?: number) => void;
  /** Offered as a Terminal entry (the in-browser shell has no strip of its own). */
  onToggleTerminal?: () => void;
  designTab: DesignTab;
  onDesignTabChange: (tab: DesignTab) => void;
}

export function WorkspaceSidebar({
  lane,
  title,
  collapsed,
  onCollapsedChange,
  conversations,
  git,
  workspaceRoot,
  selectedFile,
  onOpenFile,
  onToggleTerminal,
  designTab,
  onDesignTabChange,
}: WorkspaceSidebarProps): JSX.Element {
  const [open, setOpen] = useState<ProjectNavTarget | null>(() =>
    lane === 'design' ? { kind: 'design', id: designTab } : { kind: 'section', id: 'files' },
  );
  const [width, setWidth] = useState(SIDEBAR_DEFAULT_WIDTH);
  const [isResizing, setIsResizing] = useState(false);
  const treeRef = useRef<FileTreeHandle>(null);
  const toggleCollapsed = useCallback(() => onCollapsedChange(!collapsed), [collapsed, onCollapsedChange]);

  // A space switch shows that space's own sections.
  const previousLane = useRef(lane);
  useEffect(() => {
    if (previousLane.current === lane) return;
    previousLane.current = lane;
    setOpen(lane === 'design' ? { kind: 'design', id: designTab } : { kind: 'section', id: 'files' });
  }, [lane, designTab]);

  const show = useCallback(
    (target: ProjectNavTarget) => {
      setOpen(target);
      if (collapsed) onCollapsedChange(false);
    },
    [collapsed, onCollapsedChange],
  );

  // Ctrl/Cmd+\ collapses and expands it, as in the app.
  useWorkspaceKeyDown((event) => {
    if (event.key === '\\' && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      toggleCollapsed();
    }
  });

  useCommandHandler(lane === 'code' ? CODE_SIDEBAR_COMMANDS : DESIGN_SIDEBAR_COMMANDS);
  useEffect(() => {
    const handleHotkey = (event: Event) => {
      const commandId = (event as CustomEvent<{ commandId?: string }>).detail?.commandId;
      if (commandId === 'toggle_sidebar') toggleCollapsed();
      if (lane !== 'code') return;
      if (commandId === 'open_search') show({ kind: 'section', id: 'search' });
      if (commandId === 'toggle_explorer') {
        window.dispatchEvent(new CustomEvent('sprout:reveal-in-explorer', { detail: { path: selectedFile ?? '' } }));
      }
    };
    // "Reveal in file list" (the editor tab menu, Toggle Explorer, the
    // breadcrumb): show Files and reveal the path in the tree.
    const handleReveal = (event: Event) => {
      if (lane !== 'code') return;
      show({ kind: 'section', id: 'files' });
      const path = (event as CustomEvent<{ path?: string }>).detail?.path;
      if (path) setTimeout(() => treeRef.current?.revealFile(path), 100);
    };
    window.addEventListener('sprout:hotkey', handleHotkey);
    window.addEventListener('sprout:reveal-in-explorer', handleReveal);
    return () => {
      window.removeEventListener('sprout:hotkey', handleHotkey);
      window.removeEventListener('sprout:reveal-in-explorer', handleReveal);
    };
  }, [lane, show, toggleCollapsed, selectedFile]);

  const navigate = (target: ProjectNavTarget) => {
    if (target.kind === 'conversation') {
      if (target.id) conversations.onSelect(target.id);
      else conversations.onOpen();
      setOpen(null);
      return;
    }
    if (target.id === 'terminal') {
      onToggleTerminal?.();
      return;
    }
    if (target.kind === 'design') onDesignTabChange(target.id as DesignTab);
    setOpen(target);
  };

  const entry = (kind: 'section' | 'design', id: keyof typeof NAV_ICONS, label: string) => ({
    target: { kind, id } as ProjectNavTarget,
    label,
    icon: NAV_ICONS[id],
  });
  const codeEntries =
    lane === 'code'
      ? [
          entry('section', 'files', 'Files'),
          entry('section', 'search', 'Search'),
          ...(git ? [entry('section', 'git', 'Source control')] : []),
          ...(onToggleTerminal ? [entry('section', 'terminal', 'Terminal')] : []),
        ]
      : [];
  const designEntries = lane === 'design' ? DESIGN_SECTIONS.map((s) => entry('design', s.id, s.label)) : [];

  if (collapsed) {
    return (
      <div className="sprout-workspace-shell__rail" data-testid="workspace-sidebar-rail">
        <button
          type="button"
          className="project-nav-back"
          onClick={() => onCollapsedChange(false)}
          title="Expand sidebar"
          aria-label="Expand sidebar"
        >
          <PanelLeftOpen size={15} />
        </button>
      </div>
    );
  }

  return (
    <div className="sidebar-resize-wrapper sprout-workspace-shell__sidebar" data-testid="workspace-sidebar">
      <div className={`sidebar layered${isResizing ? ' resizing' : ''}`} style={{ width: `${width}px` }}>
        <ProjectNav
          title={title}
          onHide={() => onCollapsedChange(true)}
          hideLabel="Collapse sidebar"
          conversations={conversations}
          conversationInMain={conversations.inMain && !open}
          current={open}
          onNavigate={navigate}
          codeEntries={codeEntries}
          designEntries={designEntries}
          automationEntries={[]}
          footerEntries={[]}
          drill={
            open
              ? {
                  title: SECTION_TITLES[open.id] ?? open.id,
                  content: (
                    <SidebarSection
                      id={open.id}
                      git={git}
                      treeRef={treeRef}
                      workspaceRoot={workspaceRoot}
                      selectedFile={selectedFile}
                      onOpenFile={onOpenFile}
                    />
                  ),
                  onBack: () => setOpen(null),
                }
              : undefined
          }
        />
      </div>
      <ResizeHandle
        direction="horizontal"
        onResize={(delta) => setWidth((current) => clampSidebarWidth(current + delta))}
        onResizeStart={() => setIsResizing(true)}
        onResizeEnd={() => setIsResizing(false)}
        onDoubleClick={() => setWidth(SIDEBAR_DEFAULT_WIDTH)}
        className="sidebar-resize-handle"
        ariaLabel="Resize sidebar"
      />
    </div>
  );
}

function SidebarSection({
  id,
  git,
  treeRef,
  workspaceRoot,
  selectedFile,
  onOpenFile,
}: {
  id: string;
  git: WorkspaceGitState | null;
  treeRef: React.RefObject<FileTreeHandle>;
  workspaceRoot?: string;
  selectedFile?: string;
  onOpenFile: (path: string, line?: number) => void;
}): JSX.Element | null {
  let content: ReactNode = null;
  if (id === 'files') {
    content = (
      <FilesSection
        treeRef={treeRef}
        workspaceRoot={workspaceRoot}
        selectedFile={selectedFile}
        onOpenFile={onOpenFile}
      />
    );
  } else if (id === 'search') {
    content = <WorkspaceSearchView onFileClick={onOpenFile} autoFocus />;
  } else if (id === 'git' && git) {
    content = <WorkspaceGitStatusPanel workspace={git.workspace} panel={git.panel} />;
  } else if (DESIGN_SECTIONS.some((section) => section.id === id)) {
    content = <DesignAssetsPane />;
  }
  return <>{content}</>;
}

function FilesSection({
  treeRef,
  workspaceRoot,
  selectedFile,
  onOpenFile,
}: {
  treeRef: React.RefObject<FileTreeHandle>;
  workspaceRoot?: string;
  selectedFile?: string;
  onOpenFile: (path: string, line?: number) => void;
}): JSX.Element {
  const onFileSelect = useCallback(
    (file: FileInfo) => {
      if (!file.isDir) onOpenFile(file.path);
    },
    [onOpenFile],
  );
  const tree = useWorkspaceFileTree({ onFileSelect, selectedFile, workspaceRoot });
  return <WorkspaceFileTreeView ref={treeRef} {...tree} />;
}
