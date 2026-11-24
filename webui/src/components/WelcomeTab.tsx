import { Terminal, GitBranch, MessageSquare, Zap, BookOpen, Settings, Command, X } from 'lucide-react';
import { copy } from '../config/copy';
import { useHostCapabilities } from '../host';
import { useProjectFolder } from '../hooks/useProjectFolder';
import ProjectFolderPicker from './ProjectFolderPicker';
import './WelcomeTab.css';

/** Mac users see ⌘; everyone else sees Ctrl. Mirrors HotkeyContext logic. */
const PRIMARY_KEY = (() => {
  if (typeof navigator === 'undefined') return 'Ctrl';
  return /mac/i.test(navigator.platform) ? '⌘' : 'Ctrl';
})();

interface WelcomeTabProps {
  onDismiss?: () => void;
  onOpenCommandPalette?: () => void;
  onOpenTerminal?: () => void;
  onViewGit?: () => void;
  onStartChat?: () => void;
  onOpenSettings?: () => void;
  onOpenHotkeysConfig?: () => void;
}

/* ── Workspace Picker View ────────────────────────────────────────── */

function ProjectFolderPickerView({
  projectFolderInfo,
  homeDir,
  setProjectFolder,
}: {
  projectFolderInfo: ReturnType<typeof useProjectFolder>['projectFolderInfo'];
  homeDir: string;
  setProjectFolder: (path: string) => Promise<void>;
}): JSX.Element {
  const { projectFolderSwitching: supportsProjectFolderSwitching } = useHostCapabilities();
  const handleBrowse = () => {
    window.dispatchEvent(new CustomEvent('sprout:open-workspace-switcher'));
  };

  if (!supportsProjectFolderSwitching) {
    return (
      <div className="welcome-tab">
        <div className="welcome-header">
          <h1>{copy('welcome.pickerTitle')}</h1>
          <p>{copy('welcome.pickerSubtitle')}</p>
        </div>
      </div>
    );
  }

  return (
    <ProjectFolderPicker
      daemonRoot={projectFolderInfo.daemon_root}
      currentProjectFolder={projectFolderInfo.project_folder}
      suggestedProjects={projectFolderInfo.suggested_projects}
      recentProjectFolders={projectFolderInfo.recent_project_folders}
      onSelect={setProjectFolder}
      onBrowse={handleBrowse}
    />
  );
}

/* ── Main Welcome Content ─────────────────────────────────────────── */

function WelcomeContent({
  onDismiss,
  onOpenCommandPalette,
  onOpenTerminal,
  onViewGit,
  onStartChat,
  onOpenSettings,
  onOpenHotkeysConfig,
}: WelcomeTabProps): JSX.Element {
  const quickActions = [
    {
      icon: <Command size={20} />,
      title: 'Open Command Palette',
      description: `Press ${PRIMARY_KEY}+P to search and open files`,
      action: onOpenCommandPalette,
    },
    {
      icon: <Terminal size={20} />,
      title: 'Open Terminal',
      description: 'Run commands and shell scripts',
      action: onOpenTerminal,
    },
    {
      icon: <GitBranch size={20} />,
      title: 'View Git History',
      description: 'Browse commits and file history',
      action: onViewGit,
    },
    {
      icon: <MessageSquare size={20} />,
      title: 'Start Chat',
      description: 'Ask for code help or analysis',
      action: onStartChat,
    },
  ];

  const helpfulLinks = [
    {
      icon: <BookOpen size={18} />,
      title: 'Documentation',
      description: 'Learn how to use sprout',
      action: () => window.open('https://sprout.dev/docs', '_blank'),
    },
    {
      icon: <Settings size={18} />,
      title: 'Settings',
      description: 'Customize your editor',
      action: onOpenSettings ?? (() => window.dispatchEvent(new CustomEvent('sprout:open-settings-focus'))),
    },
    {
      icon: <Zap size={18} />,
      title: 'Keyboard Shortcuts',
      description: 'Edit your bindings',
      action: onOpenHotkeysConfig ?? (() => window.dispatchEvent(new CustomEvent('sprout:open-hotkeys-config'))),
    },
  ];

  return (
    <div data-testid="editor-empty">
      <div className="welcome-header">
        <div className="welcome-header-content">
          <h1>{copy('welcome.title')}</h1>
          <p>{copy('welcome.subtitle')}</p>
        </div>
        {onDismiss && (
          <button className="welcome-dismiss-btn" onClick={onDismiss} title="Dismiss welcome tab">
            <X size={18} />
          </button>
        )}
      </div>

      <div className="welcome-content">
        <section className="welcome-section">
          <h2>Quick Actions</h2>
          <div className="quick-actions-grid">
            {quickActions.map((action, index) => (
              <button
                key={index}
                className="quick-action-card"
                onClick={() => action.action?.()}
                type="button"
                disabled={!action.action}
              >
                <div className="quick-action-icon">{action.icon}</div>
                <div className="quick-action-content">
                  <div className="quick-action-title">{action.title}</div>
                  <div className="quick-action-description">{action.description}</div>
                </div>
              </button>
            ))}
          </div>
        </section>

        {/* The legacy "Get Started" section was decorative cards that
         * duplicated Quick Actions (Command Palette, Run Commands, View Git,
         * AI Chat) with no click handler. Removed in favor of Quick Actions
         * + Resources, which actually do things. */}

        <section className="welcome-section welcome-links">
          <h2>Resources</h2>
          <div className="resources-grid">
            {helpfulLinks.map((link, index) => (
              <button
                key={index}
                className="resource-card"
                type="button"
                onClick={() => link.action?.()}
                disabled={!link.action}
              >
                <div className="resource-icon">{link.icon}</div>
                <div className="resource-content">
                  <div className="resource-title">{link.title}</div>
                  <div className="resource-description">{link.description}</div>
                </div>
              </button>
            ))}
          </div>
        </section>
      </div>

      <div className="welcome-footer">
        <p>
          Pro tip: Press <kbd>{PRIMARY_KEY}+P</kbd> to open the command palette and search for any command or file
        </p>
      </div>
    </div>
  );
}

/* ── WelcomeTab Root Component ─────────────────────────────────────── */

function WelcomeTab(props: WelcomeTabProps): JSX.Element {
  const { projectFolderInfo, homeDir, isLoading, setProjectFolder } = useProjectFolder();

  // When project folder selection is needed (not a project), show the picker
  if (!isLoading && projectFolderInfo.needs_project_folder_selection) {
    return (
      <div className="welcome-tab" data-testid="editor-welcome-tab">
        <ProjectFolderPickerView
          projectFolderInfo={projectFolderInfo}
          homeDir={homeDir}
          setProjectFolder={setProjectFolder}
        />
      </div>
    );
  }

  // Normal welcome content
  return (
    <div className="welcome-tab" data-testid="editor-welcome-tab">
      <WelcomeContent {...props} />
    </div>
  );
}

export default WelcomeTab;
