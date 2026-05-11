import React, { createContext, useContext, useState, useCallback, type ReactNode } from 'react';
import type { WhitespaceRenderingMode } from '../extensions/whitespaceRendering';

// ---------------------------------------------------------------------------
// Pane configuration constants
// ---------------------------------------------------------------------------

export const MAX_PANES = 6;
export const DEFAULT_MAX_PANES = 6;

// ---------------------------------------------------------------------------
// Settings Context Interface
// ---------------------------------------------------------------------------

interface EditorSettingsContextValue {
  isAutoSaveEnabled: boolean;
  setAutoSaveEnabled: (enabled: boolean) => void;
  whitespaceRenderingMode: WhitespaceRenderingMode;
  setWhitespaceRenderingMode: (mode: WhitespaceRenderingMode) => void;
  autoSaveInterval: number; // milliseconds
  isFormatOnSaveEnabled: boolean;
  setFormatOnSaveEnabled: (enabled: boolean) => void;
  maxPanes: number; // Configurable max panes for UI (capped at MAX_PANES)
  setMaxPanes: (n: number) => void;
}

const EditorSettingsContext = createContext<EditorSettingsContextValue | null>(null);

export const useEditorSettings = () => {
  const context = useContext(EditorSettingsContext);
  if (!context) {
    throw new Error('useEditorSettings must be used within EditorSettingsProvider');
  }
  return context;
};

interface EditorSettingsProviderProps {
  children: ReactNode;
}

export const EditorSettingsProvider: React.FC<EditorSettingsProviderProps> = ({ children }) => {
  const [isAutoSaveEnabled, setIsAutoSaveEnabledState] = useState(true);
  const setAutoSaveEnabled = useCallback((enabled: boolean) => setIsAutoSaveEnabledState(enabled), []);

  const [whitespaceRenderingMode, setWhitespaceRenderingModeState] = useState<WhitespaceRenderingMode>(() => {
    try {
      const stored = localStorage.getItem('editor:whitespace-rendering');
      if (stored === 'boundary' || stored === 'all' || stored === 'none') return stored;
      return 'none';
    } catch (err) {
      return 'none';
    }
  });

  const setWhitespaceRenderingMode = useCallback((mode: WhitespaceRenderingMode) => {
    setWhitespaceRenderingModeState(mode);
    try {
      localStorage.setItem('editor:whitespace-rendering', mode);
    } catch (err) {
      // Ignore localStorage errors
    }
  }, []);

  const [isFormatOnSaveEnabled, setIsFormatOnSaveEnabledState] = useState(() => {
    try {
      const stored = localStorage.getItem('editor.format-on-save');
      return stored === 'true';
    } catch (err) {
      return false;
    }
  });

  const setFormatOnSaveEnabled = useCallback((enabled: boolean) => {
    setIsFormatOnSaveEnabledState(enabled);
    try {
      localStorage.setItem('editor.format-on-save', String(enabled));
    } catch (err) {
      // Ignore localStorage errors
    }
  }, []);

  const [autoSaveInterval] = useState(30000); // 30 seconds

  const [maxPanes, setMaxPanesState] = useState<number>(() => {
    try {
      const stored = localStorage.getItem('editor.max-panes');
      if (stored) {
        const parsed = parseInt(stored, 10);
        // Clamp to range [2, MAX_PANES]
        if (!isNaN(parsed) && parsed >= 2 && parsed <= MAX_PANES) {
          return parsed;
        }
      }
      return DEFAULT_MAX_PANES;
    } catch (err) {
      return DEFAULT_MAX_PANES;
    }
  });

  const setMaxPanes = useCallback((n: number) => {
    // Clamp to range [2, MAX_PANES]
    const clamped = Math.max(2, Math.min(MAX_PANES, n));
    setMaxPanesState(clamped);
    try {
      localStorage.setItem('editor.max-panes', String(clamped));
    } catch (err) {
      // Ignore localStorage errors
    }
  }, []);

  const value = React.useMemo<EditorSettingsContextValue>(() => ({
    isAutoSaveEnabled,
    setAutoSaveEnabled,
    whitespaceRenderingMode,
    setWhitespaceRenderingMode,
    autoSaveInterval,
    isFormatOnSaveEnabled,
    setFormatOnSaveEnabled,
    maxPanes,
    setMaxPanes,
  }), [
    isAutoSaveEnabled,
    setAutoSaveEnabled,
    whitespaceRenderingMode,
    setWhitespaceRenderingMode,
    autoSaveInterval,
    isFormatOnSaveEnabled,
    setFormatOnSaveEnabled,
    maxPanes,
    setMaxPanes,
  ]);

  return (
    <EditorSettingsContext.Provider value={value}>
      {children}
    </EditorSettingsContext.Provider>
  );
};
