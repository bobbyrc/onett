import { describe, expect, it } from 'vitest';
import { setupCodeFromHash } from './setupCode';

describe('setupCodeFromHash', () => {
  it('reads the code after #setup=', () => {
    expect(setupCodeFromHash('#setup=0a1b2c')).toBe('0a1b2c');
  });

  it('returns empty when the address has no code', () => {
    expect(setupCodeFromHash('')).toBe('');
    expect(setupCodeFromHash('#other')).toBe('');
    expect(setupCodeFromHash('#setup=')).toBe('');
  });

  it('accepts only lowercase hex', () => {
    expect(setupCodeFromHash('#setup=XYZ!')).toBe('');
  });

  it('refuses a code with anything after it', () => {
    expect(setupCodeFromHash('#setup=0a1b2c&x=1')).toBe('');
  });
});
