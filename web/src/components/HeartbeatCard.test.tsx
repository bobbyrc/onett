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
    expect(screen.getByRole('heading', { name: /Orchestrator/ }).textContent).toContain('heartbeat on');
    expect(screen.getByRole('heading', { name: /Orchestrator/ }).textContent).toMatch(/next check in (9|10)m/);
    expect(screen.queryByText('Next run')).toBeNull();
    expect(screen.getByText('Last check').nextElementSibling?.textContent).toMatch(/^[45]m ago$/);
    expect(screen.getByText('Told you, 24h').nextElementSibling?.textContent).toBe('2 times');
    expect(screen.getByText('Acted itself, 24h').nextElementSibling?.textContent).toBe('7 times');
    expect(screen.queryByText('Needs attention')).toBeNull();
    expect(screen.getByRole('link', { name: 'Open Operations' }).getAttribute('href')).toBe('/chat/conv1');
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('says what the heartbeat does in plain words', () => {
    show([loop()]);
    // The fixture's every is "15m", which formatGoDuration leaves as "15m".
    expect(document.querySelector('.ov-lead')?.textContent).toContain('Every 15m');
  });

  it('says disabled, has no next check, and shows the last error', () => {
    show([loop({ enabled: false, last_error: 'boom' })]);
    expect(screen.getByRole('heading', { name: /Orchestrator/ }).textContent).toContain('heartbeat disabled');
    expect(screen.getByRole('heading', { name: /Orchestrator/ }).textContent).not.toContain('next check');
    expect(screen.getByRole('alert').textContent).toContain('boom');
  });

  it('says quiet hours inside the window', () => {
    show([loop({ quiet_now: true })]);
    expect(screen.getByRole('heading', { name: /Orchestrator/ }).textContent).toContain('quiet hours');
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
