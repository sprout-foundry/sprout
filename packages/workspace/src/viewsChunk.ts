/**
 * Views entry point for the library build.
 *
 * The views a host composes (chat, agent changes, files, editor, preview,
 * the layout and the arrangement) as their own entry point and chunk. A host
 * loads them when it mounts them, not when it imports the package.
 */
export * from '../../../webui/src/views/index';
export { ViewsLayout } from '../../../webui/src/views/ViewsLayout';
