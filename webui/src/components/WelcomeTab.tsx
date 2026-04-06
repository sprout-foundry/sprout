import { File, Folder, Terminal, GitBranch, MessageSquare, Zap, BookOpen, Settings, Command, X } from 'lucide-react';
import './WelcomeTab.css';

interface WelcomeTabProps {
  onDismiss?: () => void;
  onOpenCommandPalette?: () => void;
  onOpenTerminal?: () => void;
  onViewGit?: () => void;
  onStartChat?: () => void;
}

function WelcomeTab({
  onDismiss,
  onOpenCommandPalette,
  onOpenTerminal,
  onViewGit,
  onStartChat,
}: WelcomeTabProps): JSX.Element {
  // Sample quick actions
  const quickActions = [
    {
      icon: <Command size={20} />,
      title: 'Open Command Palette',
      description: 'Press Ctrl+P to search and open files',
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
      description: 'Learn how to use ledit',
      action: () => window.open('https://ledit.dev/docs', '_blank'),
    },
    {
      icon: <Settings size={18} />,
      title: 'Settings',
      description: 'Customize your editor',
      action: onOpenCommandPalette,
    },
    {
      icon: <Zap size={18} />,
      title: 'Keyboard Shortcuts',
      description: 'Master the shortcuts',
      action: onOpenCommandPalette,
    },
  ];

  return (
    <div className="welcome-tab">
      <div className="welcome-header">
        <div className="welcome-header-content">
          <h1>Welcome to ledit</h1>
          <p>Your AI-powered code editor</p>
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
              <button key={index} className="quick-action-card" onClick={() => action.action?.()} type="button">
                <div className="quick-action-icon">{action.icon}</div>
                <div className="quick-action-content">
                  <div className="quick-action-title">{action.title}</div>
                  <div className="quick-action-description">{action.description}</div>
                </div>
              </button>
            ))}
          </div>
        </section>

        <section className="welcome-section">
          <h2>Get Started</h2>
          <div className="getting-started-grid">
            <div className="getting-started-card">
              <div className="gs-icon">
                <File size={24} />
              </div>
              <div className="gs-content">
                <h3>Open a File</h3>
                <p>Select a file from the file tree or use Ctrl+P to search</p>
              </div>
            </div>

            <div className="getting-started-card">
              <div className="gs-icon">
                <Folder size={24} />
              </div>
              <div className="gs-content">
                <h3>Navigate Projects</h3>
                <p>Use the file tree to browse your workspace</p>
              </div>
            </div>

            <div className="getting-started-card">
              <div className="gs-icon">
                <Terminal size={24} />
              </div>
              <div className="gs-content">
                <h3>Run Commands</h3>
                <p>Use the integrated terminal for shell commands</p>
              </div>
            </div>

            <div className="getting-started-card">
              <div className="gs-icon">
                <GitBranch size={24} />
              </div>
              <div className="gs-content">
                <h3>Version Control</h3>
                <p>View and manage git history and changes</p>
              </div>
            </div>

            <div className="getting-started-card">
              <div className="gs-icon">
                <MessageSquare size={24} />
              </div>
              <div className="gs-content">
                <h3>AI Assistance</h3>
                <p>Chat with AI to get code help and analysis</p>
              </div>
            </div>

            <div className="getting-started-card">
              <div className="gs-icon">
                <Command size={24} />
              </div>
              <div className="gs-content">
                <h3>Command Palette</h3>
                <p>Access all commands with Ctrl+P</p>
              </div>
            </div>
          </div>
        </section>

        <section className="welcome-section welcome-links">
          <h2>Resources</h2>
          <div className="resources-grid">
            {helpfulLinks.map((link, index) => (
              <button key={index} className="resource-card" type="button" onClick={() => link.action?.()}>
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
          Pro tip: Press <kbd>Ctrl+P</kbd> to open the command palette and search for any command or file
        </p>
      </div>
    </div>
  );
}

export default WelcomeTab;
