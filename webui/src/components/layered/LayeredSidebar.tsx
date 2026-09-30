/**
 * The layered layout's left side: the project rail (L1) and the project
 * sidebar (L2). Owns only which L2 entry is open; the panels it drills into
 * are the classic sidebar's own sections, rendered by the caller.
 */

import { useState, type ReactElement, type ReactNode } from 'react';
import { isCloud, supportsAutomations, supportsGit, supportsSettings } from '../../config/mode';
import type { SectionTab } from '../../hooks/useSidebarState';
import type { SproutInstance } from '../../services/api';
import { useActiveRepoURL } from '../../services/activeRepo';
import { closeHome, openHome, searchForRepo, useHomeView } from '../../services/homeView';
import { useRecentRepos } from '../../services/recentRepos';
import { parseRepoRef } from '../../services/workspaceFs/workspaceGit';
import { showThemedAlert, showThemedPrompt } from '../ThemedDialog';
import type { ViewType } from '../../types/app';
import { githubRepoSlug } from '../../utils/platformUrl';
import type { WorkspaceMode, WorkspaceModeId } from '../../workspaces/registry';
import ProjectNav, { NAV_ICONS, type ProjectNavConversations, type ProjectNavTarget } from './ProjectNav';
import HomeNav from './HomeNav';
import NewProjectDialog from './NewProjectDialog';
import ProjectRail, { type RailProject } from './ProjectRail';
import './Layered.css';

const SECTION_TITLES: Record<string, string> = {
  files: 'Files',
  search: 'Search',
  git: 'Source control',
  automations: 'Automations',
  settings: 'Settings',
  logs: 'Logs',
  screens: 'Design',
  tokens: 'Design',
  flows: 'Design',
  feedback: 'Design',
};

function basename(path: string | undefined): string {
  const trimmed = (path ?? '').replace(/\/+$/, '');
  return trimmed.split('/').pop() || trimmed || 'Workspace';
}

export interface LayeredSidebarProps {
  conversations?: ProjectNavConversations;
  currentView?: ViewType;
  onViewChange?: (view: ViewType) => void;
  modes: WorkspaceMode[];
  activeModeId: WorkspaceModeId;
  onSelectMode?: (id: WorkspaceModeId) => void;
  modeSection?: string;
  onModeSectionChange?: (id: string) => void;
  onSectionChange?: (section: SectionTab) => void;
  workspaceRoot?: string;
  instances: SproutInstance[];
  onInstanceChange?: (pid: number) => void;
  /** Replaces the plain project title with a control that switches projects. */
  projectSwitcher?: ReactNode;
  /** Renders the classic sidebar panel for the selected section. */
  renderSection: () => ReactNode;
  /** Collapsed: only the rail shows (narrow windows, tablets). */
  collapsed?: boolean;
  onToggleCollapsed?: () => void;
  isMobile?: boolean;
  /** Phones: close the drawer once the main view changes. */
  onCloseDrawer?: () => void;
}

// Opening a repository reloads onto ?repo=, the same clone-or-restore path
// a dashboard "Open in editor" link takes.
function openRepo(url: string): void {
  if (new URLSearchParams(window.location.search).get('repo') === url) return;
  window.location.search = searchForRepo(url);
}

async function promptForRepo(): Promise<void> {
  const input = await showThemedPrompt('GitHub repository to open (owner/name or URL):', {
    title: 'Open a repository',
    placeholder: 'owner/repo',
  });
  if (!input?.trim()) return;
  try {
    openRepo(parseRepoRef(input.trim()).url.replace(/\.git$/, ''));
  } catch (err) {
    await showThemedAlert(err instanceof Error ? err.message : String(err), {
      title: 'Invalid repository',
      type: 'warning',
    });
  }
}

