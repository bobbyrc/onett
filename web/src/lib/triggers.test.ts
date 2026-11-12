import { describe, expect, it } from 'vitest';
import type { Trigger, TriggerFiring } from '../types';
import { formatRelative } from './time';
import {
  dayLabel,
  firingWhy,
  formatDayTime,
  formatHourMinute,
  groupByDay,
  outcomeLamp,
  outcomeTone,
  outcomeWord,
  sortTriggers,
  triggerCounts,
  triggerState,
  triggerStatus,
  zoneWords,
} from './triggers';

const NOW = Date.parse('2026-11-12T09:00:00Z');

function trig(over: Partial<Trigger>): Trigger {
  return {
    id: 't1',
    name: 'Nightly',
    kind: 'schedule',
    project: 'onett',
    prompt: 'do the thing',
    enabled: true,
    created_at: '2026-11-01T00:00:00Z',
    updated_at: '2026-11-01T00:00:00Z',
    ...over,
  };
}

function firing(over: Partial<TriggerFiring>): TriggerFiring {
  return {
    id: 'f1',
    trigger_id: 't1',
    at: '2026-11-12T05:00:00Z',
    cause: 'schedule',
    outcome: 'started',
    ...over,
  };
}

describe('sortTriggers', () => {
  it('puts switched-off triggers last and sorts names within each group', () => {
    const list = [
      trig({ id: 'a', name: 'Zeta', enabled: true }),
      trig({ id: 'b', name: 'Beta', enabled: false }),
      trig({ id: 'c', name: 'Alpha', enabled: true }),
      trig({ id: 'd', name: 'Aardvark', enabled: false }),
    ];
    const before = list.map((t) => t.id);
    expect(sortTriggers(list).map((t) => t.name)).toEqual(['Alpha', 'Zeta', 'Aardvark', 'Beta']);
    expect(list.map((t) => t.id)).toEqual(before);
  });
});

describe('triggerCounts', () => {
  it('counts on, off and on-but-failing triggers', () => {
    const list = [
      trig({ id: 'a', enabled: true, last_firing: firing({ outcome: 'failed' }) }),
      trig({ id: 'b', enabled: true, last_firing: firing({ outcome: 'started' }) }),
      trig({ id: 'c', enabled: false, last_firing: firing({ outcome: 'failed' }) }),
      trig({ id: 'd', enabled: true }),
    ];
    expect(triggerCounts(list)).toEqual({ on: 3, off: 1, failing: 1 });
  });
});

describe('triggerState', () => {
  const next = new Date(NOW + 18 * 3_600_000).toISOString();
  const nextWord = `next ${formatRelative(new Date(next), NOW)}`;

  it('is off when the trigger is switched off', () => {
    expect(triggerState(trig({ enabled: false }), NOW)).toEqual({ lamp: 'off', strong: 'Off', rest: [] });
  });

  it.each([
    ['failed', 'stop', 'Last firing failed', 'failed'],
    ['skipped', 'call', 'Skipped its last firing', 'skipped'],
  ] as const)('shows the %s last firing with the next run', (_name, lamp, strong, outcome) => {
    const t = trig({ next_run_at: next, last_firing: firing({ outcome }) });
    expect(triggerState(t, NOW)).toEqual({ lamp, strong, rest: [nextWord] });
    expect(triggerState({ ...t, next_run_at: undefined }, NOW)).toEqual({ lamp, strong, rest: [] });
  });

  it('says it has not fired yet, with and without a next run', () => {
    expect(triggerState(trig({ next_run_at: next }), NOW)).toEqual({
      lamp: 'off',
      strong: 'On',
      rest: ['not fired yet', nextWord],
    });
    expect(triggerState(trig({}), NOW)).toEqual({ lamp: 'off', strong: 'On', rest: ['not fired yet'] });
  });

  it('is running for a schedule that last started, with its next run', () => {
    const t = trig({ next_run_at: next, last_firing: firing({ outcome: 'started' }) });
    expect(triggerState(t, NOW)).toEqual({ lamp: 'run', strong: 'On', rest: [nextWord] });
    expect(triggerState({ ...t, next_run_at: undefined }, NOW)).toEqual({ lamp: 'run', strong: 'On', rest: [] });
  });

  it('waits for issues for a github_issue trigger', () => {
    const t = trig({ kind: 'github_issue', last_firing: firing({ outcome: 'started', cause: 'github_issue' }) });
    expect(triggerState(t, NOW)).toEqual({ lamp: 'run', strong: 'On', rest: ['waits for the next issue'] });
  });
});

