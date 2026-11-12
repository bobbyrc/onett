import { describe, expect, it } from 'vitest';
import type { Trigger } from '../types';
import { defaultPreset, type SchedulePreset } from './triggerSchedule';
import {
  createBody,
  firesSentence,
  formProblem,
  initialValues,
  patchBody,
  scheduleProblem,
  timeZoneChoices,
  type TriggerFormValues,
} from './triggerForm';

function trig(over: Partial<Trigger> = {}): Trigger {
  return {
    id: 't1',
    name: 'Nightly',
    kind: 'schedule',
    project: 'onett',
    prompt: 'do the thing',
    enabled: true,
    cron: '30 9 * * 1-5',
    time_zone: 'America/New_York',
    created_at: '2026-11-01T00:00:00Z',
    updated_at: '2026-11-01T00:00:00Z',
    ...over,
  };
}

function values(over: Partial<TriggerFormValues> = {}): TriggerFormValues {
  return {
    name: 'Nightly',
    project: 'onett',
    kind: 'schedule',
    preset: defaultPreset(),
    timeZone: '',
    label: '',
    prompt: 'do the thing',
    enabled: true,
    ...over,
  };
}

const withPreset = (over: Partial<SchedulePreset>): SchedulePreset => ({ ...defaultPreset(), ...over });

describe('initialValues', () => {
  it('reads an edited schedule trigger into the form', () => {
    const v = initialValues(trig(), undefined, 'Europe/Paris', 'home-lab');
    expect(v).toMatchObject({
      name: 'Nightly',
      project: 'onett',
      kind: 'schedule',
      prompt: 'do the thing',
      enabled: true,
      timeZone: 'America/New_York',
      label: '',
    });
    expect(v.preset).toMatchObject({ repeat: 'days', time: '09:30', days: [1, 2, 3, 4, 5] });
  });

  it('uses the default schedule for an edited issue trigger, and keeps its label', () => {
    const v = initialValues(trig({ kind: 'github_issue', cron: undefined, time_zone: undefined, label: 'bug' }), undefined, '', '');
    expect(v.kind).toBe('github_issue');
    expect(v.preset).toEqual(defaultPreset());
    expect(v.timeZone).toBe('');
    expect(v.label).toBe('bug');
  });

  it('starts a new trigger with the first project, the browser zone and enabled on', () => {
    const v = initialValues(undefined, undefined, 'Europe/Paris', 'home-lab');
    expect(v).toEqual({
      name: '',
      project: 'home-lab',
      kind: 'schedule',
      preset: defaultPreset(),
      timeZone: 'Europe/Paris',
      label: '',
      prompt: '',
      enabled: true,
    });
  });

  it('takes the kind, prompt and schedule of a starter', () => {
    const v = initialValues(undefined, { kind: 'schedule', prompt: 'Summarise.', cron: '0 5 * * *' }, '', 'onett');
    expect(v.prompt).toBe('Summarise.');
    expect(v.preset).toMatchObject({ repeat: 'daily', time: '05:00' });

    const issue = initialValues(undefined, { kind: 'github_issue', prompt: 'Triage.' }, '', 'onett');
    expect(issue.kind).toBe('github_issue');
    expect(issue.preset).toEqual(defaultPreset());
  });
});

describe('scheduleProblem', () => {
  it('is null for a schedule the form can write', () => {
    expect(scheduleProblem(defaultPreset())).toBeNull();
  });

  it('asks for a day when none is chosen', () => {
    expect(scheduleProblem(withPreset({ repeat: 'days', days: [] }))).toBe('Choose at least one day.');
  });

  it('asks for the time as HH:MM for daily and days', () => {
    const want = 'Write the time as HH:MM, for example 09:30.';
    expect(scheduleProblem(withPreset({ repeat: 'daily', time: '9.30' }))).toBe(want);
    expect(scheduleProblem(withPreset({ repeat: 'days', days: [1], time: '25:00' }))).toBe(want);
  });

  it('describes the five cron fields for custom', () => {
    expect(scheduleProblem(withPreset({ repeat: 'custom', cron: '0 5 *' }))).toBe(
      'A cron expression has five fields: minute, hour, day of month, month, day of week.',
    );
  });

  it('asks for a step the form offers for hours and minutes', () => {
    expect(scheduleProblem(withPreset({ repeat: 'hours', every: 5 }))).toBe('Choose how often it repeats.');
    expect(scheduleProblem(withPreset({ repeat: 'minutes', every: 7 }))).toBe('Choose how often it repeats.');
  });
});

describe('formProblem', () => {
  it('is null for a complete form', () => {
    expect(formProblem(values())).toBeNull();
  });

  it('asks for a name first, even when the project is also missing', () => {
    expect(formProblem(values({ name: '   ', project: '' }))).toBe('A name is required.');
  });

  it('asks for a project next', () => {
    expect(formProblem(values({ project: '' }))).toBe('Choose a project first.');
  });

  it('gives the schedule problem for a schedule trigger', () => {
    expect(formProblem(values({ preset: withPreset({ repeat: 'days', days: [] }) }))).toBe('Choose at least one day.');
  });

  it('does not check the schedule for an issue trigger', () => {
    expect(formProblem(values({ kind: 'github_issue', preset: withPreset({ repeat: 'days', days: [] }) }))).toBeNull();
  });

  it('asks for a prompt last', () => {
    expect(formProblem(values({ prompt: '  ' }))).toBe('A prompt is required.');
  });
});

