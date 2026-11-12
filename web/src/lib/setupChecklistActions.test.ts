import { describe, expect, it } from 'vitest';
import { OWNER_UNREACHABLE_MESSAGE } from './ownerUnreachable';
import { setupSteps } from './setupChecklist';

const full = { projects: [{ name: 'app', policy: '' }] };

describe('setupSteps actions', () => {
  it('gives each step the label of its button', () => {
    const steps = setupSteps({
      ...full,
      agents: [{ name: 'coder', disabled: false, harness_cli_found: true }],
      stats: { owner_unreachable: false },
      secrets: [{ name: 'A', set: true, referenced_by: ['x'] }],
    });
    expect(steps.map((s) => [s.id, s.action])).toEqual([
      ['project', 'Add a project'],
      ['agents', 'Open agents'],
      ['policy', 'Choose a policy'],
      ['reach', 'Set up a channel'],
      ['secrets', 'Set secrets'],
    ]);
  });

  it('the policy step waits only when there is no project', () => {
    const none = setupSteps({ projects: [] }).find((s) => s.id === 'policy');
    expect(none?.waits).toBe(true);
    expect(none?.done).toBe(false);
    const some = setupSteps(full).find((s) => s.id === 'policy');
    expect(some?.waits).toBeFalsy();
  });

  it('the reach step says what the owner will not hear about when no channel is set', () => {
    const reach = setupSteps({ stats: { owner_unreachable: true } }).find((s) => s.id === 'reach');
    expect(reach?.detail).toBe(OWNER_UNREACHABLE_MESSAGE);
    expect(reach?.done).toBe(false);
  });
});
