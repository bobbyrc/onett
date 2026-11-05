// @vitest-environment jsdom
import { act, cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Run } from '../types';
import { ActivityLine, spokenDuration } from './ActivityLine';

const T0 = Date.parse('2026-01-01T12:00:00Z');
const iso = (ms: number) => new Date(T0 + ms).toISOString();

function makeRun(over: Partial<Run> = {}): Run {
  return {
    id: 'run1',
    started_at: iso(0),
    last_output_at: iso(0),
    status: 'running',
    ...over,
  } as Run;
}

function view(props: Partial<React.ComponentProps<typeof ActivityLine>>) {
  const run = props.run ?? makeRun();
  return (
    <MemoryRouter>
      <ActivityLine run={run} working label="Orchestrator" {...props} />
    </MemoryRouter>
  );
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(T0 + 72_000);
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const status = () => screen.getByRole('status');
const spoken = () => status().textContent ?? '';

describe('ActivityLine', () => {
  it('shows Working with elapsed time from started_at and ticks', () => {
    render(view({ run: makeRun({ last_output_at: iso(70_000) }) }));
    expect(status().textContent).toContain('Working · 1m 12s');
    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(status().textContent).toContain('Working · 1m 15s');
  });

  it('keeps the run link and has no Stop button', () => {
    render(view({ run: makeRun({ last_output_at: iso(70_000) }) }));
    expect(screen.getByRole('link', { name: 'view run' }).getAttribute('href')).toBe('/runs/run1');
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('shows the stall hint only after 30s without output', () => {
    // Quiet for exactly 30s at render time: no hint yet.
    render(view({ run: makeRun({ last_output_at: iso(42_000) }) }));
    expect(status().textContent).not.toContain('output');
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(status().textContent).toContain('output 31s ago');
  });

  it('uses the newest log line as the last output', () => {
    const lines = [{ time: iso(71_000) }] as never;
    render(view({ run: makeRun({ last_output_at: iso(0) }), lines }));
    expect(status().textContent).not.toContain('output');
  });

  it('keeps the live region text constant between ticks', () => {
    render(view({ run: makeRun({ last_output_at: iso(70_000) }) }));
    const hidden = status().querySelectorAll('[aria-hidden="true"]');
    const stripped = () => {
      const clone = status().cloneNode(true) as HTMLElement;
      clone.querySelectorAll('[aria-hidden="true"]').forEach((n) => n.remove());
      return clone.textContent;
    };
    const before = stripped();
    act(() => {
      vi.advanceTimersByTime(5000);
    });
    expect(stripped()).toBe(before);
    expect(before).toContain('Working');
    expect(hidden.length).toBeGreaterThan(0);
    expect(status().getAttribute('aria-live')).toBe('polite');
  });

  it('shows Worked for after success, with spoken text and no lamp', () => {
    const run = makeRun({ status: 'succeeded', ended_at: iso(100_000) });
    const { container } = render(view({ run, working: false }));
    expect(spoken()).toContain('Worked for 1m 40s');
    expect(spoken()).toContain('1 minute 40 seconds');
    expect(container.querySelector('.activity-lamp')).toBeNull();
    expect(container.querySelector('.activity-worked')).not.toBeNull();
  });

  it('says Stopped after for a cancelled run and Failed after for a failed one', () => {
    const stopped = makeRun({ status: 'cancelled', ended_at: iso(40_000) });
    const { rerender } = render(view({ run: stopped, working: false }));
    expect(spoken()).toContain('Stopped after 40s');
    expect(status().className).toContain('activity-stopped');
    rerender(view({ run: makeRun({ status: 'failed', ended_at: iso(62_000) }), working: false }));
    expect(spoken()).toContain('Failed after 1m 02s');
    expect(status().className).toContain('activity-failed');
  });

  it('renders nothing for a finished run with no end time', () => {
    const { container } = render(view({ run: makeRun({ status: 'succeeded' }), working: false }));
    expect(container.textContent).toBe('');
  });
});

describe('spokenDuration', () => {
  it('spells units out', () => {
    expect(spokenDuration(100_000)).toBe('1 minute 40 seconds');
    expect(spokenDuration(1000)).toBe('1 second');
    expect(spokenDuration(3_660_000)).toBe('1 hour 1 minute');
  });
});
