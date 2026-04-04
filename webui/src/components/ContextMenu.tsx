import React, { useEffect, useRef, useLayoutEffect } from 'react';
import { createPortal } from 'react-dom';
import './ContextMenu.css';

interface ContextMenuProps {
  isOpen: boolean;
  x: number;
  y: number;
  onClose: () => void;
  children: React.ReactNode;
  className?: string;
  zIndex?: number;
}

/**
 * A generic, controlled context menu component.
 *
 * Renders children inside a portal on `document.body` at the given (x, y)
 * position. Handles outside-click dismiss, Escape key, window blur, scroll
 * dismiss, and viewport boundary clamping.
 *
 * Consumers compose their own items using `.context-menu-item` (buttons)
 * and `.context-menu-divider` elements.
 */
const ContextMenu: React.FC<ContextMenuProps> = ({ isOpen, x, y, onClose, children, className, zIndex = 1400 }) => {
  const menuRef = useRef<HTMLDivElement>(null);
  const attachedRef = useRef(false);

  // ── Global close listeners (with rAF race-condition guard) ─

  useEffect(() => {
    if (!isOpen) {
      attachedRef.current = false;
      return;
    }

    const handleClickOutside = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        onClose();
      }
    };

    const handleScroll = () => {
      onClose();
    };

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
      }
    };

    const timer = requestAnimationFrame(() => {
      if (attachedRef.current) return;
      attachedRef.current = true;
      document.addEventListener('mousedown', handleClickOutside);
      window.addEventListener('scroll', handleScroll, true);
      document.addEventListener('keydown', handleKeyDown);
      window.addEventListener('blur', onClose);
    });

    return () => {
      cancelAnimationFrame(timer);
      if (attachedRef.current) {
        attachedRef.current = false;
        document.removeEventListener('mousedown', handleClickOutside);
        window.removeEventListener('scroll', handleScroll, true);
        document.removeEventListener('keydown', handleKeyDown);
        window.removeEventListener('blur', onClose);
      }
    };
  }, [isOpen, onClose]);

  // ── Viewport boundary clamping ────────────────────────────

  useLayoutEffect(() => {
    if (!isOpen || !menuRef.current) return;
    const el = menuRef.current;

    // Reset any inline styles from a previous open before computing fresh position
    el.style.left = '';
    el.style.top = '';

    const rect = el.getBoundingClientRect();
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const pad = 8;

    if (rect.right > vw) {
      el.style.left = `${Math.max(pad, vw - rect.width - pad)}px`;
    }
    if (rect.bottom > vh) {
      el.style.top = `${Math.max(pad, vh - rect.height - pad)}px`;
    }
  }, [isOpen, x, y]);

  if (!isOpen) return null;

  const classNames = ['context-menu'];
  if (className) classNames.push(className);

  return createPortal(
    <div
      ref={menuRef}
      className={classNames.join(' ')}
      style={{ left: x, top: y, zIndex }}
      onClick={(e) => e.stopPropagation()}
    >
      {children}
    </div>,
    document.body,
  );
};

export default ContextMenu;
