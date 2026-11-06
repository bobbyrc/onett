// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { Entry } from '../lib/transcript';
import { Transcript } from './Transcript';
import { turnWork } from './toolRuns';

const at = (s: number) => new Date(Date.parse('2026-11-05T12:00:00Z') + s * 1000).toISOString();
const tool = (id: string, s: number, over: object = {}) =>
  ({ id, kind: 'tool', runId: 'r1', time: at(s), tool: { name: 'Bash', arg: 'go test' }, input: 'Bash: {}', result: 'ok', ...over }) as Entry;
const entries = (): Entry[] => [
  { id: 'u', kind: 'owner', text: 'go', time: at(0) },
  tool('t1', 1),
  tool('t2', 2),
  { id: 'a', kind: 'text', runId: 'r1', time: at(100), text: 'The answer.' },
];
const view = (e: Entry[], liveRunId?: string) =>
  render(<Transcript entries={e} view="conversation" agentLabel="Orchestrator" liveRunId={liveRunId} />);

describe('turnWork', () => {
  it('folds the tools before a final answer and times the turn', () => {
    const [turn] = turnWork(entries());
    expect(turn.key).toBe('t1');
    expect([...turn.ids]).toEqual(['t1', 't2']);
    expect(turn.ms).toBe(99_000);
  });
  it('does not fold the live turn, a turn with no tool, or one with no answer', () => {
    expect(turnWork(entries(), 'r1')).toEqual([]);
    expect(turnWork([{ id: 'u', kind: 'owner', text: 'go', time: '' }, { id: 'a', kind: 'text', time: '', text: 'hi' }] as Entry[])).toEqual([]);
    expect(turnWork(entries().slice(0, 3))).toEqual([]);
  });
});

describe('Worked for toggle', () => {
  it('starts collapsed for a finished turn and expands on click', () => {
    view(entries());
    const toggle = screen.getByRole('button', { name: 'Worked for 1m 39s' });
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByRole('button', { name: /Ran 2 commands/ })).toBeNull();
    expect(screen.getByText('The answer.')).toBeTruthy();
    fireEvent.click(toggle);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByRole('button', { name: /Ran 2 commands/ })).toBeTruthy();
  });
  it('shows the work and no toggle while the turn is live', () => {
    view(entries().slice(0, 3), 'r1');
    expect(screen.queryByRole('button', { name: /Worked/ })).toBeNull();
    expect(screen.getByRole('button', { name: /Ran 2 commands|Running/ })).toBeTruthy();
  });
  it('keeps a denial visible inside collapsed work, in sentence case', () => {
    const e = entries();
    e.splice(2, 0, { id: 'd', kind: 'denial', runId: 'r1', time: at(1), text: 'rm -rf refused' });
    view(e);
    expect(screen.getByText('Denied')).toBeTruthy();
    expect(screen.queryByText('DENIED')).toBeNull();
  });
});

describe('tool row metadata', () => {
  it('reads status and duration on the right, and marks a failed run', () => {
    const e = entries();
    view(e);
    fireEvent.click(screen.getByRole('button', { name: /Worked/ }));
    expect(screen.getByRole('button', { name: /Ran 2 commands.*completed · 99s|Ran 2 commands.*completed/ }).textContent).toContain('completed');
  });
  it('counts errors as the status', () => {
    const e = entries();
    e[2] = tool('t2', 2, { result: 'error: boom', resultError: true });
    view(e);
    fireEvent.click(screen.getByRole('button', { name: /Worked/ }));
    expect(screen.getByRole('button', { name: /Ran 2 commands/ }).textContent).toContain('1 error');
  });
});
