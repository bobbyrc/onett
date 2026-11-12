// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AccountPanel } from './AccountPanel';

vi.mock('../auth', () => ({
  useMe: () => ({ username: 'owner', role: 'owner' }),
  signOut: vi.fn(),
}));

afterEach(cleanup);

describe('AccountPanel', () => {
  it('marks the role as a role, so "owner owner" cannot be read as a stutter', () => {
    render(<AccountPanel />);
    // Seen: "owner (owner)". Read aloud: "owner (role: owner)".
    expect(document.querySelector('.account-name')?.textContent).toBe('owner (role: owner)');
    expect(document.querySelector('.account-role .sr-only')?.textContent).toBe('role: ');
    expect(screen.getByRole('button', { name: 'Sign out' })).toBeInTheDocument();
  });
});
