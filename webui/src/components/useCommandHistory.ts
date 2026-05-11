import { useState, useCallback, useEffect, useRef } from 'react';
import type { useLog } from '../utils/log';
import { ApiService } from '../services/api';
import {
  type CommandHistoryState,
  dedupeCommands,
  loadCommandHistory,
  persistCommandHistory,
} from './command_input_history';

interface UseCommandHistoryOptions {
  log: ReturnType<typeof useLog>;
  draftValue: string;
  updateValue: (val: string, sel?: { start: number; end: number }) => void;
}

export function useCommandHistory({ log, draftValue, updateValue }: UseCommandHistoryOptions) {
  const apiService = useRef(ApiService.getInstance());
  const [history, setHistory] = useState<CommandHistoryState>({
    commands: [],
    index: -1,
    tempInput: '',
  });
  const [isHistoryMode, setIsHistoryMode] = useState(false);
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);

  const currentHistoryValue =
    isHistoryMode && history.index >= 0 ? (history.commands[history.commands.length - 1 - history.index] ?? '') : null;

  // Load history from localStorage and terminal on mount
  const loadHistory = useCallback(async () => {
    setIsLoadingHistory(true);
    try {
      const commands = await loadCommandHistory(apiService.current);
      setHistory((prev) => ({
        ...prev,
        commands,
      }));
    } catch (error) {
      log.warn('Failed to load command history', { title: 'Command History' });
    } finally {
      setIsLoadingHistory(false);
    }
  }, [log]);

  const saveToHistory = useCallback(async (command: string) => {
    if (!command.trim()) return;
    const trimmedCommand = command.trim();
    // Update local state AND persist to localStorage synchronously
    setHistory((prev) => {
      const next = dedupeCommands([...prev.commands, trimmedCommand]);
      persistCommandHistory(next);
      return { commands: next, index: -1, tempInput: '' };
    });
  }, []);

  const resetHistoryNavigation = useCallback(() => {
    setHistory((prev) => ({
      ...prev,
      index: -1,
      tempInput: '',
    }));
    setIsHistoryMode(false);
  }, []);

  const navigateHistory = (direction: number) => {
    if (history.commands.length === 0) return;

    let newIndex = history.index + direction;
    const currentInputValue = draftValue;

    if (newIndex < -1) {
      newIndex = -1;
    } else if (newIndex >= history.commands.length) {
      newIndex = history.commands.length - 1;
    }

    let newInputValue = '';

    if (newIndex === -1) {
      // Return to temp input
      newInputValue = history.tempInput;
      setIsHistoryMode(false);
    } else {
      // Navigate to history item
      newInputValue = history.commands[history.commands.length - 1 - newIndex];
      setIsHistoryMode(true);
    }

    setHistory((prev) => ({
      ...prev,
      index: newIndex,
      tempInput: history.index === -1 && !isHistoryMode ? currentInputValue : prev.tempInput,
    }));

    updateValue(newInputValue, { start: newInputValue.length, end: newInputValue.length });
  };

  // Reset history navigation when draftValue changes away from history item
  useEffect(() => {
    if (!isHistoryMode || currentHistoryValue === null) {
      return;
    }
    if (draftValue === currentHistoryValue) {
      return;
    }

    setHistory((prev) => ({
      ...prev,
      index: -1,
      tempInput: draftValue,
    }));
    setIsHistoryMode(false);
  }, [currentHistoryValue, draftValue, isHistoryMode]);

  return {
    history,
    isHistoryMode,
    isLoadingHistory,
    loadHistory,
    saveToHistory,
    resetHistoryNavigation,
    navigateHistory,
  };
}
