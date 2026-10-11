import SearchView from '../components/SearchView';
import type { SearchViewProps } from '../components/search/types';
import { requestOpenInEditor } from './openInEditor';
import './WorkspaceSearchView.css';

export interface WorkspaceSearchViewProps extends SearchViewProps {
  /** Additional class on the view root, alongside `sprout-workspace-search`. */
  className?: string;
}

/**
 * The workspace search view: the app's search panel (find, options,
 * exclude patterns, replace all) with results opening in the editor at the
 * matched line.
 *
 * Queries go to `/api/search` through the workspace fetch: a daemon host
 * answers from its backend, an in-browser (wasm) host from the browser file
 * store, as the app's own search does. Omitted, `onFileClick` dispatches a
 * `sprout:open-in-editor` request; the composed workspace hands it the editor
 * unit's opener. Unlike the app's sidebar panel, an arranged view does not
 * take focus on mount unless `autoFocus` is set.
 */
export function WorkspaceSearchView({
  className,
  onFileClick,
  autoFocus = false,
}: WorkspaceSearchViewProps): JSX.Element {
  const rootClass = className ? `sprout-workspace-search ${className}` : 'sprout-workspace-search';
  return (
    <div className={rootClass} data-testid="workspace-search">
      <SearchView onFileClick={onFileClick ?? requestOpenInEditor} autoFocus={autoFocus} />
    </div>
  );
}

export default WorkspaceSearchView;