describe('triggerStatus', () => {
  it.each([
    ['off', trig({ enabled: false }), { word: 'Off', tone: 'muted' }],
    ['not fired yet', trig({}), { word: 'Not fired yet', tone: 'muted' }],
    ['started', trig({ last_firing: firing({ outcome: 'started' }) }), { word: 'OK', tone: 'info' }],
    ['skipped', trig({ last_firing: firing({ outcome: 'skipped' }) }), { word: 'Skipped', tone: 'warn' }],
    ['failed', trig({ last_firing: firing({ outcome: 'failed' }) }), { word: 'Failed', tone: 'danger' }],
  ] as const)('reads %s', (_name, t, expected) => {
    expect(triggerStatus(t)).toEqual(expected);
  });
});

describe('outcome helpers', () => {
  it.each([
    ['started', 'Started', 'info', 'ok'],
    ['skipped', 'Skipped', 'warn', 'warn'],
    ['failed', 'Failed', 'danger', 'danger'],
  ] as const)('%s has its word, tone and lamp', (outcome, word, tone, lamp) => {
    expect(outcomeWord(outcome)).toBe(word);
    expect(outcomeTone(outcome)).toBe(tone);
    expect(outcomeLamp(outcome)).toBe(lamp);
  });
});

describe('firingWhy', () => {
  const scheduled = firing({ cause: 'schedule', scheduled_for: '2026-11-12T05:00:00Z' });

  it('gives the schedule time in UTC', () => {
    expect(firingWhy(scheduled, 'UTC')).toBe('scheduled 05:00');
    expect(firingWhy(scheduled, 'UTC', true)).toBe('Scheduled for 05:00');
  });

  it('shows the schedule time in the trigger zone', () => {
    const f = firing({ cause: 'schedule', scheduled_for: '2026-11-12T10:00:00Z' });
    expect(firingWhy(f, 'America/New_York')).toBe('scheduled 05:00');
  });

  it('names the issue for an issue firing', () => {
    const f = firing({ cause: 'github_issue', issue_number: 42 });
    expect(firingWhy(f)).toBe('issue #42');
    expect(firingWhy(f, undefined, true)).toBe('Issue #42');
  });

  it('names the person for a manual run, without the owner prefix', () => {
    const f = firing({ cause: 'manual', by: 'owner:bobbyrc' });
    expect(firingWhy(f)).toBe('run now by bobbyrc');
    expect(firingWhy(f, undefined, true)).toBe('Run now by bobbyrc');
    expect(firingWhy(firing({ cause: 'manual', by: '' }))).toBe('run now');
    expect(firingWhy(firing({ cause: 'manual', by: '' }), undefined, true)).toBe('Run now');
  });
});

describe('formatHourMinute and formatDayTime', () => {
  const date = new Date('2026-11-12T05:00:00Z');

  it('format in the given zone', () => {
    expect(formatHourMinute(date, 'UTC')).toBe('05:00');
    expect(formatDayTime(date, 'UTC')).toBe('Thu 12 Nov 05:00');
  });

  it('shows midnight as 00, not 24', () => {
    expect(formatHourMinute(new Date('2026-11-12T00:00:00Z'), 'UTC')).toBe('00:00');
  });

  it('does not throw for a zone the browser does not know', () => {
    expect(() => formatHourMinute(date, 'Not/AZone')).not.toThrow();
    expect(formatHourMinute(date, 'Not/AZone')).toMatch(/^\d{2}:\d{2}$/);
    expect(formatDayTime(date, 'Not/AZone')).toMatch(/^[A-Z][a-z]{2} \d{1,2} [A-Z][a-z]{2} \d{2}:\d{2}$/);
  });
});

describe('dayLabel and groupByDay', () => {
  const now = new Date(2026, 10, 12, 9, 58).getTime();

  it('labels today, yesterday and older days', () => {
    expect(dayLabel(new Date(2026, 10, 12, 5, 0), now)).toBe('Today');
    expect(dayLabel(new Date(2026, 10, 11, 23, 30), now)).toBe('Yesterday');
    expect(dayLabel(new Date(2026, 10, 10, 5, 0), now)).toBe('Tue 10 Nov');
  });

  it('groups consecutive firings with the same label, in order', () => {
    const firings = [
      firing({ id: 'a', at: new Date(2026, 10, 12, 5, 0).toISOString() }),
      firing({ id: 'b', at: new Date(2026, 10, 12, 9, 0).toISOString() }),
      firing({ id: 'c', at: new Date(2026, 10, 11, 5, 0).toISOString() }),
      firing({ id: 'd', at: new Date(2026, 10, 10, 5, 0).toISOString() }),
    ];
    const groups = groupByDay(firings, now);
    expect(groups.map((g) => g.label)).toEqual(['Today', 'Yesterday', 'Tue 10 Nov']);
    expect(groups.map((g) => g.firings.map((f) => f.id))).toEqual([['a', 'b'], ['c'], ['d']]);
  });
});

describe('zoneWords', () => {
  it('shows the zone name, or the daemon zone when there is none', () => {
    expect(zoneWords('America/New_York')).toBe('America/New_York');
    expect(zoneWords('')).toBe("daemon's time zone");
    expect(zoneWords(undefined)).toBe("daemon's time zone");
  });
});
