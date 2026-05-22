/**
 * MenuBar component for @sprout/ui
 *
 * A pure, props-driven menu bar with keyboard navigation, mnemonics,
 * and portal-rendered dropdowns. All data and actions come through props.
 */
import { useState, useEffect, useCallback, useRef, useLayoutEffect, useMemo } from 'react';
import { createPortal } from 'react-dom';
import './MenuBar.css';

/* ── Public types ─────────────────────────────────────────────────────── */

/** A single menu item (action, divider, or toggle). */
export interface MenuBarItem {
  /** Display label (not required for divider items) */
  label?: string;
  /** Command identifier sent to onCommandExecute */
  commandId?: string;
  /** Render a visual separator instead of an actionable item */
  divider?: boolean;
  /** Disable the item (greyed out, non-clickable) */
  disabled?: boolean;
  /** Render as a checkbox toggle */
  isToggle?: boolean;
}

/** A top-level menu group (e.g. "File", "Edit"). */
export interface MenuDefinition {
  /** Menu title shown on the bar */
  title: string;
  /** Single-character mnemonic for Alt+key access */
  mnemonic: string;
  /** Items in this menu */
  items: MenuBarItem[];
}

/** Props for the MenuBar component. */
export interface MenuBarProps {
  /** Array of menu definitions (File, Edit, View, …). */
  menus: MenuDefinition[];

  /** Called when the user selects a non-toggle menu item. */
  onCommandExecute: (commandId: string) => void;

  /**
   * Optional callback for toggle-type items (e.g. word wrap on/off).
   * If omitted, toggle items fall through to onCommandExecute.
   */
  onToggleCommand?: (commandId: string) => void;

  /**
   * Resolve a human-readable keyboard shortcut for a command id.
   * Return null / undefined if no shortcut is configured.
   */
  hotkeyForCommand?: (commandId: string) => string | null;

  /**
   * Return the checked state for a toggle item.
   * Called with the item's commandId.
   */
  getToggleState?: (commandId: string) => boolean;

  /**
   * Override the default mnemonic underline behaviour.
   * When true mnemonics are always underlined.
   */
  alwaysShowMnemonics?: boolean;
}

/* ── Internal helpers ─────────────────────────────────────────────────── */

const activeDropdownRef = { current: null as HTMLDivElement | null };

function getActionableItems(menu: MenuDefinition): MenuBarItem[] {
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

function buildMnemonicLabelHtml(label: string, showMnemonics: boolean): string {
  const escaped = escapeHtml(label);
  if (!showMnemonics) return escaped;
  return `<u>${escaped.charAt(0)}</u>${escaped.slice(1)}`;
}

/* ── Dropdown (rendered via portal) ───────────────────────────────────── */

interface MenuBarDropdownProps {
  menuDef: MenuDefinition;
  menuIndex: number;
  anchorRef: HTMLButtonElement | null;
  activeItemIndex: number | null;
  showMnemonics: boolean;
  hotkeyForCommand?: (commandId: string) => string | null;
  getToggleState?: (commandId: string) => boolean;
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
  getToggleState,
  onItemAction,
  onItemHover,
}: MenuBarDropdownProps): JSX.Element | null {
  const dropdownRef = useRef<HTMLDivElement | null>(null);

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

      // Pass 1: position below the anchor
      let left = anchorRect.left;
      let top = anchorRect.bottom + 2;

      el.style.left = `${left}px`;
      el.style.top = `${top}px`;

      // Pass 2: read the actual rect at the positioned location
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

  let actionableCounter = 0;
  return (
    <div
      ref={setDropdownEl}
      className="menu-bar-dropdown"
      role="menu"
      aria-label={`${menuDef.title} menu`}
      id="menu-bar-dropdown"
      aria-activedescendant={
        activeItemIndex !== null ? `menu-bar-item-${menuIndex}-${activeItemIndex}` : undefined
      }
    >
      {menuDef.items.map((item, rawIndex) => {
        if (item.divider) {
          return <div key={`d-${rawIndex}`} className="context-menu-divider" role="separator" />;
        }
        const currentActionableIndex = actionableCounter;
        actionableCounter++;
        const shortcut =
          item.commandId && hotkeyForCommand ? hotkeyForCommand(item.commandId) : null;
        const isChecked =
          item.isToggle && item.commandId && getToggleState
            ? getToggleState(item.commandId)
            : false;
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
              {item.isToggle && (
                <span className="menu-item-check">{isChecked ? '✓' : ''}</span>
              )}
              <span
                dangerouslySetInnerHTML={{
                  __html: buildMnemonicLabelHtml(
                    item.label ?? '',
                    showMnemonics,
                  ),
                }}
              />
            </span>
            {shortcut && <span className="menu-item-shortcut">{shortcut}</span>}
          </button>
        );
      })}
    </div>
  );
}

