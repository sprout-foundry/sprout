import { X } from 'lucide-react';
import { useEffect, useMemo } from 'react';
import { useHotkeys } from '../contexts/HotkeyContext';
import './KeyboardShortcutsModal.css';

interface KeyboardShortcutsModalProps {
  onClose: () => void;
}

interface ShortcutRow {
  commandId: string;
  label: string;
}

interface ShortcutGroup {
  title: string;
  shortcuts: ShortcutRow[];
}

const SHORTCUT_GROUPS: ShortcutGroup[] = [
  {
    title: 'File',
    shortcuts: [
      { commandId: 'save_file', label: 'Save current file' },
      { commandId: 'save_all_files', label: 'Save all files' },
      { commandId: 'close_editor', label: 'Close editor' },
    ],
  },
  {
    title: 'Navigation',
    shortcuts: [
      { commandId: 'focus_tab_1', label: 'Focus tab 1' },
      { commandId: 'focus_tab_2', label: 'Focus tab 2' },
      { commandId: 'focus_tab_3', label: 'Focus tab 3' },
      { commandId: 'focus_tab_4', label: 'Focus tab 4' },
      { commandId: 'focus_tab_5', label: 'Focus tab 5' },
      { commandId: 'focus_tab_6', label: 'Focus tab 6' },
      { commandId: 'focus_tab_7', label: 'Focus tab 7' },
      { commandId: 'focus_tab_8', label: 'Focus tab 8' },
      { commandId: 'focus_tab_9', label: 'Focus tab 9' },
      { commandId: 'focus_next_tab', label: 'Next tab' },
      { commandId: 'focus_prev_tab', label: 'Previous tab' },
      { commandId: 'switch_to_editor', label: 'Switch to editor' },
      { commandId: 'switch_to_chat', label: 'Switch to chat' },
      { commandId: 'switch_to_git', label: 'Switch to git' },
    ],
  },
  {
    title: 'Editor',
    shortcuts: [
      { commandId: 'split_editor_horizontal', label: 'Split editor horizontally' },
      { commandId: 'format_document', label: 'Format document' },
    ],
  },
];

const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);

function KeyboardShortcutsModal({ onClose }: KeyboardShortcutsModalProps): JSX.Element {
  const { hotkeyForCommand } = useHotkeys();

  // Escape closes the modal
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [onClose]);

  const groups = useMemo(
    () =>
      SHORTCUT_GROUPS.map((group) => ({
        ...group,
        shortcuts: group.shortcuts
          .map((row) => ({
            ...row,
            key: hotkeyForCommand(row.commandId),
          }))
          .filter((row) => row.key !== null),
      })).filter((group) => group.shortcuts.length > 0),
    [hotkeyForCommand],
  );

  const renderKey = (key: string) => {
    // Replace Cmd with the platform symbol on macOS for readability
    const display = isMac ? key.replace(/\bCmd\b/g, '⌘').replace(/\bCtrl\b/g, '⌃') : key;
    return display.split('+').map((part, idx, arr) => (
      <span key={`${part}-${idx}`} className="kb-key-sequence">
        <kbd className="kb-key">{part}</kbd>
        {idx < arr.length - 1 && <span className="kb-key-sep">+</span>}
      </span>
    ));
  };

  return (
    <div className="kb-modal-overlay" role="dialog" aria-modal="true" aria-label="Keyboard Shortcuts" onClick={onClose}>
      <div className="kb-modal-card" onClick={(e) => e.stopPropagation()}>
        <div className="kb-modal-header">
          <h2 className="kb-modal-title">Keyboard Shortcuts</h2>
          <button type="button" className="kb-modal-close" onClick={onClose} aria-label="Close keyboard shortcuts">
            <X size={16} />
          </button>
        </div>
        <div className="kb-modal-body">
          {groups.length === 0 ? (
            <div className="kb-modal-empty">No shortcuts configured yet.</div>
          ) : (
            groups.map((group) => (
              <section key={group.title} className="kb-group">
                <h3 className="kb-group-title">{group.title}</h3>
                <ul className="kb-list">
                  {group.shortcuts.map((row) => (
                    <li key={row.commandId} className="kb-row">
                      <span className="kb-label">{row.label}</span>
                      <span className="kb-keys">{row.key && renderKey(row.key)}</span>
                    </li>
                  ))}
                </ul>
              </section>
            ))
          )}
        </div>
      </div>
    </div>
  );
}

export default KeyboardShortcutsModal;