export default function LayeredSidebar(props: LayeredSidebarProps): ReactElement {
  const [open, setOpen] = useState<ProjectNavTarget | null>(null);
  const [creating, setCreating] = useState(false);
  const activeRepo = useActiveRepoURL();
  const repoSlug = githubRepoSlug(activeRepo);
  const recentRepos = useRecentRepos();
  const home = useHomeView();
  const title = isCloud ? (repoSlug ?? 'No repository open') : basename(props.workspaceRoot);

  const inCode = props.activeModeId !== 'design';
  const conversationInMain = inCode && !!props.conversations?.inMain;

  // A new conversation opens in the main view, like picking one does.
  const createConversation = props.conversations?.onCreate;
  const conversations =
    props.conversations && createConversation
      ? {
          ...props.conversations,
          onCreate: () => {
            closeHome();
            if (!inCode) props.onSelectMode?.('code');
            setOpen(null);
            createConversation();
            props.onCloseDrawer?.();
          },
        }
      : props.conversations;

  const navigate = (target: ProjectNavTarget) => {
    closeHome();
    if (target.kind === 'conversation') {
      if (!inCode) props.onSelectMode?.('code');
      // A named chat opens itself; the implicit single chat (no id) opens
      // whichever conversation is current.
      if (target.id) props.conversations?.onSelect(target.id);
      else props.conversations?.onOpen();
      setOpen(null);
      props.onCloseDrawer?.();
      return;
    }
    if (target.kind === 'design') {
      if (inCode) props.onSelectMode?.('design');
      props.onModeSectionChange?.(target.id);
      setOpen(target);
      return;
    }
    if (target.id === 'terminal') {
      window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'toggle_terminal' } }));
      props.onCloseDrawer?.();
      return;
    }
    const global = target.id === 'settings' || target.id === 'logs';
    if (!global && !inCode) props.onSelectMode?.('code');
    props.onSectionChange?.(target.id as SectionTab);
    setOpen(target);
  };

  const entry = (kind: 'section' | 'design', id: keyof typeof NAV_ICONS, label: string) => ({
    target: { kind, id } as ProjectNavTarget,
    label,
    icon: NAV_ICONS[id],
  });

  const codeEntries = [
    entry('section', 'files', 'Files'),
    entry('section', 'search', 'Search'),
    ...(supportsGit ? [entry('section', 'git', 'Source control')] : []),
    // Hosted: the in-browser terminal stays hidden until asked for.
    ...(isCloud ? [entry('section', 'terminal', 'Terminal')] : []),
  ];
  const designEntries = props.modes.some((m) => m.id === 'design')
    ? [
        entry('design', 'screens', 'Screens'),
        entry('design', 'flows', 'Flows'),
        entry('design', 'tokens', 'Tokens'),
        entry('design', 'feedback', 'Feedback'),
      ]
    : [];
  const automationEntries = supportsAutomations ? [entry('section', 'automations', 'Workflows')] : [];
  const footerEntries = [
    ...(supportsSettings ? [entry('section', 'settings', 'Settings')] : []),
    entry('section', 'logs', 'Logs'),
  ];

  // Design sections render the design assets pane, the same panel the
  // classic sidebar shows beside the design rail; the L2 entries stand in
  // for the rail's icons.
  const drillContent = props.renderSection();

  const projects: RailProject[] = isCloud
    ? recentRepos.map((url) => ({
        id: url,
        label: githubRepoSlug(url) ?? url,
        active: !home.open && githubRepoSlug(url) === repoSlug,
        onSelect: () => {
          if (githubRepoSlug(url) === repoSlug) closeHome();
          else openRepo(url);
          props.onCloseDrawer?.();
        },
      }))
    : props.instances.length > 0
      ? props.instances.map((inst) => ({
          id: String(inst.pid),
          label: basename(inst.working_dir),
          active: inst.is_current,
          onSelect: () => {
            props.onInstanceChange?.(inst.pid);
            props.onCloseDrawer?.();
          },
        }))
      : [{ id: 'local', label: title, active: true }];

  return (
    <>
      <ProjectRail
        projects={projects}
        collapsed={props.collapsed}
        onToggleCollapsed={props.isMobile ? undefined : props.onToggleCollapsed}
        addActions={
          isCloud
            ? [
                { label: 'New project…', onSelect: () => setCreating(true) },
                { label: 'Open a repository…', onSelect: () => void promptForRepo() },
              ]
            : undefined
        }
        onOpenSettings={() => navigate({ kind: 'section', id: 'settings' })}
        homeActive={home.open}
        onOpenActivity={props.onCloseDrawer}
        onOpenHome={() => {
          // On phones the rail sits inside the drawer: choosing a place
          // there should show it, as choosing a sidebar entry does.
          openHome(home.path);
          props.onCloseDrawer?.();
        }}
      />
      {!props.collapsed && home.open && (
        <HomeNav
          path={home.path}
          projectLabel={isCloud && !repoSlug ? 'the editor' : title}
          onBackToProject={() => {
            closeHome();
            props.onCloseDrawer?.();
          }}
          onNavigated={props.onCloseDrawer}
        />
      )}
      {!props.collapsed && !home.open && (
        <ProjectNav
          title={title}
          titleControl={props.projectSwitcher}
          onHide={props.onToggleCollapsed}
          hideLabel={props.isMobile ? 'Close sidebar' : 'Collapse sidebar'}
          conversations={conversations}
          conversationInMain={conversationInMain && !open}
          current={open}
          onNavigate={navigate}
          codeEntries={codeEntries}
          designEntries={designEntries}
          automationEntries={automationEntries}
          footerEntries={footerEntries}
          drill={
            open
              ? { title: SECTION_TITLES[open.id] ?? open.id, content: drillContent, onBack: () => setOpen(null) }
              : undefined
          }
        />
      )}
      {creating && <NewProjectDialog onClose={() => setCreating(false)} onCreated={openRepo} />}
    </>
  );
}
