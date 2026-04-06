import { useState, useEffect, useCallback, useRef, useLayoutEffect } from 'react';
import { createPortal } from 'react-dom';
import { useHotkeys } from '../contexts/HotkeyContext';
import './MenuBar.css';

// ── Menu item types ───────────────────────────────────────────────────

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

// ── Menu definitions ───────────────────────────────────────────────────

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
      { label: 'Toggle Linked Scrolling', commandId: 'toggle_linked_scroll' },
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
    ],
  },
  {
    title: 'Help',
    mnemonic: 'H',
    items: [
      { label: 'Keyboard Shortcuts', commandId: 'open_hotkeys_config' },
      { divider: true, label: '' },
      { label: 'About ledit', commandId: 'about' },
    ],
  },
];

// ── Helpers ──────────────────────────────────────────────────────────

/** Get actionable (non-divider, non-disabled) items from a menu. */
function getActionableItems(menu: MenuDef): MenuButtonItem[] {
  return menu.items.filter((i) => !i.divider && !i.disabled);
}

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

function getToggleState(commandId: string | undefined): boolean {
  if (!commandId) return false;
  if (commandId === 'toggle_minimap') {
    try {
      return localStorage.getItem('editor:minimap-enabled') === 'true';
    } catch {
      return false;
    }
  }
  return false;
}

// ── Component ────────────────────────────────────────────────────────

