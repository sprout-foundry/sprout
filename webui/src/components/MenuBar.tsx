import { useState, useEffect, useCallback, useRef, useLayoutEffect } from 'react';
import { createPortal } from 'react-dom';
import { useHotkeys } from '../contexts/HotkeyContext';
import { ApiService } from '../services/api';
import './MenuBar.css';

const APP_VERSION = '1.0.0';

const activeDropdownRef = { current: null as HTMLDivElement | null };

interface MenuButtonItem {
  label: string;
  commandId?: string;
  divider?: boolean;
  disabled?: boolean;
  isToggle?: boolean;
}

interface MenuDef {
  title: string;
  mnemonic: string;
  items: MenuButtonItem[];
}

const MENUS: MenuDef[] = [
  {
    title: 'File',
    mnemonic: 'F',
    items: [
      { label: 'New File', commandId: 'new_file' },
      { label: 'Open File...', commandId: 'quick_open' },
      { label: 'Save', commandId: 'save_file' },
      { label: 'Save All', commandId: 'save_all_files' },
      { divider: true, label: '' },
      { label: 'Close Editor', commandId: 'close_editor' },
      { label: 'Close All Editors', commandId: 'close_all_editors' },
      { label: 'Close Other Editors', commandId: 'close_other_editors' },
    ],
  },
  {
    title: 'Edit',
    mnemonic: 'E',
    items: [
      { label: 'Undo', commandId: 'undo' },
      { label: 'Redo', commandId: 'redo' },
      { divider: true, label: '' },
      { label: 'Cut', commandId: 'editor_cut' },
      { label: 'Copy', commandId: 'editor_copy' },
      { label: 'Paste', commandId: 'editor_paste' },
      { divider: true, label: '' },
      { label: 'Find', commandId: 'editor_find' },
      { label: 'Find and Replace', commandId: 'editor_replace' },
      { divider: true, label: '' },
      { label: 'Select All', commandId: 'editor_select_all' },
      { divider: true, label: '' },
      { label: 'Command Palette...', commandId: 'command_palette' },
      { divider: true, label: '' },
      { label: 'Toggle File Explorer', commandId: 'toggle_explorer' },
      { label: 'Toggle Sidebar', commandId: 'toggle_sidebar' },
      { label: 'Toggle Terminal', commandId: 'toggle_terminal' },
    ],
  },
  {
    title: 'View',
    mnemonic: 'V',
    items: [
      { label: 'Switch to Chat', commandId: 'switch_to_chat' },
      { label: 'Switch to Editor', commandId: 'switch_to_editor' },
      { label: 'Switch to Git', commandId: 'switch_to_git' },
      { divider: true, label: '' },
      { label: 'Toggle Word Wrap', commandId: 'editor_toggle_word_wrap', isToggle: true },
      { label: 'Toggle Minimap', commandId: 'toggle_minimap', isToggle: true },
      { label: 'Toggle Linked Scrolling', commandId: 'toggle_linked_scroll', isToggle: true },
      { divider: true, label: '' },
      { label: 'Split Editor Vertical', commandId: 'split_editor_vertical' },
      { label: 'Split Editor Horizontal', commandId: 'split_editor_horizontal' },
      { label: 'Split Editor Grid', commandId: 'split_editor_grid' },
      { divider: true, label: '' },
      { label: 'Reset Saved Layout', commandId: 'reset_saved_layout' },
    ],
  },
  {
    title: 'Terminal',
    mnemonic: 'T',
    items: [
      { label: 'Focus Terminal', commandId: 'toggle_terminal' },
      { label: 'Split Terminal Vertical', commandId: 'split_terminal_vertical' },
      { label: 'Split Terminal Horizontal', commandId: 'split_terminal_horizontal' },
      { divider: true, label: '' },
      { label: 'Clear Terminal', commandId: 'clear_terminal' },
      { label: 'Kill Terminal', commandId: 'kill_terminal' },
    ],
  },
  {
    title: 'Help',
    mnemonic: 'H',
    items: [
      { label: 'Keyboard Shortcuts', commandId: 'open_hotkeys_config' },
      { divider: true, label: '' },
      { label: 'Export Diagnostics', commandId: 'export_diagnostics' },
      { label: 'Report Issue', commandId: 'open_report_issue' },
      { divider: true, label: '' },
      { label: 'About ledit', commandId: 'about' },
    ],
  },
];

