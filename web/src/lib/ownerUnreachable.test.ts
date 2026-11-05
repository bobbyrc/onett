import { describe, expect, it } from 'vitest';
import { OWNER_UNREACHABLE_MESSAGE, ownerWarning } from './ownerUnreachable';

describe('ownerWarning', () => {
  it('is null while the owner can be reached or stats are not loaded', () => {
    expect(ownerWarning(undefined)).toBeNull();
    expect(ownerWarning({ owner_unreachable: false, blocked_goals: [{ id: 'g', project: 'p', title: 't', reason: 'r' }] })).toBeNull();
  });

  it('warns with no goal lines when no goal is blocked', () => {
    expect(ownerWarning({ owner_unreachable: true, blocked_goals: [] })).toEqual({ message: OWNER_UNREACHABLE_MESSAGE, goals: [] });
  });

  it('names each blocked goal and its reason', () => {
    const w = ownerWarning({ owner_unreachable: true, blocked_goals: [{ id: 'g1', project: 'p', title: 'Ship it', reason: 'no progress' }] });
    expect(w?.goals).toEqual([{ id: 'g1', title: 'Ship it', text: 'is blocked and you have not been notified: no progress' }]);
  });
});
