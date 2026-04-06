import { useCallback, useEffect, useRef, useState } from 'react';
import type { KeyboardEvent, ReactElement } from 'react';
import ReactDOM from 'react-dom/client';
import './ThemedDialog.css';

/* ── Type helpers ────────────────────────────────────────────── */

type DialogType = 'info' | 'warning' | 'error' | 'success' | 'danger';

const ICON_BY_TYPE: Record<DialogType, string> = {
  info: 'ℹ',
  warning: '⚠',
  error: '✕',
  success: '✓',
  danger: '✕',
};

const DEFAULT_TITLE_BY_TYPE: Record<DialogType, string> = {
  info: 'Info',
  warning: 'Warning',
  error: 'Error',
  success: 'Success',
  danger: 'Confirm Action',
};

/* ── Scroll lock ─────────────────────────────────────────────── */

let scrollLockCount = 0;

function lockScroll() {
  if (scrollLockCount === 0) {
    document.body.style.overflow = 'hidden';
  }
  scrollLockCount += 1;
}

function unlockScroll() {
  scrollLockCount = Math.max(0, scrollLockCount - 1);
  if (scrollLockCount === 0) {
    document.body.style.overflow = '';
  }
}

/* ── Internal alert dialog component ─────────────────────────── */

interface AlertDialogProps {
  title: string;
  message: string;
  type: DialogType;
  onClose: () => void;
}

function AlertDialog({ title, message, type, onClose }: AlertDialogProps): JSX.Element {
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape' || e.key === 'Enter') {
        e.preventDefault();
        onClose();
      }
    },
    [onClose],
  );

  useEffect(() => {
    lockScroll();
    return () => unlockScroll();
  }, []);

  return (
    <div className="themed-dialog-overlay" onClick={onClose} onKeyDown={handleKeyDown}>
      <div className="themed-dialog-card" onClick={(e) => e.stopPropagation()}>
        <div className={`themed-dialog-accent-bar themed-dialog-accent-bar--${type}`} />
        <div className="themed-dialog-header">
          <span className={`themed-dialog-icon themed-dialog-icon--${type}`}>{ICON_BY_TYPE[type]}</span>
          <h2 className="themed-dialog-title">{title}</h2>
        </div>
        <div className="themed-dialog-body">{message}</div>
        <div className="themed-dialog-footer">
          <button type="button" className="themed-dialog-btn themed-dialog-btn--primary" onClick={onClose} autoFocus>
            OK
          </button>
        </div>
      </div>
    </div>
  );
};

/* ── Internal confirm dialog component ───────────────────────── */

interface ConfirmDialogProps {
  title: string;
  message: string;
  type: DialogType;
  confirmLabel: string;
  cancelLabel: string;
  onConfirm: () => void;
  onCancel: () => void;
}

function ConfirmDialog({
  title,
  message,
  type,
  confirmLabel,
  cancelLabel,
  onConfirm,
  onCancel,
}: ConfirmDialogProps): JSX.Element {
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onCancel();
      } else if (e.key === 'Enter') {
        e.preventDefault();
        onConfirm();
      }
    },
    [onConfirm, onCancel],
  );

  useEffect(() => {
    lockScroll();
    return () => unlockScroll();
  }, []);

  // For danger type, overlay click does NOT dismiss
  const handleOverlayClick = type === 'danger' ? undefined : onCancel;

  const confirmBtnClass =
    type === 'danger'
      ? 'themed-dialog-btn themed-dialog-btn--danger'
      : type === 'warning'
        ? 'themed-dialog-btn themed-dialog-btn--primary'
        : 'themed-dialog-btn themed-dialog-btn--primary';

  return (
    <div className="themed-dialog-overlay" onClick={handleOverlayClick} onKeyDown={handleKeyDown}>
      <div className="themed-dialog-card" onClick={(e) => e.stopPropagation()}>
        <div className={`themed-dialog-accent-bar themed-dialog-accent-bar--${type}`} />
        <div className="themed-dialog-header">
          <span className={`themed-dialog-icon themed-dialog-icon--${type}`}>{ICON_BY_TYPE[type]}</span>
          <h2 className="themed-dialog-title">{title}</h2>
        </div>
        <div className="themed-dialog-body">{message}</div>
        <div className="themed-dialog-footer">
          {type !== 'danger' && (
            <button type="button" className="themed-dialog-btn" onClick={onCancel}>
              {cancelLabel}
            </button>
          )}
          <button type="button" className={confirmBtnClass} onClick={onConfirm} autoFocus={type !== 'danger'}>
            {confirmLabel}
          </button>
          {type === 'danger' && (
            <button type="button" className="themed-dialog-btn" onClick={onCancel} autoFocus>
              {cancelLabel}
            </button>
          )}
        </div>
      </div>
    </div>
  );
};

/* ── Internal prompt dialog component ────────────────────────── */

interface PromptDialogProps {
  title: string;
  message: string;
  type: DialogType;
  defaultValue: string;
  placeholder: string;
  onSubmit: (value: string) => void;
  onCancel: () => void;
}

