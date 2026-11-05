// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';
import type { LoopView } from '../types';
import { HeartbeatStatus } from './HeartbeatCard';

afterEach(cleanup);

const loop = (over: Partial<LoopView> = {}): LoopView => ({
  name: 'heartbeat',
  project: 'onett',
  enabled: true,
  every: '15m',
  last_run_at: new Date(Date.now() - 5 * 60_000).toISOString(),
  next_run_at: new Date(Date.now() + 10 * 60_000).toISOString(),
  item_count: 3,
  actions_24h: 7,
  escalations_24h: 2,
  operations_conversation_id: 'conv1',
  ...over,
});

const show = (loops: LoopView[]) =>
  render(
    <MemoryRouter>
      <HeartbeatStatus loops={loops} />
    </MemoryRouter>,
  );

describe('HeartbeatStatus', () => {
  it('shows the counts, times and the Operations link', () => {
    show([loop()]);
    expect(screen.getByRole('heading', { name: /Heartbeat/ }).textContent).toContain('on');
    expect(screen.getByText('Last run').nextElementSibling?.textContent).toMatch(/^[45]m ago$/);
    expect(screen.getByText('Next run').nextElementSibling?.textContent).toMatch(/^in 9m|^in 10m/);
    expect(screen.getByText('Needs attention').nextElementSibling?.textContent).toBe('3');
    expect(screen.getByText('Actions, 24h').nextElementSibling?.textContent).toBe('7');
    expect(screen.getByText('Escalations, 24h').nextElementSibling?.textContent).toBe('2');
    expect(screen.getByRole('link', { name: 'Open Operations' }).getAttribute('href')).toBe('/chat/conv1');
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('says disabled, has no next run, and shows the last error', () => {
    show([loop({ enabled: false, last_error: 'boom' })]);
    expect(screen.getByRole('heading', { name: /Heartbeat/ }).textContent).toContain('disabled');
    expect(screen.getByText('Next run').nextElementSibling?.textContent).toBe('—');
    expect(screen.getByRole('alert').textContent).toContain('boom');
  });

  it('says quiet hours inside the window', () => {
    show([loop({ quiet_now: true })]);
    expect(screen.getByRole('heading', { name: /Heartbeat/ }).textContent).toContain('quiet hours');
  });

  it('has no Operations link before the conversation exists', () => {
    show([loop({ operations_conversation_id: undefined })]);
    expect(screen.queryByRole('link', { name: 'Open Operations' })).toBeNull();
  });

  it('is hidden when the daemon reports no heartbeat loop', () => {
    const { container } = show([]);
    expect(container.textContent).toBe('');
  });
});