describe('createBody', () => {
  it('sends the trimmed fields and the cron and zone of a schedule', () => {
    expect(createBody(values({ name: ' Nightly ', prompt: ' do it ', timeZone: 'UTC', label: 'bug' }))).toEqual({
      name: 'Nightly',
      kind: 'schedule',
      project: 'onett',
      prompt: 'do it',
      enabled: true,
      cron: '0 9 * * *',
      time_zone: 'UTC',
    });
  });

  it('leaves out the zone when the daemon zone is chosen', () => {
    expect(createBody(values())).not.toHaveProperty('time_zone');
  });

  it('sends the label of an issue trigger, and never the schedule fields', () => {
    const body = createBody(values({ kind: 'github_issue', label: ' bug ', timeZone: 'UTC' }));
    expect(body).toEqual({ name: 'Nightly', kind: 'github_issue', project: 'onett', prompt: 'do the thing', enabled: true, label: 'bug' });
    expect(body).not.toHaveProperty('cron');
    expect(body).not.toHaveProperty('time_zone');
  });

  it('leaves out an empty label', () => {
    expect(createBody(values({ kind: 'github_issue', label: '   ' }))).not.toHaveProperty('label');
  });

  it('never sends the label of a schedule', () => {
    expect(createBody(values({ label: 'bug' }))).not.toHaveProperty('label');
  });
});

describe('patchBody', () => {
  it('is empty when nothing changed', () => {
    const t = trig();
    expect(patchBody(t, initialValues(t, undefined, '', ''))).toEqual({});
  });

  it('sends only the tick when only the tick changed', () => {
    const t = trig();
    expect(patchBody(t, { ...initialValues(t, undefined, '', ''), enabled: false })).toEqual({ enabled: false });
  });

  it('sends the name and prompt trimmed, when they changed', () => {
    const t = trig();
    expect(patchBody(t, { ...initialValues(t, undefined, '', ''), name: ' New name ', prompt: 'new' })).toEqual({
      name: 'New name',
      prompt: 'new',
    });
  });

  it('sends a cleared zone as an empty string', () => {
    const t = trig();
    expect(patchBody(t, { ...initialValues(t, undefined, '', ''), timeZone: '' })).toEqual({ time_zone: '' });
  });

  it('sends a changed schedule as its cron', () => {
    const t = trig();
    const v = { ...initialValues(t, undefined, '', ''), preset: withPreset({ repeat: 'daily', time: '06:00' }) };
    expect(patchBody(t, v)).toEqual({ cron: '0 6 * * *' });
  });

  it('sends a changed label, and a cleared one as an empty string', () => {
    const t = trig({ kind: 'github_issue', cron: undefined, time_zone: undefined, label: 'bug' });
    expect(patchBody(t, { ...initialValues(t, undefined, '', ''), label: ' feature ' })).toEqual({ label: 'feature' });
    expect(patchBody(t, { ...initialValues(t, undefined, '', ''), label: '' })).toEqual({ label: '' });
  });

  it('never sends the schedule of an issue trigger', () => {
    const t = trig({ kind: 'github_issue', cron: undefined, time_zone: undefined, label: 'bug' });
    const v = { ...initialValues(t, undefined, '', ''), preset: withPreset({ repeat: 'daily', time: '06:00' }) };
    expect(patchBody(t, v)).toEqual({});
  });
});

describe('firesSentence', () => {
  it('says the days, time and zone in words', () => {
    expect(firesSentence('30 9 * * 1-5', 'America/New_York')).toBe(
      'Fires Monday to Friday at 09:30, America/New_York time.',
    );
  });

  it("names the daemon's zone when none is set, and lowercases the leading Every", () => {
    expect(firesSentence('0 5 * * *', '')).toBe("Fires every day at 05:00, daemon's time.");
  });

  it('falls back to the cron text for a schedule it cannot put in words', () => {
    expect(firesSentence('*/45 * * * *', 'UTC')).toBe('Fires on the cron expression above, UTC time.');
  });
});

describe('timeZoneChoices', () => {
  it('lists the browser zones, sorted, with no empty entry', () => {
    const list = timeZoneChoices('', '');
    expect(list).not.toContain('');
    expect(list).toEqual([...list].sort((a, b) => a.localeCompare(b)));
  });

  it('adds the current and browser zones when the list lacks them, once each', () => {
    const list = timeZoneChoices('Mars/Olympus', 'Mars/Phobos');
    expect(list).toContain('Mars/Olympus');
    expect(list).toContain('Mars/Phobos');
    expect(list.filter((z) => z === 'Mars/Olympus')).toHaveLength(1);
  });

  it('does not repeat a zone the list already has', () => {
    const list = timeZoneChoices('UTC', 'UTC');
    expect(list.filter((z) => z === 'UTC')).toHaveLength(1);
  });
});
