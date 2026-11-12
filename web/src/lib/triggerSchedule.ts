// The schedule form's choices as a five-field cron expression, and back. It only reads the shapes
// this form writes; any other cron text is kept as "custom" and is never parsed further.

export type Repeat = 'daily' | 'days' | 'hours' | 'minutes' | 'custom';

/** The schedule form's state. Days are 1 = Monday to 7 = Sunday, sorted, without repeats. */
export interface SchedulePreset {
  repeat: Repeat;
  time: string; // "HH:MM", used by daily and days
  days: number[]; // used by days
  every: number; // the step, used by hours and minutes
  cron: string; // the expression as typed, used by custom
}

export const HOUR_STEPS = [1, 2, 3, 4, 6, 8, 12];
export const MINUTE_STEPS = [5, 10, 15, 20, 30];
export const DAY_NAMES = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']; // index 0 is day 1

const DAY_FULL_NAMES = [
  'Monday',
  'Tuesday',
  'Wednesday',
  'Thursday',
  'Friday',
  'Saturday',
  'Sunday',
];

const TIME = /^([01]?\d|2[0-3]):([0-5]\d)$/;

/** The form's starting values: every weekday at 09:00, with 6 as the first step offered. */
export function defaultPreset(): SchedulePreset {
  return { repeat: 'daily', time: '09:00', days: [1, 2, 3, 4, 5], every: 6, cron: '' };
}

/** The cron expression for the form's choices, or null while the form is not valid yet. */
export function presetToCron(p: SchedulePreset): string | null {
  switch (p.repeat) {
    case 'daily': {
      const c = parseClock(p.time);
      return c ? `${c.minute} ${c.hour} * * *` : null;
    }
    case 'days': {
      const c = parseClock(p.time);
      if (!c || p.days.length === 0) return null;
      if (p.days.length === 7) return `${c.minute} ${c.hour} * * *`;
      // Cron counts Sunday as 0, so the sort and the range check use cron numbers.
      const nums = p.days.map((d) => (d === 7 ? 0 : d)).sort((a, b) => a - b);
      const consecutive =
        nums.length >= 3 && nums.every((n, i) => i === 0 || n === nums[i - 1] + 1);
      const list = consecutive ? `${nums[0]}-${nums[nums.length - 1]}` : nums.join(',');
      return `${c.minute} ${c.hour} * * ${list}`;
    }
    case 'hours':
      if (!HOUR_STEPS.includes(p.every)) return null;
      return p.every === 1 ? '0 * * * *' : `0 */${p.every} * * *`;
    case 'minutes':
      return MINUTE_STEPS.includes(p.every) ? `*/${p.every} * * * *` : null;
    case 'custom': {
      const fields = p.cron.trim().split(/\s+/);
      return fields.length === 5 ? fields.join(' ') : null;
    }
  }
}

/** The form's state for a cron expression. Anything outside the shapes the form writes is custom. */
export function cronToPreset(cron: string): SchedulePreset {
  const normal = cron.trim().split(/\s+/).join(' ');
  const base = { ...defaultPreset(), cron: normal };

  if (normal === '0 * * * *') return { ...base, repeat: 'hours', every: 1 };

  const hourStep = /^0 \*\/(\d{1,2}) \* \* \*$/.exec(normal);
  if (hourStep && HOUR_STEPS.includes(Number(hourStep[1]))) {
    return { ...base, repeat: 'hours', every: Number(hourStep[1]) };
  }

  const minuteStep = /^\*\/(\d{1,2}) \* \* \* \*$/.exec(normal);
  if (minuteStep && MINUTE_STEPS.includes(Number(minuteStep[1]))) {
    return { ...base, repeat: 'minutes', every: Number(minuteStep[1]) };
  }

  const daily = /^(\d{1,2}) (\d{1,2}) \* \* \*$/.exec(normal);
  if (daily) {
    const time = clockText(daily[1], daily[2]);
    if (time) return { ...base, repeat: 'daily', time };
  }

  const weekly = /^(\d{1,2}) (\d{1,2}) \* \* ([0-7](?:-[0-7])?(?:,[0-7](?:-[0-7])?)*)$/.exec(normal);
  if (weekly) {
    const time = clockText(weekly[1], weekly[2]);
    const days = time ? parseDayList(weekly[3]) : null;
    if (time && days) return { ...base, repeat: 'days', time, days };
  }

  return { ...base, repeat: 'custom' };
}

/** The schedule in words, such as "Monday to Friday at 09:30", or null for custom cron text. */
export function describeCron(cron: string): string | null {
  const p = cronToPreset(cron);
  switch (p.repeat) {
    case 'custom':
      return null;
    case 'daily':
      return `Every day at ${p.time}`;
    case 'hours':
      return p.every === 1 ? 'Every hour' : `Every ${p.every} hours`;
    case 'minutes':
      return `Every ${p.every} minutes`;
    case 'days':
      return `${dayWords(p.days)} at ${p.time}`;
  }
}

function parseClock(time: string): { hour: number; minute: number } | null {
  const m = TIME.exec(time);
  return m ? { hour: Number(m[1]), minute: Number(m[2]) } : null;
}

// Cron writes hour and minute as plain numbers; the form shows them as "HH:MM".
function clockText(minute: string, hour: string): string | null {
  const m = Number(minute);
  const h = Number(hour);
  if (m > 59 || h > 23) return null;
  return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`;
}

// The regex has already checked the shape; this expands ranges and maps cron's 0 to Sunday (7).
function parseDayList(list: string): number[] | null {
  const days = new Set<number>();
  for (const item of list.split(',')) {
    const parts = item.split('-');
    const from = Number(parts[0]);
    const to = parts.length === 2 ? Number(parts[1]) : from;
    if (from > to) return null;
    for (let d = from; d <= to; d++) days.add(d === 0 ? 7 : d);
  }
  return [...days].sort((a, b) => a - b);
}

function dayWords(days: number[]): string {
  if (days.length === 7) return 'Every day';
  const plural = (d: number) => `${DAY_FULL_NAMES[d - 1]}s`;
  if (days.length === 1) return plural(days[0]);
  const consecutive = days.length >= 3 && days.every((d, i) => i === 0 || d === days[i - 1] + 1);
  if (consecutive) {
    return `${DAY_FULL_NAMES[days[0] - 1]} to ${DAY_FULL_NAMES[days[days.length - 1] - 1]}`;
  }
  const names = days.map(plural);
  return `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`;
}
