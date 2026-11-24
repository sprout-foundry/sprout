import SearchView from '../components/SearchView';
import type { SearchViewProps } from '../components/search/types';
import { requestOpenInEditor } from './openInEditor';
import './SpacesSearchView.css';

export interface SpacesSearchViewProps extends SearchViewProps {
  /** Additional class on the view root, alongside `sprout-spaces-search`. */
  className?: string;
}

/**
 * The spaces search view: the app's search panel (find, options,
 * exclude patterns, replace all) with results opening in the editor at the
 * matched line.
 *
 * Queries go to `/api/search` through the spaces fetch: a daemon host
 * answers from its backend, an in-browser (wasm) host from the browser file
 * store, as the app's own search does. Omitted, `onFileClick` dispatches a
 * `sprout:open-in-editor` request; the composed spaces hand it the editor
 * unit's opener. Unlike the app's sidebar panel, an arranged view does not
 * take focus on mount unless `autoFocus` is set.
 */
export function SpacesSearchView({ className, onFileClick, autoFocus = false }: SpacesSearchViewProps): JSX.Element {
  const rootClass = className ? `sprout-spaces-search ${className}` : 'sprout-spaces-search';
  return (
    <div className={rootClass} data-testid="spaces-search">
      <SearchView onFileClick={onFileClick ?? requestOpenInEditor} autoFocus={autoFocus} />
    </div>
  );
}

export default SpacesSearchView;