/* ── MenuBar ──────────────────────────────────────────────────────────── */

function MenuBar({
  menus,
  onCommandExecute,
  onToggleCommand,
  hotkeyForCommand,
  getToggleState,
  alwaysShowMnemonics = false,
}: MenuBarProps): JSX.Element {
  const [activeMenuIndex, setActiveMenuIndex] = useState<number | null>(null);
  const [activeItemIndex, setActiveItemIndex] = useState<number | null>(null);
  const [showMnemonics, setShowMnemonics] = useState(false);
  const menuTitleRefs = useRef<(HTMLButtonElement | null)[]>([]);

  useEffect(() => {
    return () => {
      activeDropdownRef.current = null;
    };
  }, []);

  /* ── Global keyboard shortcuts (F10 / Alt) ──────────────────────────── */
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
        const menuIndex = menus.findIndex((m: MenuDefinition) => m.mnemonic.toLowerCase() === key);
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
  }, [activeMenuIndex, menus]);

  /* ── Keyboard navigation when a menu is open ────────────────────────── */
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
          setActiveMenuIndex((prev) => (prev !== null ? (prev + 1) % menus.length : 0));
          setActiveItemIndex(0);
          break;
        case 'ArrowLeft':
          e.preventDefault();
          setActiveMenuIndex(
            (prev) => (prev !== null ? (prev - 1 + menus.length) % menus.length : menus.length - 1),
          );
          setActiveItemIndex(0);
          break;
        case 'ArrowDown':
          e.preventDefault();
          setActiveItemIndex((prev) => {
            const menuItems = getActionableItems(menus[activeMenuIndex]);
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
  }, [activeMenuIndex, menus]);

  /* ── Execute a menu item ────────────────────────────────────────────── */
  const executeItem = useCallback(
    (menuIndex: number, actionableIndex: number) => {
      const menu = menus[menuIndex];
      const item = getActionableItems(menu)[actionableIndex];
      if (!item || !item.commandId) return;

      if (item.isToggle && onToggleCommand) {
        onToggleCommand(item.commandId);
      } else {
        onCommandExecute(item.commandId);
      }

      setActiveMenuIndex(null);
      setActiveItemIndex(null);
    },
    [menus, onCommandExecute, onToggleCommand],
  );

  /* ── Enter / Space to confirm selection ─────────────────────────────── */
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

  /* ── Mouse handlers ─────────────────────────────────────────────────── */
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

  /* ── Click-outside to close ─────────────────────────────────────────── */
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      const target = e.target as Node;
      const clickedInsideMenu =
        menuTitleRefs.current.some((ref) => ref?.contains(target)) ||
        activeDropdownRef.current?.contains(target);
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
        {menus.map((menu: MenuDefinition, index: number) => {
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
              <span
                dangerouslySetInnerHTML={{
                  __html: buildMnemonicLabelHtml(menu.title, showMnemonics || alwaysShowMnemonics),
                }}
              />
            </button>
          );
        })}
      </div>
      {activeMenuIndex !== null &&
        createPortal(
          <MenuBarDropdown
            menuDef={menus[activeMenuIndex]}
            menuIndex={activeMenuIndex}
            anchorRef={menuTitleRefs.current[activeMenuIndex]}
            activeItemIndex={activeItemIndex}
            showMnemonics={showMnemonics || alwaysShowMnemonics}
            hotkeyForCommand={hotkeyForCommand}
            getToggleState={getToggleState}
            onItemAction={handleItemAction}
            onItemHover={setActiveItemIndex}
          />,
          document.body,
        )}
    </>
  );
}

export default MenuBar;
