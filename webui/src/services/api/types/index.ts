/**
 * Barrel re-export for all API type definitions.
 *
 * Types are organized by domain in individual files within this directory.
 * This barrel preserves backward compatibility — anything importing from
 * './types' (resolved to this index) gets every type that was previously
 * in the monolithic types.ts.
 */

export * from './common';
export * from './credentials';
export * from './editor';
export * from './git';
export * from './misc';
export * from './onboarding';
export * from './search';
export * from './session';
export * from './settings';
export * from './ssh';
