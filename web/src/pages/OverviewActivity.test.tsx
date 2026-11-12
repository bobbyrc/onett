// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { OnettEvent } from '../types';
import { Activity } from './Overview';

const feed = vi.hoisted(() => ({ events: [] as unknown[] }));

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  getEventsHistory: async () => feed.events,
}));

vi.mock('../events', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../events')>()),
  subscribe: () => () => {},
}));

afterEach(cleanup);

const show = (events: OnettEvent[]) => {
  feed.events = events;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <Activity problems={[]} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
};

const blocked: OnettEvent = {
  id: 1,
  time: new Date().toISOString(),
  type: 'task.updated',
  project: 'onett',
  task_id: 't1',
  message: 'Fix login: blocked: tests fail',
  data: { id: 't1', title: 'Fix login', status: 'blocked', blocked_reason: 'tests fail' },
};

describe('Overview Activity', () => {
  it('renders a blocked task as a sentence with links to the task and the project', async () => {
    show([blocked]);
    const link = await screen.findByRole('link', { name: 'Fix login' });
    expect(link.getAttribute('href')).toBe('/tasks/t1');
    const message = link.closest('.feed-message');
    expect(message?.textContent?.startsWith('Blocked: ')).toBe(true);
    expect(document.querySelector('.ov-feed-where')?.textContent).toBe('onett');
    expect(document.querySelectorAll('.feed-row')).toHaveLength(1);
  });

  it('renders no row for a chat message', async () => {
    show([{ id: 2, time: new Date().toISOString(), type: 'chat.message', message: 'hello' }]);
    await screen.findByText('No activity yet. Events appear here as work moves.');
    expect(document.querySelectorAll('.feed-row')).toHaveLength(0);
  });

  it('shows the empty text when there are no events', async () => {
    show([]);
    await screen.findByText('No activity yet. Events appear here as work moves.');
  });
});
