/**
 * contractCompat tests (SP-160 §160c) — API contract version negotiation.
 *
 * Pins the negotiation rules the app applies before it starts:
 *   - a different MAJOR contract version is refused (both directions: the
 *     daemon reporting a HIGHER major and an older major both refuse — the
 *     rule is "major versions are not compatible with each other", not
 *     "newer is fine");
 *   - a newer MINOR version on the daemon starts with a warning (forward
 *     compatible);
 *   - an equal or older minor, and a daemon that predates the field, start
 *     cleanly;
 *   - an unparseable reported version refuses (a garbage value must not be
 *     treated as compatible);
 *   - the build's pin is 1.0.0 (the three-way pin with the Go source of
 *     truth and the seed; a Go test fails if they drift).
 */

import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { checkContractCompat, ContractRefusal, CONTRACT_VERSION } from './contractCompat';

describe('CONTRACT_VERSION pin', () => {
  it('is pinned to 1.0.0 (the contract the seed and the Go source of truth name)', () => {
    expect(CONTRACT_VERSION).toBe('1.0.0');
  });
});

describe('checkContractCompat', () => {
  it('starts cleanly when the daemon matches the build pin', () => {
    expect(checkContractCompat('1.0.0')).toEqual({ ok: true });
  });

  it('starts cleanly for an equal major with an older minor', () => {
    const result = checkContractCompat('1.0.0', '1.2.0');
    expect(result.ok).toBe(true);
    expect(result.warning).toBeUndefined();
  });

  it('starts with a warning for a newer minor contract', () => {
    const result = checkContractCompat('1.3.0');
    expect(result.ok).toBe(true);
    expect(result.warning).toContain('1.3.0');
    expect(result.warning).toContain('1.0.0');
  });

  it('refuses when the daemon reports a HIGHER major version', () => {
    const result = checkContractCompat('2.0.0');
    expect(result.ok).toBe(false);
    expect(result.message).toContain('2.0.0');
    expect(result.message).toContain('1.0.0');
  });

  it('refuses when the daemon reports an OLDER major version', () => {
    const result = checkContractCompat('0.9.0');
    expect(result.ok).toBe(false);
    expect(result.message).toContain('0.9.0');
  });

  it('refuses an unparseable reported version instead of guessing', () => {
    const result = checkContractCompat('banana');
    expect(result.ok).toBe(false);
    expect(result.message).toContain('banana');
  });

  it('treats a daemon without the field as compatible (pre-negotiation daemon)', () => {
    expect(checkContractCompat(undefined).ok).toBe(true);
    expect(checkContractCompat('').ok).toBe(true);
    expect(checkContractCompat('   ').ok).toBe(true);
  });

  it('accepts single-part and pre-release forms of the version', () => {
    expect(checkContractCompat('1').ok).toBe(true);
    expect(checkContractCompat('1.0.0-beta.1').ok).toBe(true);
    expect(checkContractCompat('2.0.0-beta.1').ok).toBe(false);
  });

  it('honors an explicit expected version (a build pinned to a different contract)', () => {
    // A client built against contract 2.x refuses a 1.x daemon.
    expect(checkContractCompat('1.0.0', '2.0.0').ok).toBe(false);
    // ...and accepts its own major with a newer minor.
    const newerMinor = checkContractCompat('2.4.0', '2.0.0');
    expect(newerMinor.ok).toBe(true);
    expect(newerMinor.warning).toBeDefined();
  });
});

describe('ContractRefusal', () => {
  it('renders a blocking alert with the supplied message', () => {
    render(<ContractRefusal message="daemon reports 2.0.0" />);
    const alert = screen.getByRole('alert');
    expect(alert).toHaveTextContent('Incompatible API contract version');
    expect(alert).toHaveTextContent('daemon reports 2.0.0');
  });
});
