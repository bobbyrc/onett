// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const api = vi.hoisted(() => ({ retryTask: vi.fn() }));
const role = vi.hoisted(() => ({ operate: true }));
vi.mock('../api', async (importOriginal) => ({ ...(await importOriginal<typeof import('../api')>()), ...api }));
vi.mock('../auth', async (importOriginal) => ({ ...(await importOriginal<typeof import('../auth')>()), useCan: () => role.operate }));

import { RetryTasksButton } from './RetryTasksButton';

function wrap(ui: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

beforeEach(() => {
  role.operate = true;
  api.retryTask.mockResolvedValue(undefined);
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('RetryTasksButton', () => {
  it('retries each task once, one after the other, in the order given', async () => {
    wrap(<RetryTasksButton ids={['a', 'b', 'c']} label="Retry all 3" />);
    fireEvent.click(screen.getByRole('button', { name: 'Retry all 3' }));
    await waitFor(() => expect(api.retryTask).toHaveBeenCalledTimes(3));
    expect(api.retryTask.mock.calls.map((call) => call[0])).toEqual(['a', 'b', 'c']);
  });

  it('with confirm, the first click only arms the button and the second retries', async () => {
    wrap(<RetryTasksButton ids={['a']} label="Retry all 1" confirm />);
    fireEvent.click(screen.getByRole('button', { name: 'Retry all 1' }));
    expect(screen.getByRole('button', { name: 'Confirm?' })).toBeTruthy();
    expect(api.retryTask).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }));
    await waitFor(() => expect(api.retryTask).toHaveBeenCalledWith('a'));
  });

  it('keeps going after one failure and reports how many could not be retried', async () => {
    api.retryTask.mockImplementation(async (id: string) => {
      if (id === 'b') throw new Error('boom');
    });
    wrap(<RetryTasksButton ids={['a', 'b', 'c']} label="Retry all 3" />);
    fireEvent.click(screen.getByRole('button', { name: 'Retry all 3' }));
    expect(await screen.findByText('1 of 3 could not be retried: boom')).toBeTruthy();
    expect(api.retryTask).toHaveBeenCalledTimes(3);
  });

  it('shows nothing to a viewer who cannot operate', () => {
    role.operate = false;
    wrap(<RetryTasksButton ids={['a']} label="Retry" />);
    expect(screen.queryByRole('button')).toBeNull();
  });
});
