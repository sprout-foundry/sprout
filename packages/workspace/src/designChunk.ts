/**
 * Design space views.
 *
 * A separate entry point and chunk: the design surface and its views load
 * when the design space opens, not when the package is imported. A later
 * step of the composition work replaces the web UI module paths with the
 * views package once the design views move there.
 */
export { default as DesignSurface } from '../../../webui/src/components/design/DesignSurface';
export { default as DesignView } from '../../../webui/src/components/design/DesignView';
