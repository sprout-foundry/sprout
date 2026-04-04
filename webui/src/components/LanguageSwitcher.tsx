import { useState, useRef, useEffect, useMemo, useCallback } from 'react';
import type { CSSProperties, FC, KeyboardEvent as ReactKeyboardEvent } from 'react';
import { createPortal } from 'react-dom';
import { Check, FileCode } from 'lucide-react';
import { allLanguageEntries } from '../extensions/languageRegistry';
import './LanguageSwitcher.css';

interface LanguageSwitcherProps {
  currentLanguageId: string | null;
  isAutoDetected: boolean;
  onLanguageChange: (languageId: string | null) => void;
}

/** Estimated total popup height (max-height list + search + footer + borders). */
const ESTIMATED_POPUP_HEIGHT = 350;

/**
 * A compact language mode switcher for the editor toolbar.
 *
 * Shows the current language name (or "Auto (Language)") and opens a
 * searchable popup listing all languages when clicked.  The popup is
 * rendered via a portal to avoid overflow clipping from parent containers.
 */
const LanguageSwitcher: FC<LanguageSwitcherProps> = ({ currentLanguageId, isAutoDetected, onLanguageChange }) => {
  const [isOpen, setIsOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const popupRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);

  // Resolve display label
  const displayName = useMemo(() => {
    if (!currentLanguageId) return 'Auto';
    const entry = allLanguageEntries.find((e) => e.id === currentLanguageId);
    if (!entry) return 'Auto';
    return isAutoDetected ? `Auto (${entry.name})` : entry.name;
  }, [currentLanguageId, isAutoDetected]);

  // Filtered list
  const items = useMemo(() => {
    if (!query.trim()) return allLanguageEntries;
    const q = query.toLowerCase();
    return allLanguageEntries.filter((e) => e.name.toLowerCase().includes(q) || e.id.toLowerCase().includes(q));
  }, [query]);

  // Keep selected index in bounds (conceptual list is [Auto-detect, ...items])
  useEffect(() => {
    setSelectedIndex((prev) => Math.min(prev, items.length));
  }, [items.length]);

  // Reset selected index when popup reopens
  useEffect(() => {
    if (isOpen) setSelectedIndex(0);
  }, [isOpen]);

  // Focus search when opened
  useEffect(() => {
    if (isOpen && searchRef.current) {
      searchRef.current.focus();
    }
  }, [isOpen]);

  // Close popup and return focus to the trigger button
  const closePopup = useCallback(() => {
    setIsOpen(false);
    buttonRef.current?.focus();
  }, []);

  // Close on outside clicks
  useEffect(() => {
    if (!isOpen) return;

    const handler = (e: MouseEvent) => {
      if (
        popupRef.current &&
        !popupRef.current.contains(e.target as Node) &&
        buttonRef.current &&
        !buttonRef.current.contains(e.target as Node)
      ) {
        closePopup();
      }
    };

    const timer = requestAnimationFrame(() => {
      document.addEventListener('mousedown', handler);
    });
    return () => {
      cancelAnimationFrame(timer);
      document.removeEventListener('mousedown', handler);
    };
  }, [isOpen, closePopup]);

  // Close on scroll
  useEffect(() => {
    if (!isOpen) return;
    const handler = () => closePopup();
    window.addEventListener('scroll', handler, true);
    return () => window.removeEventListener('scroll', handler, true);
  }, [isOpen, closePopup]);

  // Close on Escape
  useEffect(() => {
    if (!isOpen) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        closePopup();
      }
    };
    document.addEventListener('keydown', handler, true);
    return () => document.removeEventListener('keydown', handler, true);
  }, [isOpen, closePopup]);

  // Determine the index of "Auto-detect" in the filtered list (always 0 conceptually)
  const autoDetectIndex = 0;

  const handleSelect = (languageId: string | null) => {
    onLanguageChange(languageId);
    closePopup();
    setQuery('');
  };

  // Scroll the selected item into view after index changes
  useEffect(() => {
    if (!isOpen || !popupRef.current) return;
    requestAnimationFrame(() => {
      const selected = popupRef.current?.querySelector('.language-switcher-item.selected');
      (selected as HTMLElement)?.scrollIntoView({ block: 'nearest' });
    });
  }, [selectedIndex, isOpen]);

  const handleKeyDown = (e: ReactKeyboardEvent) => {
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        setSelectedIndex((prev) => Math.min(prev + 1, items.length));
        break;
      case 'ArrowUp':
        e.preventDefault();
        setSelectedIndex((prev) => Math.max(prev - 1, 0));
        break;
      case 'Home':
        e.preventDefault();
        setSelectedIndex(0);
        break;
      case 'End':
        e.preventDefault();
        setSelectedIndex(items.length);
        break;
      case 'Enter':
        e.preventDefault();
        if (selectedIndex === autoDetectIndex) {
          handleSelect(null);
        } else {
          const item = items[selectedIndex - 1]; // -1 because auto-detect is at 0
          if (item) handleSelect(item.id);
        }
        break;
    }
  };

  // Compute popup position relative to the button
  const [popupStyle, setPopupStyle] = useState<CSSProperties>({});
  useEffect(() => {
    if (!isOpen || !buttonRef.current) return;

    const positionPopup = () => {
      const rect = buttonRef.current?.getBoundingClientRect();
      if (!rect) return;
      const viewportWidth = window.innerWidth;
      const viewportHeight = window.innerHeight;
      const preferredWidth = 240;
      const leftPos = Math.max(8, Math.min(rect.left, viewportWidth - preferredWidth - 8));

      const wouldOverflowBelow = rect.bottom + ESTIMATED_POPUP_HEIGHT + 4 > viewportHeight;

      if (wouldOverflowBelow) {
        setPopupStyle({
          position: 'fixed',
          bottom: viewportHeight - rect.top + 4,
          left: leftPos,
          width: preferredWidth,
        });
      } else {
        setPopupStyle({
          position: 'fixed',
          top: rect.bottom + 4,
          left: leftPos,
          width: preferredWidth,
        });
      }
    };

    positionPopup();
    window.addEventListener('resize', positionPopup);
    return () => window.removeEventListener('resize', positionPopup);
  }, [isOpen]);

  return (
    <>
      <button
        ref={buttonRef}
        className="language-switcher-button"
        onClick={() => setIsOpen((prev) => !prev)}
        title={`Language: ${displayName} — click to change`}
        aria-haspopup="listbox"
        aria-expanded={isOpen}
        aria-label={`Language: ${displayName} — click to change`}
        data-testid="language-switcher-button"
        data-language-id={currentLanguageId ?? 'auto'}
        data-auto-detected={String(isAutoDetected)}
      >
        <FileCode size={14} />
        <span className="language-switcher-label">{displayName}</span>
      </button>

      {isOpen &&
        createPortal(
          <div
            className="language-switcher-popup"
            style={popupStyle}
            ref={popupRef}
            role="listbox"
            aria-label="Select language mode"
            data-testid="language-switcher-popup"
          >
            <div className="language-switcher-search">
              <input
                ref={searchRef}
                type="text"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder="Filter languages..."
                className="language-switcher-search-input"
              />
            </div>

            <div className="language-switcher-list">
              {/* Auto-detect option */}
              <div
                className={`language-switcher-item ${selectedIndex === autoDetectIndex ? 'selected' : ''} ${currentLanguageId == null || isAutoDetected ? 'active' : ''}`}
                onMouseEnter={() => setSelectedIndex(autoDetectIndex)}
                onClick={() => handleSelect(null)}
                role="option"
                aria-selected={selectedIndex === autoDetectIndex}
              >
                <span className="language-switcher-item-name">Auto-detect</span>
                {(currentLanguageId == null || isAutoDetected) && (
                  <Check size={14} className="language-switcher-check" />
                )}
              </div>

              {items.map((entry, i) => {
                const listIndex = i + 1; // +1 because auto-detect is at 0
                const isActive = entry.id === currentLanguageId && !isAutoDetected;
                return (
                  <div
                    key={entry.id}
                    className={`language-switcher-item ${selectedIndex === listIndex ? 'selected' : ''} ${isActive ? 'active' : ''}`}
                    onMouseEnter={() => setSelectedIndex(listIndex)}
                    onClick={() => handleSelect(entry.id)}
                    role="option"
                    aria-selected={selectedIndex === listIndex}
                  >
                    <span className="language-switcher-item-name">{entry.name}</span>
                    {isActive && <Check size={14} className="language-switcher-check" />}
                  </div>
                );
              })}

              {items.length === 0 && query.trim() && (
                <div className="language-switcher-no-results">No matching languages</div>
              )}
            </div>

            <div className="language-switcher-footer">↑↓ Navigate · Enter Select · Esc Close</div>
          </div>,
          document.body,
        )}
    </>
  );
};

export default LanguageSwitcher;
