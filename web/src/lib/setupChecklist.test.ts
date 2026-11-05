import { describe, expect, it } from 'vitest';
import { setupIncomplete, setupSteps } from './setupChecklist';

const secret = (name: string, set: boolean, refs: string[] = ['x']) => ({ name, set, referenced_by: refs });

describe('setupSteps', () => {
  it('leaves out steps whose data is missing', () => {
    expect(setupSteps({})).toEqual([]);
    expect(setupSteps({ stats: { owner_unreachable: false } }).map((s) => s.id)).toEqual(['reach']);
  });

  it('shows everything unchecked on a fresh install', () => {
    const steps = setupSteps({
      projects: [],
      agents: [{ name: 'coder', disabled: true }],
      stats: { owner_unreachable: true },
      secrets: [secret('TELEGRAM_BOT_TOKEN', false)],
    });
    expect(steps.map((s) => [s.id, s.done])).toEqual([
      ['project', false],
      ['agents', false],
      ['policy', false],
      ['reach', false],
      ['secrets', false],
    ]);
    expect(steps.find((s) => s.id === 'secrets')?.detail).toContain('TELEGRAM_BOT_TOKEN');
    expect(setupIncomplete(steps)).toBe(true);
  });

  it('checks each step off as it is done, and the default policy counts', () => {
    const steps = setupSteps({
      projects: [{ name: 'app', policy: '' }],
      agents: [{ name: 'coder', disabled: false }],
      stats: { owner_unreachable: false },
      secrets: [secret('A', true), secret('B', false, [])],
    });
    expect(steps.every((s) => s.done)).toBe(true);
    expect(setupIncomplete(steps)).toBe(false);
  });

  it('is not incomplete before anything loads', () => {
    expect(setupIncomplete(setupSteps({}))).toBe(false);
  });
});
