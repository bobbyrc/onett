import { describe, expect, it } from 'vitest';
import { describePause } from './pause';

const since = '2026-11-04T22:36:00';

describe('describePause', () => {
  it('says who paused', () => {
    expect(describePause({ reason: 'owner', by: 'bob', since })).toMatch(/^Paused by bob since \d\d:\d\d\.$/);
  });

  it('omits the name when unknown', () => {
    expect(describePause({ reason: 'owner', by: '', since })).toMatch(/^Paused since \d\d:\d\d\.$/);
  });
});
