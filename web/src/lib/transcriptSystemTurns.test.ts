import { describe, expect, it } from 'vitest';
import type { ChatMessage, LogLine } from '../types';
import { isInternalLine, orchestratorEntries, type Entry } from './transcript';

const msg = (over: Partial<ChatMessage>): ChatMessage => ({
  id: 'm',
  conversation_id: 'c',
  role: 'assistant',
  text: '',
  created_at: '2026-01-01T00:00:00Z',
  ...over,
});
const log = (seq: number, kind: LogLine['kind'], text: string, run_id = 'r1'): LogLine => ({
  run_id,
  seq,
  time: '2026-01-01T00:00:00Z',
  kind,
  text,
});

describe('a conversation the system started', () => {
  it('shows the replies and a marker when there is no owner message', () => {
    const messages = [
      msg({ id: 's', role: 'system', kind: 'internal', run_id: 'r1', text: '[onett] goal wake-up' }),
      msg({ id: 'a', role: 'assistant', run_id: 'r1', text: 'Done.' }),
    ];
    const logs = { r1: [log(1, 'system', 'session started (model x)'), log(2, 'text', 'Done.')] };
    const e = orchestratorEntries(messages, logs);
    expect(e.map((x) => x.kind)).toEqual(['divider', 'system', 'text']);
    expect(e[0]).toMatchObject({ text: 'Automatic update' });
    expect(e[2]).toMatchObject({ text: 'Done.' });
  });

  it('shows each reply once, and a reply whose log is not loaded as plain text', () => {
    const messages = [
      msg({ id: 'a', run_id: 'r1', text: 'one' }),
      msg({ id: 'b', run_id: 'r1', text: 'one again' }),
      msg({ id: 'c', run_id: 'r2', text: 'two' }),
    ];
    const e = orchestratorEntries(messages, { r1: [log(1, 'text', 'one')] });
    expect(e.map((x) => (x as Extract<Entry, { kind: 'text' }>).text)).toEqual(['one', 'two']);
  });
});

describe('isInternalLine', () => {
  const sys = (text: string): Entry => ({ id: 'x', kind: 'system', text, time: '' });
  it('matches the session and reattach lines only', () => {
    expect(isInternalLine(sys('session started (model claude-opus-5-5)'))).toBe(true);
    expect(isInternalLine(sys('session started'))).toBe(true);
    expect(isInternalLine(sys('onett restarted; reattached to the agent at stream offset 0'))).toBe(true);
    expect(isInternalLine(sys('Run ended: denied'))).toBe(false);
    expect(isInternalLine({ id: 'y', kind: 'text', text: 'session started', time: '' })).toBe(false);
  });
});
