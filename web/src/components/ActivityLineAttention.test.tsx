// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';
import type { Run } from '../types';
import { ActivityLine, attentionFor } from './ActivityLine';

afterEach(cleanup);

const run = {
  id: 'r1',
  status: 'succeeded',
  started_at: '2026-01-01T12:00:00Z',
  ended_at: '2026-01-01T12:01:40Z',
  last_output_at: '2026-01-01T12:01:40Z',
} as Run;

const view = (props: Partial<React.ComponentProps<typeof ActivityLine>>) => (
  <MemoryRouter>
    <ActivityLine run={run} working={false} label="Implementer" {...props} />
  </MemoryRouter>
);

describe('ActivityLine attention', () => {
  it('says Waiting for you, without a timer, instead of the finished-run line', () => {
    render(view({ attention: 'waiting' }));
    const line = screen.getByRole('status');
    expect(line.textContent).toBe('Waiting for you');
    expect(line.className).toContain('activity-waiting');
  });

  it('says Blocked', () => {
    render(view({ attention: 'blocked' }));
    expect(screen.getByRole('status').textContent).toBe('Blocked');
  });

  it('shows even when the thread has no run yet', () => {
    render(view({ run: undefined, attention: 'waiting' }));
    expect(screen.getByRole('status').textContent).toBe('Waiting for you');
  });

  it('yields to Working while a turn runs', () => {
    render(view({ working: true, attention: 'blocked' }));
    expect(screen.getByRole('status').textContent).toContain('Working');
  });

  it('maps task statuses', () => {
    expect(attentionFor('awaiting_approval')).toBe('waiting');
    expect(attentionFor('blocked')).toBe('blocked');
    expect(attentionFor('running')).toBeUndefined();
  });
});