function getActionableItems(menu: MenuDef): MenuButtonItem[] {
  return menu.items.filter((i) => !i.divider && !i.disabled);
}

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function getToggleState(commandId: string | undefined): boolean {
  if (!commandId) return false;
  try {
    switch (commandId) {
      case 'editor_toggle_word_wrap':
        // Defaults to true: word wrap is enabled unless explicitly disabled
        return localStorage.getItem('editor:word-wrap-enabled') !== 'false';
      case 'editor_toggle_linked_scroll':
        // Defaults to true unless explicitly disabled
        return localStorage.getItem('editor:linked-scroll-enabled') !== 'false';
      case 'toggle_minimap':
        // Defaults to false: minimap is off unless explicitly enabled
        return localStorage.getItem('editor:minimap-enabled') === 'true';
      default:
        return false;
    }
  } catch {
    return false;
  }
}

function MenuBar(): JSX.Element | null {
  const { hotkeyForCommand } = useHotkeys();
  const [activeMenuIndex, setActiveMenuIndex] = useState<number | null>(null);
  const [activeItemIndex, setActiveItemIndex] = useState<number | null>(null);
  const [showMnemonics, setShowMnemonics] = useState(false);
  const menuTitleRefs = useRef<(HTMLButtonElement | null)[]>([]);

  useEffect(() => {
    return () => {
      activeDropdownRef.current = null;
    };
  }, []);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'F10' && !e.ctrlKey && !e.metaKey && !e.shiftKey) {
        e.preventDefault();
        if (activeMenuIndex !== null) {
          setActiveMenuIndex(null);
          setActiveItemIndex(null);
        } else {
          setShowMnemonics(true);
          setActiveMenuIndex(0);
          setActiveItemIndex(0);
        }
        return;
      }
      if (e.key === 'Alt') {
        if (activeMenuIndex !== null) {
          e.preventDefault();
          setActiveMenuIndex(null);
          setActiveItemIndex(null);
        }
      } else if (e.altKey && !e.ctrlKey && !e.metaKey) {
        const key = e.key.toLowerCase();
        const menuIndex = MENUS.findIndex((m) => m.mnemonic.toLowerCase() === key);
        if (menuIndex !== -1) {
          e.preventDefault();
          e.stopPropagation();
          setShowMnemonics(true);
          setActiveMenuIndex((prev) => (prev === menuIndex ? null : menuIndex));
          setActiveItemIndex(0);
        } else if (activeMenuIndex !== null) {
          e.preventDefault();
          e.stopPropagation();
        }
      }
    };
    const handleKeyUp = (e: KeyboardEvent) => {
      if (e.key === 'Alt' || e.key === 'F10') {
        setShowMnemonics(false);
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    window.addEventListener('keyup', handleKeyUp);
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('keyup', handleKeyUp);
    };
  }, [activeMenuIndex]);

  useEffect(() => {
    if (activeMenuIndex === null) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      switch (e.key) {
        case 'Escape':
          e.preventDefault();
          setActiveMenuIndex(null);
          setActiveItemIndex(null);
          break;
        case 'ArrowRight':
          e.preventDefault();
          setActiveMenuIndex((prev) => (prev !== null ? (prev + 1) % MENUS.length : 0));
          setActiveItemIndex(0);
          break;
        case 'ArrowLeft':
          e.preventDefault();
          setActiveMenuIndex((prev) => (prev !== null ? (prev - 1 + MENUS.length) % MENUS.length : MENUS.length - 1));
          setActiveItemIndex(0);
          break;
        case 'ArrowDown':
          e.preventDefault();
          setActiveItemIndex((prev) => {
            const menuItems = getActionableItems(MENUS[activeMenuIndex]);
            return Math.min((prev ?? -1) + 1, menuItems.length - 1);
          });
          break;
        case 'ArrowUp':
          e.preventDefault();
          setActiveItemIndex((prev) => Math.max((prev ?? 0) - 1, 0));
          break;
        case 'Enter':
        case ' ':
          e.preventDefault();
          break;
        case 'Alt':
        case 'F10':
          e.preventDefault();
          setActiveMenuIndex(null);
          setActiveItemIndex(null);
          break;
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [activeMenuIndex]);

  const executeItem = useCallback((menuIndex: number, actionableIndex: number) => {
    const menu = MENUS[menuIndex];
    const item = getActionableItems(menu)[actionableIndex];
    if (!item) return;

    // Special command dispatching before the generic hotkey path
    switch (item.commandId) {
      case 'open_hotkeys_config':
        // Must dispatch to the correct listener in useHotkeysConfig.ts
        window.dispatchEvent(new CustomEvent('ledit:open-hotkeys-config'));
        break;
      case 'about':
        // Use window.alert for now (a proper dialog component is a separate task)
        window.alert(`ledit WebUI\nVersion ${APP_VERSION}\n\nA modern, keyboard-accessible code editor.`);
        break;
      case 'export_diagnostics':
        ApiService.getInstance()
          .exportSupportBundle()
          .catch((err) => console.error('Export diagnostics failed:', err));
        break;
      case 'open_report_issue':
        window.open('https://github.com/alantheprice/ledit/issues/new', '_blank', 'noopener,noreferrer');
        break;
      default:
        // Generic hotkey dispatch (handles all editor/terminal/view commands)
        if (item.commandId) {
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: item.commandId } }));
        }
        break;
    }

    setActiveMenuIndex(null);
    setActiveItemIndex(null);
  }, []);

  useEffect(() => {
    if (activeMenuIndex === null || activeItemIndex === null) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        e.stopPropagation();
        executeItem(activeMenuIndex, activeItemIndex);
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [activeMenuIndex, activeItemIndex, executeItem]);

  const handleTitleClick = useCallback((index: number) => {
    setActiveMenuIndex((prev) => (prev === index ? null : index));
    setActiveItemIndex(0);
  }, []);

  const handleItemAction = useCallback(
    (menuIndex: number, actionableIndex: number) => {
      executeItem(menuIndex, actionableIndex);
    },
    [executeItem],
  );

  const handleTitleMouseEnter = useCallback(
    (index: number) => {
      if (activeMenuIndex !== null) {
        setActiveMenuIndex(index);
        setActiveItemIndex(0);
      }
    },
    [activeMenuIndex],
  );

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      const target = e.target as Node;
      const clickedInsideMenu =
        menuTitleRefs.current.some((ref) => ref?.contains(target)) || activeDropdownRef.current?.contains(target);
      if (!clickedInsideMenu) {
        setActiveMenuIndex(null);
        setActiveItemIndex(null);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  return (
    <>
      <div className="menu-bar" role="menubar">
        {MENUS.map((menu, index) => {
          const isActive = activeMenuIndex === index;
          return (
            <button
              key={menu.mnemonic}
              ref={(el) => {
                menuTitleRefs.current[index] = el;
              }}
              className={`menu-bar-title ${isActive ? 'menu-bar-title--active' : ''}`}
              onClick={() => handleTitleClick(index)}
              onMouseEnter={() => handleTitleMouseEnter(index)}
              aria-expanded={isActive}
              aria-haspopup="menu"
              role="menuitem"
            >
              <span dangerouslySetInnerHTML={{ __html: buildMnemonicLabelHtml(menu.title, showMnemonics) }} />
            </button>
          );
        })}
      </div>
      {activeMenuIndex !== null &&
        createPortal(
          <MenuBarDropdown
            menuDef={MENUS[activeMenuIndex]}
            menuIndex={activeMenuIndex}
            anchorRef={menuTitleRefs.current[activeMenuIndex]}
            activeItemIndex={activeItemIndex}
            showMnemonics={showMnemonics}
            hotkeyForCommand={hotkeyForCommand}
            onItemAction={handleItemAction}
            onItemHover={setActiveItemIndex}
          />,
          document.body,
        )}
    </>
  );
}

interface MenuBarDropdownProps {
  menuDef: MenuDef;
  menuIndex: number;
  anchorRef: HTMLButtonElement | null;
  activeItemIndex: number | null;
  showMnemonics: boolean;
  hotkeyForCommand: (commandId: string) => string | null;
  onItemAction: (menuIndex: number, actionableIndex: number) => void;
  onItemHover: (index: number) => void;
}

function MenuBarDropdown({
  menuDef,
  menuIndex,
  anchorRef,
  activeItemIndex,
  showMnemonics,
  hotkeyForCommand,
  onItemAction,
  onItemHover,
}: MenuBarDropdownProps): JSX.Element | null {
  const dropdownRef = useRef<HTMLDivElement | null>(null);
  let actionableCounter = 0;

  // Sync the module-level ref so the parent's click-outside handler can
  // detect clicks inside the portal dropdown.
  const setDropdownEl = useCallback((el: HTMLDivElement | null) => {
    dropdownRef.current = el;
    activeDropdownRef.current = el;
  }, []);

  useLayoutEffect(() => {
    if (!dropdownRef.current || !anchorRef) return;
    const position = () => {
      if (!dropdownRef.current || !anchorRef) return;
      const anchorRect = anchorRef.getBoundingClientRect();
      const el = dropdownRef.current;
      const vw = window.innerWidth;
      const vh = window.innerHeight;

      // Pass 1: position below the anchor first
      let left = anchorRect.left;
      let top = anchorRect.bottom + 2;

      el.style.left = `${left}px`;
      el.style.top = `${top}px`;

      // Pass 2: now read the actual rect at the positioned location
      const elRect = el.getBoundingClientRect();
      if (elRect.right > vw) {
        left = Math.max(2, vw - elRect.width - 4);
      }
      if (elRect.bottom > vh) {
        top = anchorRect.top - elRect.height - 2;
      }
      if (top < 0) {
        top = 2;
      }

      el.style.left = `${left}px`;
      el.style.top = `${top}px`;
    };
    position();
    window.addEventListener('resize', position);
    window.addEventListener('scroll', position, true);
    return () => {
      window.removeEventListener('resize', position);
      window.removeEventListener('scroll', position, true);
    };
  }, [anchorRef, menuDef]);

  if (!anchorRef) return null;

  return (
    <div
      ref={setDropdownEl}
      className="menu-bar-dropdown"
      role="menu"
      aria-label={`${menuDef.title} menu`}
      id="menu-bar-dropdown"
      aria-activedescendant={activeItemIndex !== null ? `menu-bar-item-${menuIndex}-${activeItemIndex}` : undefined}
    >
      {menuDef.items.map((item, rawIndex) => {
        if (item.divider) {
          return <div key={`d-${rawIndex}`} className="context-menu-divider" role="separator" />;
        }
        const currentActionableIndex = actionableCounter;
        actionableCounter++;
        const shortcut = item.commandId && item.commandId !== 'about' ? hotkeyForCommand(item.commandId) : null;
        const isChecked = item.isToggle ? getToggleState(item.commandId) : false;
        return (
          <button
            key={`i-${rawIndex}`}
            id={`menu-bar-item-${menuIndex}-${currentActionableIndex}`}
            tabIndex={-1}
            className={`context-menu-item ${activeItemIndex === currentActionableIndex ? 'selected' : ''}`}
            onClick={() => onItemAction(menuIndex, currentActionableIndex)}
            onMouseEnter={() => onItemHover(currentActionableIndex)}
            role={item.isToggle ? 'menuitemcheckbox' : 'menuitem'}
            aria-checked={item.isToggle ? isChecked : undefined}
          >
            <span className="menu-item-label">
              {item.isToggle && <span className="menu-item-check">{isChecked ? '✓' : ''}</span>}
              <span dangerouslySetInnerHTML={{ __html: buildMnemonicLabelHtml(item.label, showMnemonics) }} />
            </span>
            {shortcut && <span className="menu-item-shortcut">{shortcut}</span>}
          </button>
        );
      })}
    </div>
  );
}

function buildMnemonicLabelHtml(label: string, showMnemonics: boolean): string {
  const escaped = escapeHtml(label);
  if (!showMnemonics) return escaped;
  return `<u>${escaped.charAt(0)}</u>${escaped.slice(1)}`;
}

export default MenuBar;
