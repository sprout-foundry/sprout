import {
  useContext,
  useCallback,
  useMemo,
  useSyncExternalStore,
  createContext,
  createElement,
  type ReactNode,
} from 'react';
import type { AppState } from '../types/app';

// ── Type Definitions ─────────────────────────────────────────────────────

/**
 * Type for the setState function returned by useAppStoreSetState
 *
 * Unlike React's built-in setState, the updater returns a Partial<AppState>
 * containing only the fields that changed. The store preserves all other
 * field references unchanged, enabling fine-grained re-render optimization.
 *
 * Usage:
 * ```tsx
 * const setState = useAppStoreSetState();
 * setState(prev => ({ messages: [...prev.messages, newMessage] }));
 * ```
 */
export type AppStoreSetState = (updater: (prev: AppState) => Partial<AppState>) => void;

// ── AppStore Class ───────────────────────────────────────────────────────

/**
 * Performance-optimized state store using the external store pattern.
 *
 * Key optimization: When setState is called, only the fields that actually changed
 * get new references. Unchanged fields keep their old object references.
 * This means Object.is(prev.field, next.field) returns true for unchanged fields,
 * allowing useSyncExternalStore to skip re-renders for components that only
 * subscribe to those fields.
 */
class AppStore {
  private state: AppState;
  private listeners: Set<() => void>;
  /** Stable bound subscribe function — avoids creating new functions on each access. */
  private boundSubscribe: (listener: () => void) => () => void;

  constructor(initialState: AppState) {
    this.state = initialState;
    this.listeners = new Set();
    // Bind once so useSyncExternalStore sees a stable subscribe reference.
    this.boundSubscribe = this.subscribe.bind(this);
  }

  /**
   * Get the current full state snapshot.
   */
  getSnapshot(): AppState {
    return this.state;
  }

  /**
   * Returns the stable subscribe function for useSyncExternalStore.
   * This is the same reference for the lifetime of the store instance.
   */
  getSubscribe(): (listener: () => void) => () => void {
    return this.boundSubscribe;
  }

  /**
   * Subscribe to state changes.
   * @param listener Function called when state changes
   * @returns Unsubscribe function
   */
  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  /**
   * Update state with an updater function.
   *
   * Only fields that the updater returns and that are actually different
   * will get new references. All other fields keep their old references.
   *
   * @example
   * ```ts
   * store.set(prev => ({ messages: [...prev.messages, newMessage] }));
   * ```
   */
  set(updater: (prev: AppState) => Partial<AppState>): void {
    const partial = updater(this.state);

    // Guard against accidental undefined / null return values.
    if (!partial || typeof partial !== 'object') return;

    // Create new state, only replacing fields that the updater returned.
    const newState = { ...this.state };
    let changed = false;

    for (const key of Object.keys(partial)) {
      if (!Object.is(partial[key as keyof AppState], this.state[key as keyof AppState])) {
        (newState as Record<string, unknown>)[key] = partial[key as keyof AppState];
        changed = true;
      }
    }

    if (!changed) {
      return; // No actual change, skip notification.
    }

    this.state = newState as AppState;
    this.listeners.forEach((l) => l());
  }
}

// ── Factory Function ─────────────────────────────────────────────────────

/**
 * Create a new AppStore instance with the given initial state.
 */
export function createAppStore(initialState: AppState): AppStore {
  return new AppStore(initialState);
}

// ── Context ─────────────────────────────────────────────────────────────

const AppStoreContext = createContext<AppStore | null>(null);

// ── Provider Component ────────────────────────────────────────────────────

export interface AppStoreProviderProps {
  initialState: AppState;
  children: ReactNode;
}

/**
 * Provider component that makes the AppStore available to all descendants.
 *
 * @example
 * ```tsx
 * <AppStoreProvider initialState={initialState}>
 *   <App />
 * </AppStoreProvider>
 * ```
 */
export function AppStoreProvider({ initialState, children }: AppStoreProviderProps): React.JSX.Element {
  const store = useMemo(() => createAppStore(initialState), [initialState]);

  return createElement(AppStoreContext.Provider, { value: store }, children);
}

// ── Hooks ───────────────────────────────────────────────────────────────

/**
 * Hook to access the AppStore instance.
 * @private
 */
function useAppStore(): AppStore {
  const store = useContext(AppStoreContext);
  if (!store) {
    throw new Error('useAppStore must be used within an AppStoreProvider');
  }
  return store;
}

/**
 * Get the full app state.
 *
 * This hook subscribes to all state changes and returns the complete AppState.
 * Use sparingly — prefer `useAppStateField` for single field subscriptions to
 * avoid unnecessary re-renders.
 *
 * @example
 * ```tsx
 * const state = useAppStoreState();
 * console.log(state.messages);
 * ```
 */
export function useAppStoreState(): AppState {
  const store = useAppStore();
  const getSnapshot = useCallback(() => store.getSnapshot(), [store]);
  return useSyncExternalStore(store.getSubscribe(), getSnapshot);
}

/**
 * Get the setState function for updating the app state.
 *
 * @example
 * ```tsx
 * const setState = useAppStoreSetState();
 *
 * const addMessage = (msg: Message) => {
 *   setState(prev => ({ messages: [...prev.messages, msg] }));
 * };
 * ```
 */
export function useAppStoreSetState(): AppStoreSetState {
  const store = useAppStore();
  return useCallback(
    (updater: (prev: AppState) => Partial<AppState>) => {
      store.set(updater);
    },
    [store],
  );
}

/**
 * Get a selected value from the app state using a selector function.
 *
 * The component will re-render whenever the selected value changes (determined by Object.is).
 *
 * IMPORTANT: For optimal performance, the selector should be a stable function reference
 * (e.g., defined outside the component or wrapped in useCallback). If the selector
 * function changes on every render, it will cause unnecessary re-renders.
 *
 * @example
 * ```tsx
 * // Better: use useAppStateField for single fields
 * const messages = useAppStateField('messages');
 *
 * // Alternative: use selector for derived values
 * const getMessageCount = useCallback((s: AppState) => s.messages.length, []);
 * const count = useAppState(getMessageCount);
 * ```
 */
export function useAppState<R>(selector: (s: AppState) => R): R {
  const store = useAppStore();
  const getSnapshot = useCallback(() => selector(store.getSnapshot()), [store, selector]);
  return useSyncExternalStore(store.getSubscribe(), getSnapshot);
}

/**
 * Get a single field from the app state with optimized re-render behavior.
 *
 * This is the preferred way to access state fields. The component will only
 * re-render when the specified field actually changes (determined by Object.is).
 *
 * @param key The AppState field key to subscribe to
 * @returns The value of the specified field
 *
 * @example
 * ```tsx
 * const messages = useAppStateField('messages');
 * const isProcessing = useAppStateField('isProcessing');
 * const logs = useAppStateField('logs');
 * ```
 */
export function useAppStateField<K extends keyof AppState>(key: K): AppState[K] {
  const store = useAppStore();
  // useCallback ensures a stable getSnapshot reference for this (store, key) pair.
  const getSnapshot = useCallback(() => store.getSnapshot()[key], [store, key]);
  return useSyncExternalStore(
    store.getSubscribe(), // stable reference — no re-subscription on render
    getSnapshot,
  );
}
