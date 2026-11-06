// @vitest-environment jsdom
import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { Entry } from '../lib/transcript';
import { Transcript } from './Transcript';

const call = (id: string, runId: string): Entry => ({
  id,
  kind: 'tool',
  time: '',
  runId,
  tool: { name: 'Bash', arg: '' },
  input: '',
  result: 'ok',
});
// An error entry splits tool groups without ending the turn, so no work is folded away.
const split = (id: string, runId: string): Entry => ({ id, kind: 'error', time: '', runId, text: 'x' });

const metas = (c: HTMLElement) => [...c.querySelectorAll('.tl-meta')].map((m) => m.textContent);

describe('run token totals in tool rows', () => {
  it('shows a run total once, on its last tool row', () => {
    const entries = [call('a', 'r1'), call('b', 'r1'), split('x', 'r1'), call('c', 'r1'), split('y', 'r2'), call('d', 'r2')];
    const { container } = render(
      <Transcript entries={entries} view="conversation" agentLabel="Agent" runTokens={{ r1: 67_500, r2: 900 }} />,
    );
    const m = metas(container);
    expect(m).toHaveLength(3);
    expect(m[0]).toBe('completed'); // r1's first group is not its last tool row
    expect(m[1]).toBe('completed · 67.5k tokens');
    expect(m[2]).toBe('completed · 900 tokens');
  });
  it('shows nothing without totals', () => {
    const { container } = render(<Transcript entries={[call('a', 'r1')]} view="conversation" agentLabel="Agent" />);
    expect(metas(container)).toEqual(['completed']);
  });
});
