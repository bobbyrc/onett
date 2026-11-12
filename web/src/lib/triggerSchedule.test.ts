import { describe, expect, it } from 'vitest';
import {
  cronToPreset,
  defaultPreset,
  describeCron,
  presetToCron,
  type SchedulePreset,
} from './triggerSchedule';

const daysAt = (time: string, days: number[]): SchedulePreset => ({
  ...defaultPreset(),
  repeat: 'days',
  time,
  days,
});

describe('presetToCron', () => {
  it.each<[string, SchedulePreset, string | null]>([
    ['daily 05:00', { ...defaultPreset(), repeat: 'daily', time: '05:00' }, '0 5 * * *'],
    ['daily 9:30 without a leading zero', { ...defaultPreset(), repeat: 'daily', time: '9:30' }, '30 9 * * *'],
    ['daily 24:00 is not a time', { ...defaultPreset(), repeat: 'daily', time: '24:00' }, null],
    ['daily 9.30 is not a time', { ...defaultPreset(), repeat: 'daily', time: '9.30' }, null],
    ['days Monday to Friday', daysAt('09:30', [1, 2, 3, 4, 5]), '30 9 * * 1-5'],
    ['days Monday and Thursday', daysAt('09:30', [1, 4]), '30 9 * * 1,4'],
    ['days Friday only', daysAt('09:30', [5]), '30 9 * * 5'],
    ['days Saturday and Sunday', daysAt('09:30', [6, 7]), '30 9 * * 0,6'],
    ['days three consecutive', daysAt('09:30', [1, 2, 3]), '30 9 * * 1-3'],
    ['days two consecutive stay a list', daysAt('09:30', [1, 2]), '30 9 * * 1,2'],
    ['days Friday to Sunday, Sunday is 0', daysAt('09:30', [5, 6, 7]), '30 9 * * 0,5,6'],
    ['days all seven is daily', daysAt('09:30', [1, 2, 3, 4, 5, 6, 7]), '30 9 * * *'],
    ['days none chosen', daysAt('09:30', []), null],
    ['days with an invalid time', daysAt('25:00', [1]), null],
    ['hours every 1', { ...defaultPreset(), repeat: 'hours', every: 1 }, '0 * * * *'],
    ['hours every 6', { ...defaultPreset(), repeat: 'hours', every: 6 }, '0 */6 * * *'],
    ['hours step not offered', { ...defaultPreset(), repeat: 'hours', every: 5 }, null],
    ['minutes every 15', { ...defaultPreset(), repeat: 'minutes', every: 15 }, '*/15 * * * *'],
    ['minutes step not offered', { ...defaultPreset(), repeat: 'minutes', every: 7 }, null],
    ['custom trims and collapses spaces', { ...defaultPreset(), repeat: 'custom', cron: '  0   5 * * * ' }, '0 5 * * *'],
    ['custom with four fields', { ...defaultPreset(), repeat: 'custom', cron: '0 5 * *' }, null],
    ['custom empty', { ...defaultPreset(), repeat: 'custom', cron: '' }, null],
  ])('%s', (_name, preset, expected) => {
    expect(presetToCron(preset)).toBe(expected);
  });
});

describe('cronToPreset', () => {
  it.each([
    ['0 5 * * *', { repeat: 'daily', time: '05:00' }],
    ['30 9 * * 1-5', { repeat: 'days', time: '09:30', days: [1, 2, 3, 4, 5] }],
    ['0 8 * * 1,4', { repeat: 'days', time: '08:00', days: [1, 4] }],
    ['0 10 * * 0,6', { repeat: 'days', time: '10:00', days: [6, 7] }],
    ['0 10 * * 7', { repeat: 'days', time: '10:00', days: [7] }],
    ['0 */6 * * *', { repeat: 'hours', every: 6 }],
    ['0 * * * *', { repeat: 'hours', every: 1 }],
    ['*/15 * * * *', { repeat: 'minutes', every: 15 }],
    ['  0   5 * * * ', { repeat: 'daily', time: '05:00' }],
  ])('reads %j as the shape the form writes', (cron, expected) => {
    expect(cronToPreset(cron)).toEqual({ ...defaultPreset(), ...expected, cron: cron.trim().split(/\s+/).join(' ') });
  });

  it.each([
    '*/45 * * * *',
    '0 5 1 * *',
    '0 9 * * 1-5/2',
    '0 9 * * mon',
    '',
  ])('keeps %j as custom', (cron) => {
    expect(cronToPreset(cron)).toEqual({ ...defaultPreset(), repeat: 'custom', cron: cron.trim().split(/\s+/).join(' ') });
  });
});

describe('describeCron', () => {
  it.each([
    ['0 5 * * *', 'Every day at 05:00'],
    ['30 9 * * 1-5', 'Monday to Friday at 09:30'],
    ['0 16 * * 5', 'Fridays at 16:00'],
    ['0 8 * * 1,4', 'Mondays and Thursdays at 08:00'],
    ['0 10 * * 0,6', 'Saturdays and Sundays at 10:00'],
    ['0 9 * * 1,3,5', 'Mondays, Wednesdays and Fridays at 09:00'],
    ['0 */6 * * *', 'Every 6 hours'],
    ['0 * * * *', 'Every hour'],
    ['*/15 * * * *', 'Every 15 minutes'],
    ['*/45 * * * *', null],
  ])('describes %j', (cron, expected) => {
    expect(describeCron(cron)).toBe(expected);
  });
});

describe('round trip', () => {
  const presets: SchedulePreset[] = [
    { ...defaultPreset(), repeat: 'daily', time: '07:45' },
    daysAt('18:05', [2, 4]),
    daysAt('06:00', [1, 2, 3, 4, 5]),
    { ...defaultPreset(), repeat: 'hours', every: 8 },
    { ...defaultPreset(), repeat: 'minutes', every: 10 },
  ];

  it.each(presets.map((p) => [p.repeat, p] as const))('%s survives presetToCron then cronToPreset', (_repeat, p) => {
    const back = cronToPreset(presetToCron(p)!);
    expect(back.repeat).toBe(p.repeat);
    if (p.repeat === 'daily') expect(back.time).toBe(p.time);
    if (p.repeat === 'days') {
      expect(back.time).toBe(p.time);
      expect(back.days).toEqual(p.days);
    }
    if (p.repeat === 'hours' || p.repeat === 'minutes') expect(back.every).toBe(p.every);
  });
});
