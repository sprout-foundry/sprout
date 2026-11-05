import { afterEach, describe, expect, it } from 'vitest';
import { installCopy, resetCopyForTests } from '../config/copy';
import { productName, SPROUT_PRODUCT_NAME } from './branding';
import { cloudHost } from './cloudHost';
import { localHost } from './localHost';

describe('productName', () => {
  afterEach(() => {
    resetCopyForTests();
  });

  it("defaults to Sprout's own name", () => {
    expect(SPROUT_PRODUCT_NAME).toBe('Sprout');
    expect(productName(undefined)).toBe('Sprout');
    expect(productName(null)).toBe('Sprout');
    expect(productName({} as never)).toBe('Sprout');
  });

  it('uses the host brand when one is declared', () => {
    expect(productName({ branding: { name: 'Acme Code' } } as never)).toBe('Acme Code');
  });

  it('falls back to Sprout for a blank declaration', () => {
    expect(productName({ branding: { name: '   ' } } as never)).toBe('Sprout');
  });

  it('reads the open-source host as Sprout and the platform host as its own brand', () => {
    expect(productName(localHost)).toBe('Sprout');
    expect(productName(cloudHost)).toBe('Sprout Foundry');
  });

  it('uses an installed app.name copy override when the host declares no brand', () => {
    installCopy({ 'app.name': 'Acme Code' });
    expect(productName(undefined)).toBe('Acme Code');
    expect(productName(localHost)).toBe('Acme Code');
  });

  it('prefers a declared host brand over an installed app.name override', () => {
    installCopy({ 'app.name': 'Acme Code' });
    expect(productName({ branding: { name: 'Host Brand' } } as never)).toBe('Host Brand');
    expect(productName(cloudHost)).toBe('Sprout Foundry');
  });

  it('ignores a blank app.name override', () => {
    installCopy({ 'app.name': '   ' });
    expect(productName(undefined)).toBe('Sprout');
  });
});