function PromptDialog({
  title,
  message,
  type,
  defaultValue,
  placeholder,
  onSubmit,
  onCancel,
}: PromptDialogProps): JSX.Element {
  const [value, setValue] = useState(defaultValue);
  const inputRef = useRef<HTMLInputElement>(null);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onCancel();
      } else if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        onSubmit(value);
      }
    },
    [onSubmit, onCancel, value],
  );

  useEffect(() => {
    lockScroll();
    return () => unlockScroll();
  }, []);

  useEffect(() => {
    // Focus the input after mount
    const timer = setTimeout(() => {
      if (inputRef.current) {
        inputRef.current.focus();
      }
    }, 50);
    return () => clearTimeout(timer);
  }, []);

  return (
    <div className="themed-dialog-overlay" onClick={onCancel} onKeyDown={handleKeyDown}>
      <div className="themed-dialog-card" onClick={(e) => e.stopPropagation()}>
        <div className={`themed-dialog-accent-bar themed-dialog-accent-bar--${type}`} />
        <div className="themed-dialog-header">
          <span className={`themed-dialog-icon themed-dialog-icon--${type}`}>{ICON_BY_TYPE[type]}</span>
          <h2 className="themed-dialog-title">{title}</h2>
        </div>
        <div className="themed-dialog-body">{message}</div>
        <div className="themed-dialog-input-row">
          <input
            ref={inputRef}
            type="text"
            className="themed-dialog-input"
            value={value}
            placeholder={placeholder}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                onSubmit(value);
              }
            }}
          />
        </div>
        <div className="themed-dialog-footer">
          <button type="button" className="themed-dialog-btn" onClick={onCancel}>
            Cancel
          </button>
          <button
            type="button"
            className="themed-dialog-btn themed-dialog-btn--primary"
            onClick={() => onSubmit(value)}
          >
            Submit
          </button>
        </div>
      </div>
    </div>
  );
};

/* ── Portal helpers ──────────────────────────────────────────── */

/**
 * Mount a React element into a temporary container appended to document.body,
 * return a cleanup function that unmounts and removes the container.
 */
function mountToBody(element: ReactElement): () => void {
  const container = document.createElement('div');
  container.setAttribute('data-themed-dialog-portal', '');
  document.body.appendChild(container);

  const root = ReactDOM.createRoot(container);
  root.render(element);

  return () => {
    root.unmount();
    if (container.parentNode) {
      container.parentNode.removeChild(container);
    }
  };
}

/* ── Public API ──────────────────────────────────────────────── */

/**
 * Show a themed alert dialog. Returns a promise that resolves when the user
 * dismisses it (clicks OK, presses Enter or Escape).
 */
export async function showThemedAlert(
  message: string,
  options?: { title?: string; type?: 'info' | 'warning' | 'error' | 'success' },
): Promise<void> {
  return new Promise<void>((resolve) => {
    const type: DialogType = options?.type || 'info';
    const title = options?.title || DEFAULT_TITLE_BY_TYPE[type];

    let cleanup: (() => void) | null = null;

    const handleClose = () => {
      // Use microtask so React finishes its state update first
      queueMicrotask(() => {
        if (cleanup) {
          cleanup();
          cleanup = null;
        }
        resolve();
      });
    };

    cleanup = mountToBody(<AlertDialog title={title} message={message} type={type} onClose={handleClose} />);
  });
}

/**
 * Show a themed confirm dialog. Resolves to `true` when the user confirms,
 * `false` when they cancel.
 */
export async function showThemedConfirm(
  message: string,
  options?: {
    title?: string;
    confirmLabel?: string;
    cancelLabel?: string;
    type?: 'info' | 'warning' | 'error' | 'danger';
  },
): Promise<boolean> {
  return new Promise<boolean>((resolve) => {
    const type: DialogType = options?.type || 'info';
    const title = options?.title || DEFAULT_TITLE_BY_TYPE[type];
    const confirmLabel = options?.confirmLabel || 'Confirm';
    const cancelLabel = options?.cancelLabel || 'Cancel';

    let cleanup: (() => void) | null = null;

    const handleConfirm = () => {
      queueMicrotask(() => {
        if (cleanup) {
          cleanup();
          cleanup = null;
        }
        resolve(true);
      });
    };

    const handleCancel = () => {
      queueMicrotask(() => {
        if (cleanup) {
          cleanup();
          cleanup = null;
        }
        resolve(false);
      });
    };

    cleanup = mountToBody(
      <ConfirmDialog
        title={title}
        message={message}
        type={type}
        confirmLabel={confirmLabel}
        cancelLabel={cancelLabel}
        onConfirm={handleConfirm}
        onCancel={handleCancel}
      />,
    );
  });
}

/**
 * Show a themed input prompt dialog. Resolves to the entered string,
 * or `null` if the user cancels.
 */
export async function showThemedPrompt(
  message: string,
  options?: {
    title?: string;
    defaultValue?: string;
    placeholder?: string;
    type?: 'info' | 'warning';
  },
): Promise<string | null> {
  return new Promise<string | null>((resolve) => {
    const type: DialogType = options?.type || 'info';
    const title = options?.title || DEFAULT_TITLE_BY_TYPE[type];
    const defaultValue = options?.defaultValue || '';
    const placeholder = options?.placeholder || '';

    let cleanup: (() => void) | null = null;

    const handleSubmit = (value: string) => {
      queueMicrotask(() => {
        if (cleanup) {
          cleanup();
          cleanup = null;
        }
        resolve(value);
      });
    };

    const handleCancel = () => {
      queueMicrotask(() => {
        if (cleanup) {
          cleanup();
          cleanup = null;
        }
        resolve(null);
      });
    };

    cleanup = mountToBody(
      <PromptDialog
        title={title}
        message={message}
        type={type}
        defaultValue={defaultValue}
        placeholder={placeholder}
        onSubmit={handleSubmit}
        onCancel={handleCancel}
      />,
    );
  });
}
