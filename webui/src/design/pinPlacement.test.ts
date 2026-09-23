/**
 * pinPlacement bridge tests (SP-140-6 §6e).
 *
 * The arm/point CustomEvent bridge and the card-badge count helper: pure
 * window-event behavior, testable without components.
 */

import { describe, expect, it, vi } from 'vitest';
import {
  armPinPlacement,
  disarmPinPlacement,
  onPinPlacePoint,
  onPinPlacementArm,
  pendingCountForStem,
  publishPinPlacePoint,
} from './pinPlacement';

describe('pinPlacement bridge', () => {
  it('arm reaches the listener; disarm carries cancel', () => {
    const handler = vi.fn();
    const off = onPinPlacementArm(handler);
    armPinPlacement();
    expect(handler).toHaveBeenLastCalledWith(false);
    disarmPinPlacement();
    expect(handler).toHaveBeenLastCalledWith(true);
    off();
  });

  it('unsubscribe stops the arm events', () => {
    const handler = vi.fn();
    const off = onPinPlacementArm(handler);
    off();
    armPinPlacement();
    expect(handler).not.toHaveBeenCalled();
  });

  it('a placed point reaches the listener', () => {
    const handler = vi.fn();
    const off = onPinPlacePoint(handler);
    publishPinPlacePoint({ x: 0.42, y: 0.18 });
    expect(handler).toHaveBeenCalledWith({ x: 0.42, y: 0.18 });
    off();
  });

  it('malformed points are ignored, not clamped', () => {
    const handler = vi.fn();
    const off = onPinPlacePoint(handler);
    window.dispatchEvent(new CustomEvent('sprout-design-place-point', { detail: { x: 'left', y: 1 } }));
    window.dispatchEvent(new CustomEvent('sprout-design-place-point', { detail: { x: Number.NaN, y: 1 } }));
    window.dispatchEvent(new CustomEvent('sprout-design-place-point', { detail: undefined }));
    expect(handler).not.toHaveBeenCalled();
    off();
  });
});

describe('pendingCountForStem', () => {
  const feedback = [
    { name: 'login.json', annotationCount: 3, resolvedCount: 1 },
    { name: 'inbox.json', annotationCount: 2, resolvedCount: 2 },
  ];

  it('counts unresolved annotations', () => {
    expect(pendingCountForStem(feedback, 'login')).toBe(2);
  });

  it('a fully resolved target counts zero', () => {
    expect(pendingCountForStem(feedback, 'inbox')).toBe(0);
  });

  it('a target with no feedback file counts zero', () => {
    expect(pendingCountForStem(feedback, 'signup')).toBe(0);
  });
});
