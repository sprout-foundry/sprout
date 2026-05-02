/**
 * Re-export of useAppStateContext for backward compatibility.
 *
 * All existing consumers import { useAppState } from './hooks/useAppState'
 * and this module simply re-exports the context hook so those imports
 * continue to work without changes.
 */
export { useAppStateContext as useAppState } from '../contexts/AppStateContext';
export type { AppStateContextValue as UseAppStateReturn } from '../contexts/AppStateContext';
