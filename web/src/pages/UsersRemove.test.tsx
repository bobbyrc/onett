// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import type { User } from '../types';
import { canRemove } from './Users';

const user = (username: string, role: User['role']): User => ({ username, role, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' });

describe('canRemove', () => {
  it('is false for the only owner, whom the server never removes', () => {
    const owner = user('ann', 'owner');
    expect(canRemove(owner, [owner])).toBe(false);
    expect(canRemove(owner, [owner, user('bob', 'operator'), user('cy', 'viewer')])).toBe(false);
  });

  it('is true for an owner while another owner remains', () => {
    const ann = user('ann', 'owner');
    const bob = user('bob', 'owner');
    expect(canRemove(ann, [ann, bob])).toBe(true);
    expect(canRemove(bob, [ann, bob])).toBe(true);
  });

  it('is true for anyone who is not an owner', () => {
    const owner = user('ann', 'owner');
    const op = user('bob', 'operator');
    expect(canRemove(op, [owner, op])).toBe(true);
    expect(canRemove(user('cy', 'viewer'), [owner])).toBe(true);
  });
});
