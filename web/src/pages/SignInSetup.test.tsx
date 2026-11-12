// @vitest-environment jsdom
// The first-account sheet needs the setup code that `onett open` puts in the address. Without it
// the sheet only says how to get one; with it, the code goes to the server with the new account.
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const auth = vi.hoisted(() => ({ setUp: vi.fn(), signIn: vi.fn() }));
vi.mock('../auth', async (importOriginal) => ({ ...(await importOriginal<typeof import('../auth')>()), ...auth }));

import { SignIn } from './SignIn';

beforeEach(() => {
  vi.clearAllMocks();
  auth.setUp.mockResolvedValue(undefined);
  auth.signIn.mockResolvedValue(undefined);
});

afterEach(() => {
  cleanup();
  window.location.hash = '';
});

describe('SignIn setup code', () => {
  it('shows the instruction, and no form fields, when setup has no code', () => {
    window.location.hash = '';
    render(<SignIn setup={true} expired={false} />);

    expect(screen.getByRole('heading', { name: 'Create the owner account' })).toBeTruthy();
    expect(screen.getByText(/onett open/)).toBeTruthy();
    expect(screen.queryByLabelText('Username')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Create account' })).toBeNull();
  });

  it('sends the code from the address with the new account', async () => {
    window.location.hash = '#setup=0a1b2c';
    render(<SignIn setup={true} expired={false} />);

    // The setup labels carry their hint text, so match them by prefix.
    fireEvent.change(screen.getByLabelText(/^Username/), { target: { value: 'owner' } });
    fireEvent.change(screen.getByLabelText(/^Password/), { target: { value: 'a-long-password-123' } });
    fireEvent.change(screen.getByLabelText('Confirm password'), { target: { value: 'a-long-password-123' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));

    await waitFor(() => expect(auth.setUp).toHaveBeenCalledWith('owner', 'a-long-password-123', '0a1b2c'));
  });

  it('removes the code from the address bar after reading it', async () => {
    window.location.hash = '#setup=0a1b2c';
    render(<SignIn setup={true} expired={false} />);

    await waitFor(() => expect(window.location.hash).toBe(''));
  });

  it('reads a setup link pasted into the open page', async () => {
    window.location.hash = '';
    render(<SignIn setup={true} expired={false} />);
    expect(screen.getByText(/onett open/)).toBeTruthy();

    window.location.hash = '#setup=0a1b2c';
    act(() => {
      window.dispatchEvent(new HashChangeEvent('hashchange'));
    });

    await waitFor(() => expect(screen.getByRole('button', { name: 'Create account' })).toBeTruthy());
    expect(screen.queryByText(/onett open/)).toBeNull();
  });

  it('keeps sign-in mode unchanged without a code', () => {
    window.location.hash = '';
    render(<SignIn setup={false} expired={false} />);

    expect(screen.getByLabelText('Username')).toBeTruthy();
    expect(screen.getByLabelText('Password')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeTruthy();
    expect(screen.queryByText(/onett open/)).toBeNull();
  });
});
