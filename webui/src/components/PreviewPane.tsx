/**
 * Thin re-export of the presentational PreviewPane, which lives in
 * `@sprout/ui` (packages/ui — it is props-only and has no app
 * dependencies). Kept as a webui module so existing relative imports
 * (`import PreviewPane from './PreviewPane'`) and tests keep working;
 * styles ship via `@sprout/ui/dist/style.css`.
 */
import { PreviewPane } from '@sprout/ui';

export { PreviewPane };
export type { PreviewPaneProps, PreviewPaneStatus } from '@sprout/ui';
export default PreviewPane;
