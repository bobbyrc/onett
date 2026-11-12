import { describe, expect, it } from 'vitest';
import { ageWords } from './needs';

describe('ageWords when the age cannot be worked out', () => {
  it('says under a minute for NaN, Infinity and a negative age, never NaN', () => {
    expect(ageWords(NaN)).toBe('under a minute');
    expect(ageWords(Infinity)).toBe('under a minute');
    expect(ageWords(-5)).toBe('under a minute');
  });
});
