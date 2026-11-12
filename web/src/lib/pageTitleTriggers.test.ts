import { describe, expect, it } from 'vitest';
import { shortId } from './format';
import { routeTitle } from './pageTitle';

describe('routeTitle for triggers', () => {
  it('names the triggers list', () => {
    expect(routeTitle('/triggers')).toBe('Triggers');
  });

  it('names a trigger by the short form of its ID', () => {
    const id = '0190e5a2-7c41-7b3e-9d52-4f1a8c6e2b10';
    expect(routeTitle(`/triggers/${id}`)).toBe('Trigger ' + shortId(id));
  });
});
