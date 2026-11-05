// @vitest-environment jsdom
import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { Entry } from '../lib/transcript';
import { Transcript } from './Transcript';
import { changedFiles, groupToolRuns, liveLabel, summarizeRun, type ToolEntry } from './toolRuns';

let n = 0;
const call = (name: string, arg = '', extra: Partial<ToolEntry> = {}): ToolEntry => ({
  id: `t${++n}`,
  kind: 'tool',
  time: '',
  runId: 'r1',
  tool: { name, arg },
  input: `${name}: {}`,
  result: 'ok',
  ...extra,
});
const text = (t = 'hi'): Entry => ({ id: `x${++n}`, kind: 'text', time: '', text: t });

describe('groupToolRuns', () => {
  it('keeps one call single and folds two or more', () => {
    const one = groupToolRuns([call('Bash')]);
    expect(one).toHaveLength(1);
    expect(one[0].type).toBe('entry');
    const two = groupToolRuns([call('Bash'), call('Read')]);
    expect(two).toHaveLength(1);
    expect(two[0].type === 'run' && two[0].calls).toHaveLength(2);
  });
  it('splits runs at a message or any other entry', () => {
    const blocks = groupToolRuns([call('Bash'), call('Bash'), text(), call('Read'), call('Read'), call('Read'), { id: 'e', kind: 'error', time: '', text: 'x' }, call('Edit')]);
    expect(blocks.map((b) => (b.type === 'run' ? `run${b.calls.length}` : b.entry.kind))).toEqual(['run2', 'text', 'run3', 'error', 'tool']);
  });
});

describe('summarizeRun', () => {
  it('writes a sentence with correct plurals, across Claude and Codex names', () => {
    const calls = [call('Bash'), call('shell'), call('exec_command'), call('Read'), call('Read'), call('MultiEdit'), call('Grep'), call('Glob')];
    expect(summarizeRun(calls)).toBe('Ran 3 commands, read 2 files, edited 1 file, searched 2 times');
    expect(summarizeRun([call('Bash'), call('edit', 'a.go, b.go')])).toBe('Ran 1 command, edited 1 file');
    expect(summarizeRun([call('apply_patch'), call('Write')])).toBe('Edited 2 files');
  });
  it('counts anything else as tools, singular and plural', () => {
    expect(summarizeRun([call('Task'), call('Bash')])).toBe('Ran 1 command, used 1 tool');
    expect(summarizeRun([call('Task'), call('TodoWrite'), call('WebFetch')])).toBe('Used 3 tools');
  });
});

describe('changedFiles', () => {
  it('sums +/- per file and splits Codex path lists', () => {
    const files = changedFiles([
      call('Edit', 'a.go', { tool: { name: 'Edit', arg: 'a.go', added: 3, removed: 1 } }),
      call('Edit', 'a.go', { tool: { name: 'Edit', arg: 'a.go', added: 2, removed: 4 } }),
      call('Write', 'b.go'),
      call('edit', 'c.go, d.go'),
      call('Read', 'e.go'),
    ]);
    expect(files).toEqual([
      { path: 'a.go', added: 5, removed: 5 },
      { path: 'b.go', added: 0, removed: 0 },
      { path: 'c.go', added: 0, removed: 0 },
      { path: 'd.go', added: 0, removed: 0 },
    ]);
  });
});

const view = (entries: Entry[], liveRunId?: string) =>
  render(<Transcript entries={entries} view="conversation" agentLabel="Orchestrator" liveRunId={liveRunId} />);

describe('ToolRun', () => {
  it('toggles aria-expanded on the run and on each call', () => {
    view([call('Bash', 'go vet'), call('Edit', 'a.go', { tool: { name: 'Edit', arg: 'a.go', added: 2, removed: 1 } })]);
    const head = screen.getByRole('button', { name: /Ran 1 command, edited 1 file/ });
    expect(head).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByText('go vet')).toBeNull();
    fireEvent.click(head);
    expect(head).toHaveAttribute('aria-expanded', 'true');
    const lines = screen.getAllByRole('button', { name: /Bash|Edit/ }).filter((b) => b !== head);
    expect(lines).toHaveLength(2);
    expect(lines[0]).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(lines[0]);
    expect(lines[0]).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText('Bash: {}')).toBeInTheDocument();
  });
  it('shows +/- totals and the changed-file list once expanded', () => {
    view([
      call('Edit', 'a.go', { tool: { name: 'Edit', arg: 'a.go', added: 3, removed: 1 } }),
      call('Edit', 'b.go', { tool: { name: 'Edit', arg: 'b.go', added: 2, removed: 4 } }),
    ]);
    const head = screen.getByRole('button', { name: /Edited 2 files/ });
    expect(head).toHaveTextContent('+5 −5');
    expect(head).toHaveTextContent('2 files');
    fireEvent.click(head);
    const list = screen.getByRole('list', { name: 'Changed files' });
    expect(within(list).getAllByRole('listitem')).toHaveLength(2);
    expect(list).toHaveTextContent('+3 −1');
    expect(list).toHaveTextContent('+2 −4');
  });
  it('names the latest call while live and settles into the summary', () => {
    const entries = [call('Read', 'a.go'), call('Bash', 'go test ./...', { result: undefined })];
    const { rerender } = view(entries);
    const live = screen.getByText('Running go test ./...');
    expect(live).toHaveAttribute('aria-live', 'polite');
    entries[1] = { ...entries[1], result: 'PASS' } as ToolEntry;
    rerender(<Transcript entries={[...entries]} view="conversation" agentLabel="x" />);
    expect(screen.getByText('Ran 1 command, read 1 file')).toBeInTheDocument();
  });
  it('is not live when another run is the live one', () => {
    view([call('Read'), call('Bash', 'x', { result: undefined })], 'other');
    expect(screen.getByText('Ran 1 command, read 1 file')).toBeInTheDocument();
  });
  it('shows errors as text', () => {
    view([call('Bash', 'x', { resultError: true }), call('Bash', 'y')]);
    expect(screen.getByText('1 error')).toBeInTheDocument();
  });
  it('renders a single call as a plain tool line', () => {
    view([call('Bash', 'ls')]);
    expect(screen.getByRole('button', { name: /Bash/ })).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('button', { name: /Ran 1/ })).toBeNull();
  });
});

describe('liveLabel', () => {
  it('uses a verb per kind', () => {
    expect(liveLabel(call('Grep', 'foo'))).toBe('Searching foo');
    expect(liveLabel(call('Task'))).toBe('Task');
  });
});
