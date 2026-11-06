import { describe, expect, it } from 'vitest';
import type { LoopView } from '../types';
import { loopLabel, loopStatusWord } from './loopLabel';

const loop = (over: Partial<LoopView> = {}): LoopView => ({
  name: 'issue_intake',
  project: 'onett',
  enabled: true,
  every: '5m',
  last_run_at: '2026-11-05T12:00:00Z',
  ...over,
});

describe('loopLabel', () => {
  it('turns a snake_case id into Title Case words', () => {
    expect(loopLabel('issue_intake')).toBe('Issue Intake');
    expect(loopLabel('self_update')).toBe('Self Update');
    expect(loopLabel('heartbeat')).toBe('Heartbeat');
  });

  it('keeps PR upper case', () => {
    expect(loopLabel('pr_shepherd')).toBe('PR Shepherd');
  });
});

describe('loopStatusWord', () => {
  it('says OK after a clean run', () => {
    expect(loopStatusWord(loop())).toBe('OK');
  });

  it('says Failed when the last run left an error', () => {
    expect(loopStatusWord(loop({ last_error: 'boom' }))).toBe('Failed');
  });

  it('says Off for a disabled loop, whatever its last error', () => {
    expect(loopStatusWord(loop({ enabled: false, last_error: 'boom' }))).toBe('Off');
  });

  it('says Not run yet for an enabled loop with no run, including Go zero time', () => {
    expect(loopStatusWord(loop({ last_run_at: '' }))).toBe('Not run yet');
    expect(loopStatusWord(loop({ last_run_at: '0001-01-01T00:00:00Z' }))).toBe('Not run yet');
  });
});
