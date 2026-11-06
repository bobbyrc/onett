import { describe, expect, it } from 'vitest';
import type { LogLine } from '../types';
import { runEntries } from './transcript';

const progress =
  '{"type":"tool_progress","tool_use_id":"toolu_1-heartbeat-0","tool_name":"Bash","elapsed_time_seconds":30,"heartbeat":true}';

function l(seq: number, kind: LogLine['kind'], text: string, tool_id?: string): LogLine {
  return { run_id: 'r1', seq, time: '2026-01-01T00:00:00Z', kind, text, tool_id };
}

describe('raw stream events', () => {
  it('are not shown as transcript entries', () => {
    const entries = runEntries(
      [
        l(1, 'tool_use', 'Bash: {"command":"sleep 60"}', 't1'),
        l(2, 'system', progress),
        l(3, 'system', progress.slice(0, 60)), // truncated by the parser
        l(4, 'system', 'session started'),
        l(5, 'tool_result', 'done', 't1'),
      ],
      'r1',
    );
    expect(entries.filter((e) => e.kind === 'system').map((e) => 'text' in e && e.text)).toEqual(['session started']);
    expect(JSON.stringify(entries)).not.toContain('tool_progress');
  });
});
