// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { LogLine } from '../types';

const lines: LogLine[] = [];
vi.mock('../runLogs', () => ({ useRunLogLines: () => ({ lines, loading: false }) }));
import { LogViewer } from './LogViewer';

const L = (seq: number, kind: LogLine['kind'], text: string): LogLine => ({ run_id: 'r1', seq, kind, text, time: '2026-11-05T12:00:00Z' });

describe('LogViewer', () => {
  afterEach(cleanup);
  it('shows a tool call as name and main argument, with the full input behind show more', () => {
    lines.length = 0;
    const raw = 'Bash: {"command":"cd web && npm test","timeout":600000}';
    lines.push(L(1, 'tool_use', raw));
    render(<LogViewer runId="r1" active={false} />);
    expect(screen.getByText('Bash: cd web && npm test')).toBeTruthy();
    expect(screen.queryByText(raw)).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'show more' }));
    expect(screen.getByText(raw)).toBeTruthy();
  });

  it('hides tool_progress heartbeats and other raw stream events', () => {
    lines.length = 0;
    lines.push(
      L(1, 'system', '{"type":"tool_progress","tool_use_id":"a-heartbeat-1","elapsed_time_seconds":3}'),
      L(2, 'system', '{"type":"rate_limit_event","x":1}'),
      L(3, 'system', 'session started'),
    );
    render(<LogViewer runId="r1" active={false} />);
    expect(screen.queryByText(/tool_progress/)).toBeNull();
    expect(screen.queryByText(/rate_limit_event/)).toBeNull();
    expect(screen.getByText('session started')).toBeTruthy();
    expect(screen.getByText('1 line')).toBeTruthy();
  });
});
