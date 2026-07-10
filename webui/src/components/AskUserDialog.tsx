import { useEffect, useCallback, useMemo, useRef, useState } from 'react';
import { CheckSquare, Square, Circle } from 'lucide-react';
import ReactMarkdown from 'react-markdown';
import remarkBreaks from 'remark-breaks';
import remarkGfm from 'remark-gfm';
import './ThemedDialog.css';

export interface AskUserDialogOption {
  label: string;
  value?: string;
  description?: string;
}

export interface AskUserDialogProps {
  requestId: string;
  question: string;
  header?: string;
  options?: AskUserDialogOption[];
  multiSelect?: boolean;
  defaultValue?: string;
  onRespond: (requestId: string, response: string) => void;
}

const optionValue = (opt: AskUserDialogOption): string =>
  opt.value && opt.value.trim().length > 0 ? opt.value : opt.label;

function AskUserDialog({
  requestId,
  question,
  header,
  options,
  multiSelect,
  defaultValue,
  onRespond,
}: AskUserDialogProps): JSX.Element {
  const hasOptions = Array.isArray(options) && options.length > 0;
  const isMulti = Boolean(multiSelect) && hasOptions;

  const initialResponse = useMemo(() => {
    if (hasOptions) return '';
    return defaultValue ?? '';
  }, [hasOptions, defaultValue]);

  const initialSelection = useMemo(() => {
    if (!hasOptions || !defaultValue) return new Set<string>();
    const values = defaultValue
      .split(',')
      .map((v) => v.trim())
      .filter(Boolean);
    return new Set<string>(values);
  }, [hasOptions, defaultValue]);

  const [response, setResponse] = useState(initialResponse);
  const [selected, setSelected] = useState<Set<string>>(initialSelection);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const firstOptionRef = useRef<HTMLButtonElement>(null);

  const buildSelectionResponse = useCallback((set: Set<string>): string => Array.from(set).join(','), []);

  const submitSingleOption = useCallback(
    (opt: AskUserDialogOption) => {
      const value = optionValue(opt);
      onRespond(requestId, value);
    },
    [requestId, onRespond],
  );

  const handleSubmit = useCallback(() => {
    if (hasOptions && isMulti) {
      const trimmed = buildSelectionResponse(selected);
      if (trimmed.length === 0) return;
      onRespond(requestId, trimmed);
      return;
    }
    if (hasOptions && !isMulti) {
      // Single-select expects an explicit click on an option, but if a
      // default is present and the user just hits Enter, honor it.
      if (defaultValue) {
        onRespond(requestId, defaultValue);
      }
      return;
    }
    const trimmedResponse = response.trim();
    if (trimmedResponse.length === 0) {
      if (defaultValue) {
        onRespond(requestId, defaultValue);
      }
      return;
    }
    onRespond(requestId, trimmedResponse);
  }, [requestId, response, onRespond, hasOptions, isMulti, selected, defaultValue, buildSelectionResponse]);

  const toggleOption = useCallback((opt: AskUserDialogOption) => {
    const value = optionValue(opt);
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(value)) {
        next.delete(value);
      } else {
        next.add(value);
      }
      return next;
    });
  }, []);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        // Cannot dismiss via Escape — user MUST respond
        e.preventDefault();
        return;
      }
      if (e.key === 'Enter') {
        if (e.metaKey || e.ctrlKey || e.shiftKey) {
          return;
        }
        // For freeform textarea, plain Enter inserts newline. Submit on
        // Cmd/Ctrl+Enter only when the textarea is focused.
        if (!hasOptions && document.activeElement === textareaRef.current) {
          return;
        }
        e.preventDefault();
        handleSubmit();
      }
    },
    [handleSubmit, hasOptions],
  );

  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown);
    document.body.style.overflow = 'hidden';
    const timer = setTimeout(() => {
      if (hasOptions) {
        firstOptionRef.current?.focus();
      } else {
        textareaRef.current?.focus();
      }
    }, 60);

    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      document.body.style.overflow = '';
      clearTimeout(timer);
    };
  }, [handleKeyDown, hasOptions]);

  const submitDisabled = isMulti
    ? selected.size === 0
    : hasOptions
      ? !defaultValue
      : response.trim().length === 0 && !defaultValue;

  return (
    <div className="ask-user-overlay" role="dialog" aria-modal="true" aria-label="User input required">
      <div className="ask-user-card" onClick={(e) => e.stopPropagation()}>
        <div className="ask-user-accent-bar" />

        <div className="ask-user-header">
          <span className="ask-user-icon" aria-hidden="true">
            ?
          </span>
          <div className="ask-user-heading-stack">
            {header && <span className="ask-user-chip">{header}</span>}
            <h2 className="ask-user-title">Question</h2>
          </div>
        </div>

        <div className="ask-user-body">
          <div className="ask-user-question-block">
            <ReactMarkdown
              remarkPlugins={[remarkGfm, remarkBreaks]}
              components={{
                p: ({ children }) => <p className="ask-user-question-text">{children}</p>,
                code: ({ className, children, ...props }) => (
                  <code className={className ? className : 'ask-user-inline-code'} {...props}>
                    {children}
                  </code>
                ),
                a: ({ children, ...props }) => (
                  <a {...props} target="_blank" rel="noopener noreferrer">
                    {children}
                  </a>
                ),
              }}
            >
              {question}
            </ReactMarkdown>
          </div>

          {hasOptions ? (
            <div
              className={`ask-user-options ${isMulti ? 'ask-user-options--multi' : 'ask-user-options--single'}`}
              role={isMulti ? 'group' : 'radiogroup'}
              aria-label="Available responses"
            >
              {options!.map((opt, idx) => {
                const value = optionValue(opt);
                const isSelected = isMulti ? selected.has(value) : defaultValue === value;
                const ariaProps = isMulti
                  ? { 'aria-pressed': isSelected }
                  : { 'aria-checked': isSelected, role: 'radio' as const };
                return (
                  <button
                    key={`${value}-${idx}`}
                    type="button"
                    ref={idx === 0 ? firstOptionRef : undefined}
                    className={`ask-user-option ${isSelected ? 'ask-user-option--selected' : ''}`}
                    onClick={() => (isMulti ? toggleOption(opt) : submitSingleOption(opt))}
                    {...ariaProps}
                  >
                    <span className="ask-user-option-marker" aria-hidden="true">
                      {isMulti ? (isSelected ? <CheckSquare size={16} /> : <Square size={16} />) : isSelected ? <Circle size={16} fill="currentColor" /> : <Circle size={16} />}
                    </span>
                    <span className="ask-user-option-body">
                      <span className="ask-user-option-label">{opt.label}</span>
                      {opt.description && <span className="ask-user-option-description">{opt.description}</span>}
                    </span>
                  </button>
                );
              })}
            </div>
          ) : (
            <div>
              <label htmlFor="ask-user-response">Your Response</label>
              <textarea
                id="ask-user-response"
                ref={textareaRef}
                value={response}
                onChange={(e) => setResponse(e.target.value)}
                placeholder={defaultValue ? `Default: ${defaultValue}` : 'Type your response here...'}
                rows={4}
              />
              <span className="ask-user-hint">Enter to submit · Shift+Enter for newline</span>
            </div>
          )}
        </div>

        <div className="ask-user-footer">
          {hasOptions && !isMulti ? (
            <span className="ask-user-footer-hint">Click an option to submit</span>
          ) : (
            <button
              type="button"
              className="ask-user-btn ask-user-btn--submit"
              onClick={handleSubmit}
              disabled={submitDisabled}
            >
              {isMulti && selected.size > 0 ? `Submit (${selected.size})` : 'Submit'}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

export default AskUserDialog;
