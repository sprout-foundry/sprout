import { Loader2 } from 'lucide-react';
import { memo, type ReactNode } from 'react';
import EditorBreadcrumb, { type BreadcrumbSymbol } from './EditorBreadcrumb';
import './EditorToolbar.css';

interface EditorToolbarProps {
  /** Saving spinner displayed in the breadcrumb area while a save is in
   *  flight. Save is triggered by Ctrl/⌘+S or the omnibox; there's no
   *  longer a dedicated Save button (the dirty `●` on the tab + hotkey
   *  cover the affordance). */
  saving?: boolean;
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
  saving = false,
  breadcrumbProps,
  actions = [],
  rightActions = [],
}: EditorToolbarProps): JSX.Element {
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
            {saving && (
              <span className="toolbar-saving" title="Saving…" aria-label="Saving">
                <Loader2 size={12} className="spinner" />
              </span>
            )}
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
      </div>
    </div>
  );
}

export default memo(EditorToolbar);
