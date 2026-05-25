/**
 * AppStateContext - Application state management using React Context + useReducer.
 *
 * Replaces the useState-based useAppState hook with a context provider pattern.
 * Maintains backward compatibility with existing consumers that use setState
 * in the functional update style: setState((prev) => ({...prev, changes})).
 */

import React, {
  createContext,
  useContext,
  useReducer,
  type Dispatch,
  type SetStateAction,
  type ReactNode,
} from 'react';
import { loadPersistedAppState } from '../services/appStatePersistence';
import type { AppState } from '../types/app';

/**
 * Default application state values.
 * These are the base values used when no persisted state is available,
 * or for fields that should never be persisted (runtime-only state).
 */
const DEFAULT_APP_STATE: AppState = {
  provider: '',
  model: '',
  sessionId: null,
  queryCount: 0,
  messages: [],
  logs: [],
  currentView: 'chat',
  toolExecutions: [],
  stats: {},
  currentTodos: [],
  fileEdits: [],
  subagentActivities: [],
  delegateActivities: [],
  isConnected: false,
  isProcessing: false,
  lastError: null,
  queryProgress: null,
  activeChatId: null,
  chatSessions: [],
  perChatCache: {},
  securityApprovalRequest: null,
  securityPromptRequest: null,
  askUserRequest: null,
      modelSelectionRequest: null,
    driftNotification: null,
  };/**
 * Simple reducer that applies a state update.
 *
 * Handles both:
 * - Functional updaters: (prev: AppState) => AppState
 * - Partial state objects to merge (backward compatibility)
 *
 * @param prevState - The previous application state
 * @param action - Either a functional updater or partial state to merge
 * @returns The new application state
 */
function appStateReducer(prevState: AppState, action: SetStateAction<AppState>): AppState {
  if (typeof action === 'function') {
    return (action as (prev: AppState) => AppState)(prevState);
  }
  // Direct state update - full replacement (not partial merge)
  return action;
}

/**
 * Context value interface for AppState consumers.
 *
 * The setState function accepts either the new state directly (for backward
 * compatibility with some code) or a functional updater (the preferred pattern).
 * Mirrors React's setState(newValue or setState((prev) => newState)) API.
 */
export interface AppStateContextValue {
  /** Current application state */
  state: AppState;
  /**
   * Dispatch a state update. Accepts either:
   * - A partial state object to merge (backward compat)
   * - A functional updater that receives the previous state and returns the new state
   *
   * @example
   * // Functional updater (preferred)
   * setState((prev) => ({...prev, provider: 'openai'}))
   *
   * @example
   * // Direct update (backward compatible)
   * setState({...state, provider: 'openai'})
   */
  setState: Dispatch<SetStateAction<AppState>>;
}

export const AppStateContext = createContext<AppStateContextValue | null>(null);

export interface AppStateProviderProps {
  children: ReactNode;
}

/**
 * AppStateProvider - Wraps the application with app state management.
 *
 * Initializes state from localStorage using the same logic as the original
 * useAppState hook, merging persisted values with defaults and ensuring
 * runtime-only fields are reset.
 *
 * @example
 * ```tsx
 * <AppStateProvider>
 *   <App />
 * </AppStateProvider>
 * ```
 */
export function AppStateProvider({ children }: AppStateProviderProps): JSX.Element {
  const [state, dispatch] = useReducer(appStateReducer, DEFAULT_APP_STATE, (defaultState) => {
    const persisted = loadPersistedAppState();
    return {
      ...defaultState,
      ...persisted,
      // Runtime-only defaults that must never be loaded from storage
      isConnected: false,
      isProcessing: false,
      lastError: null,
      queryProgress: null,
      activeChatId: null,
      chatSessions: [],
      perChatCache: {},
    };
  });

  const contextValue: AppStateContextValue = {
    state,
    setState: dispatch,
  };

  return <AppStateContext.Provider value={contextValue}>{children}</AppStateContext.Provider>;
}

/**
 * useAppStateContext - Hook to access app state from context.
 *
 * @throws Error if used outside of AppStateProvider
 * @returns The AppStateContextValue containing state and setState
 *
 * @example
 * ```tsx
 * const { state, setState } = useAppStateContext();
 *
 * // Update state using functional updater
 * setState((prev) => ({...prev, provider: 'openai'}));
 *
 * // Read current state
 * console.log(state.provider);
 * ```
 */
export function useAppStateContext(): AppStateContextValue {
  const context = useContext(AppStateContext);
  if (context === null) {
    throw new Error('useAppStateContext must be used within an AppStateProvider');
  }
  return context;
}
