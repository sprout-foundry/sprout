import { useRef, useCallback, useMemo } from 'react';
import './Editor.css';

export interface EditorProps {
  value: string;
  onChange?: (value: string) => void;
  language?: string;
  readOnly?: boolean;
  lineNumbers?: boolean;
  onCursorChange?: (pos: { line: number; column: number }) => void;
  placeholder?: string;
  className?: string;
}

/**
 * A lightweight code editor wrapper using textarea with line number gutter.
 *
 * Since this is a UI library without CodeMirror dependency, this provides
 * a basic code editing experience with line numbers, monospace font,
 * and cursor position tracking.
 */
function Editor({
  value,
  onChange,
  language,
  readOnly = false,
  lineNumbers = true,
  onCursorChange,
  placeholder = 'Enter code here...',
  className,
}: EditorProps): JSX.Element {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const lineNumbersRef = useRef<HTMLDivElement>(null);

  // Count lines in value
  const lineCount = useMemo(() => {
    if (!value) return 1;
    return value.split('\n').length;
  }, [value]);

  // Sync scroll between textarea and line numbers
  const handleScroll = useCallback(() => {
    if (textareaRef.current && lineNumbersRef.current) {
      lineNumbersRef.current.scrollTop = textareaRef.current.scrollTop;
    }
  }, []);

  // Handle cursor position changes
  const handleCursorChange = useCallback(() => {
    if (!textareaRef.current) return;
    const textarea = textareaRef.current;
    const text = textarea.value.substring(0, textarea.selectionStart);
    const lines = text.split('\n');
    const line = lines.length - 1;
    const column = lines[lines.length - 1].length;

    onCursorChange?.({ line, column });
  }, [onCursorChange]);

  // Handle text input
  const handleChange = useCallback(
    (e: React.ChangeEvent<HTMLTextAreaElement>) => {
      onChange?.(e.target.value);
    },
    [onChange],
  );

  // Handle key press for Tab key support
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Tab' && !readOnly) {
        e.preventDefault();
        const textarea = textareaRef.current;
        if (!textarea) return;

        const start = textarea.selectionStart;
        const end = textarea.selectionEnd;
        const spaces = '  '; // 2 spaces for indentation

        const newValue = value.substring(0, start) + spaces + value.substring(end);
        onChange?.(newValue);

        // Move cursor after inserted spaces
        requestAnimationFrame(() => {
          textarea.selectionStart = textarea.selectionEnd = start + spaces.length;
          handleCursorChange();
        });
      }
    },
    [readOnly, value, onChange, handleCursorChange],
  );

  // Generate line numbers
  const lineNumbersContent = useMemo(() => {
    return Array.from({ length: lineCount }, (_, i) => i + 1).join('\n');
  }, [lineCount]);

  return (
    <div className={`editor-container ${className || ''}`}>
      {/* Language indicator */}
      {language && <div className="editor-language">{language}</div>}

      <div className="editor-wrapper">
        {/* Line numbers gutter */}
        {lineNumbers && (
          <div ref={lineNumbersRef} className="editor-line-numbers" aria-hidden="true">
            {lineNumbersContent}
          </div>
        )}

        {/* Textarea for editing */}
        <textarea
          ref={textareaRef}
          className="editor-textarea"
          value={value}
          onChange={handleChange}
          onScroll={handleScroll}
          onKeyUp={handleCursorChange}
          onClick={handleCursorChange}
          onKeyDown={handleKeyDown}
          placeholder={placeholder}
          readOnly={readOnly}
          spellCheck={false}
          aria-label={language ? `Code editor - ${language}` : 'Code editor'}
        />
      </div>
    </div>
  );
}

export default Editor;
