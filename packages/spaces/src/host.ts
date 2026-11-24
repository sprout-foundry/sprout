/**
 * The host contract entry point.
 *
 * A host passes a `SproutHost` to the workspace; it is the only channel
 * between Sprout and whatever hosts it. The contract is defined in
 * `webui/src/host` today and is re-exported here so a host states its
 * identity, entitlements, transport, navigation intents, notification sink,
 * chrome slots, theme and capabilities through one documented surface.
 *
 * Nothing here reads a build flag, `appMode`, or the current URL to decide
 * who the host is.
 */
export * from '../../../webui/src/host/index';