function MenuBar(): JSX.Element | null {
  const { hotkeyForCommand } = useHotkeys();
  const [activeMenuIndex, setActiveMenuIndex] = useState<number | null>(null);
  const [activeItemIndex, setActiveItemIndex] = useState<number | null>(null);
  const [showMnemonics, setShowMnemonics] = useState(false);
  const menuTitleRefs = useRef<(HTMLButtonElement | null)[]>([]);

  // ── Alt key: show/hide mnemonics + open menus via Alt+letter ─────

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Alt') {
        if (activeMenuIndex !== null) {
          // Alt while a menu is open → close it
          e.preventDefault();
          setActiveMenuIndex(null);
          setActiveItemIndex(null);
        }
      } else if (e.altKey && !e.ctrlKey && !e.metaKey) {
        // Check for Alt+mnemonic to open a menu
        const key = e.key.toLowerCase();
        const menuIndex = MENUS.findIndex((m) => m.mnemonic.toLowerCase() === key);
        if (menuIndex !== -1) {
          e.preventDefault();
          e.stopPropagation();
          setShowMnemonics(true);
          // If the same menu is already open, close it; otherwise open it
          setActiveMenuIndex((prev) => (prev === menuIndex ? null : menuIndex));
          setActiveItemIndex(0);
        } else if (activeMenuIndex !== null) {
          // A menu dropdown is open but this Alt+key doesn't match a mnemonic.
          // Swallow the event so HotkeyContext doesn't fire a command (e.g. Alt+Z
          // would toggle word wrap behind the open menu).
          e.preventDefault();
          e.stopPropagation();
        }
      }
    };

    const handleKeyUp = (e: KeyboardEvent) => {
      if (e.key === 'Alt') {
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

  // ── Keyboard navigation while a menu is open ─────────────────────

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
          e.preventDefault();
          setActiveMenuIndex(null);
          setActiveItemIndex(null);
          break;
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [activeMenuIndex]);

  // ── Execute a menu item by menu index + actionable index ─────────

  const executeItem = useCallback((menuIndex: number, actionableIndex: number) => {
    const menu = MENUS[menuIndex];
    const item = getActionableItems(menu)[actionableIndex];
    if (!item) return;

    if (item.commandId === 'about') {
      setActiveMenuIndex(null);
      setActiveItemIndex(null);
      window.alert('ledit WebUI\nVersion 1.0.0\n\nA modern, keyboard-accessible code editor.');
      return;
    }

    if (item.commandId) {
      window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: item.commandId } }));
    }

    setActiveMenuIndex(null);
    setActiveItemIndex(null);
  }, []);

  // ── Execute the currently selected item ───────────────────────────

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

  // ── Click handlers ────────────────────────────────────────────────

  const handleTitleClick = useCallback((index: number) => {
    setActiveMenuIndex((prev) => (prev === index ? null : index));
    setActiveItemIndex(0);
  }, []);

  const handleItemAction = useCallback((menuIndex: number, actionableIndex: number) => {
    executeItem(menuIndex, actionableIndex);
  }, [executeItem]);

  const handleTitleMouseEnter = useCallback(
    (index: number) => {
      if (activeMenuIndex !== null) {
        // A menu is already open → switch to hovered menu
        setActiveMenuIndex(index);
        setActiveItemIndex(0);
      }
    },
    [activeMenuIndex],
  );

  // ── Close menus on outside click ──────────────────────────────────

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      const target = e.target as Node;
      // Check if click was inside any menu title button
      const clickedInsideMenu = menuTitleRefs.current.some((ref) => ref?.contains(target));
      if (!clickedInsideMenu) {
        setActiveMenuIndex(null);
        setActiveItemIndex(null);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  // ── Build HTML with underlined mnemonic ───────────────────────────

  const buildLabelHtml = useCallback(
    (label: string): string => {
      if (!showMnemonics) return escapeHtml(label);
      // Underline the first character as the mnemonic
      const escaped = escapeHtml(label);
      return `<u>${escaped.charAt(0)}</u>${escaped.slice(1)}`;
    },
    [showMnemonics],
  );

  // ── Render ────────────────────────────────────────────────────────

  return (
    <>
      {/* Menu bar strip */}
      <div className="menu-bar" role="menubar">
        {MENUS.map((menu, index) => {
          const isActive = activeMenuIndex === index;
          return (
            <button
              key={menu.mnemonic}
              ref={(el) => { menuTitleRefs.current[index] = el; }}
              className={`menu-bar-title ${isActive ? 'menu-bar-title--active' : ''}`}
              onClick={() => handleTitleClick(index)}
              onMouseEnter={() => handleTitleMouseEnter(index)}
              aria-expanded={isActive}
              aria-haspopup="true"
              role="menuitem"
            >
              <span dangerouslySetInnerHTML={{ __html: buildLabelHtml(menu.title) }} />
            </button>
          );
        })}
      </div>

      {/* Dropdown */}
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

// ── Dropdown sub-component ──────────────────────────────────────────

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
  const dropdownRef = useRef<HTMLDivElement>(null);
  let actionableCounter = 0;

  // Position the dropdown below the anchor (useLayoutEffect to avoid flicker)
  useLayoutEffect(() => {
    if (!dropdownRef.current || !anchorRef) return;

    const position = () => {
      if (!dropdownRef.current || !anchorRef) return;

      const anchorRect = anchorRef.getBoundingClientRect();
      const el = dropdownRef.current;

      let left = anchorRect.left;
      let top = anchorRect.bottom + 2;

      // Clamp to viewport
      const vw = window.innerWidth;
      const vh = window.innerHeight;
      const elRect = el.getBoundingClientRect();

      if (elRect.right > vw) {
        left = Math.max(2, vw - elRect.width - 4);
      }
      if (elRect.bottom > vh) {
        top = anchorRect.top - elRect.height - 2;
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
    <div ref={dropdownRef} className="menu-bar-dropdown" role="menu" aria-label={`${menuDef.title} menu`}>
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
            className={`context-menu-item ${activeItemIndex === currentActionableIndex ? 'selected' : ''}`}
            onClick={() => onItemAction(menuIndex, currentActionableIndex)}
            onMouseEnter={() => onItemHover(currentActionableIndex)}
            role="menuitem"
          >
            <span className="menu-item-label">
              {item.isToggle && <span className="menu-item-check">{isChecked ? '✓' : ''}</span>}
              <span dangerouslySetInnerHTML={{ __html: buildItemLabelHtml(item.label, showMnemonics) }} />
            </span>
            {shortcut && <span className="menu-item-shortcut">{shortcut}</span>}
          </button>
        );
      })}
    </div>
  );
}

function buildItemLabelHtml(label: string, showMnemonics: boolean): string {
  const escaped = escapeHtml(label);
  if (!showMnemonics) return escaped;
  return `<u>${escaped.charAt(0)}</u>${escaped.slice(1)}`;
}

export default MenuBar;
