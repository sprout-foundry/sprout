import { Save, Sun, Moon, Loader2 } from 'lucide-react';
import { memo, type ReactNode } from 'react';
import { useTheme } from '../contexts/ThemeContext';
import EditorBreadcrumb, { type BreadcrumbSymbol } from './EditorBreadcrumb';
import './EditorToolbar.css';

interface EditorToolbarProps {
  onSave: () => void;
  saving?: boolean;
  showSave?: boolean;
  breadcrumbProps?: {
    filePath: string;
    onNavigate?: (path: string) => void;
    symbols?: BreadcrumbSymbol[];
    onNavigateToSymbol?: (line: number) => void;
  };
  actions?: Array<{
    id: string;
    title: string;
    icon: ReactNode;
    onClick: () => void;
    active?: boolean;
    disabled?: boolean;
  }>;
  rightActions?: Array<{
    id: string;
    title: string;
    icon: ReactNode;
    onClick: () => void;
    active?: boolean;
    disabled?: boolean;
  }>;
}

function EditorToolbar({
  onSave,
  saving = false,
  showSave = true,
  breadcrumbProps,
  actions = [],
  rightActions = [],
}: EditorToolbarProps): JSX.Element {
  const { theme, themePack, toggleTheme } = useTheme();

  return (
    <div className="editor-toolbar">
      <div className="toolbar-group">
        {breadcrumbProps && (
          <>
            <div className="toolbar-breadcrumb">
              <EditorBreadcrumb
                filePath={breadcrumbProps.filePath}
                onNavigate={breadcrumbProps.onNavigate}
                symbols={breadcrumbProps.symbols}
                onNavigateToSymbol={breadcrumbProps.onNavigateToSymbol}
              />
            </div>
            <div className="toolbar-separator" />
          </>
        )}

        {actions.map((action) => (
          <button
            key={action.id}
            className={`toolbar-button ${action.active ? 'active' : ''}`}
            onClick={action.onClick}
            title={action.title}
            disabled={action.disabled}
          >
            <span className="toolbar-icon">{action.icon}</span>
          </button>
        ))}
      </div>

      <div className="toolbar-group">
        {rightActions.map((action) => (
          <button
            key={action.id}
            className={`toolbar-button ${action.active ? 'active' : ''}`}
            onClick={action.onClick}
            title={action.title}
            disabled={action.disabled}
          >
            <span className="toolbar-icon">{action.icon}</span>
          </button>
        ))}

        {/* Save */}
        {showSave ? (
          <button className="toolbar-button" onClick={onSave} title="Save file (Ctrl+S)" disabled={saving}>
            {saving ? (
              <span className="toolbar-icon">
                <Loader2 size={16} className="spinner" />
              </span>
            ) : (
              <span className="toolbar-icon">
                <Save size={16} />
              </span>
            )}
          </button>
        ) : null}

        {/* Theme Toggle */}
        <button
          className="toolbar-button"
          onClick={toggleTheme}
          title={`Switch mode from ${themePack.name} to ${theme === 'dark' ? 'light' : 'dark'}`}
        >
          <span className="toolbar-icon">{theme === 'dark' ? <Sun size={16} /> : <Moon size={16} />}</span>
        </button>
      </div>
    </div>
  );
}

export default memo(EditorToolbar);
