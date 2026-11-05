import { describe, expect, it } from 'vitest';
import { goalActions, needsText } from './goals';

describe('goalActions', () => {
  it('offers what the server accepts for each status', () => {
    expect(goalActions('active')).toEqual(['pause', 'block', 'done']);
    expect(goalActions('paused')).toEqual(['resume', 'block', 'done']);
    expect(goalActions('blocked')).toEqual(['resume', 'done']);
    expect(goalActions('done')).toEqual([]);
  });
  it('asks for text only on done and block', () => {
    expect(needsText('done')).toBe(true);
    expect(needsText('block')).toBe(true);
    expect(needsText('pause')).toBe(false);
  });
});
