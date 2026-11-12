// View logic for the Triggers page: list order, the words and lamp on each card, and how a firing
// is described. Pure functions only; the page decides where they show.
import type { Tone } from './status';
import type { Trigger, TriggerFiring, TriggerFiringOutcome } from '../types';
import { formatRelative, parseTime } from './time';

export type TriggerLamp = 'run' | 'call' | 'stop' | 'off';

/** A card's lamp and the words beside it: `strong` says what lit the lamp, `rest` the quieter facts after it. */
export interface TriggerState {
  lamp: TriggerLamp;
  strong: string;
  rest: string[];
}

/** A copy sorted for the list: by name (localeCompare), with switched-off triggers last. */
export function sortTriggers(list: Trigger[]): Trigger[] {
  return [...list].sort(
    (a, b) => Number(!a.enabled) - Number(!b.enabled) || a.name.localeCompare(b.name),
  );
}

/** How many triggers are on, off, and on with a failed last firing. */
export function triggerCounts(list: Trigger[]): { on: number; off: number; failing: number } {
  const on = list.filter((t) => t.enabled).length;
  const failing = list.filter((t) => t.enabled && t.last_firing?.outcome === 'failed').length;
  return { on, off: list.length - on, failing };
}

/** A card's lamp and its words, from whether the trigger is on and how its last firing went. */
export function triggerState(t: Trigger, now: number): TriggerState {
  if (!t.enabled) return { lamp: 'off', strong: 'Off', rest: [] };

  const at = t.kind === 'schedule' ? parseTime(t.next_run_at) : null;
  const next = at ? `next ${formatRelative(at, now)}` : null;
  const withNext = (...lead: string[]): string[] => (next ? [...lead, next] : lead);

  const last = t.last_firing;
  if (last?.outcome === 'failed') {
    return { lamp: 'stop', strong: 'Last firing failed', rest: withNext() };
  }
  if (last?.outcome === 'skipped') {
    return { lamp: 'call', strong: 'Skipped its last firing', rest: withNext() };
  }
  if (!last) return { lamp: 'off', strong: 'On', rest: withNext('not fired yet') };
  if (t.kind === 'github_issue') {
    return { lamp: 'run', strong: 'On', rest: ['waits for the next issue'] };
  }
  return { lamp: 'run', strong: 'On', rest: withNext() };
}

/** The one-word status on the trigger's own page, and its pill tone. */
export function triggerStatus(t: Trigger): { word: string; tone: Tone } {
  if (!t.enabled) return { word: 'Off', tone: 'muted' };
  const last = t.last_firing;
  if (!last) return { word: 'Not fired yet', tone: 'muted' };
  switch (last.outcome) {
    case 'started':
      return { word: 'OK', tone: 'info' };
    case 'skipped':
      return { word: 'Skipped', tone: 'warn' };
    case 'failed':
      return { word: 'Failed', tone: 'danger' };
  }
}

/** The word for one firing's outcome. */
export function outcomeWord(o: TriggerFiringOutcome): string {
  switch (o) {
    case 'started':
      return 'Started';
    case 'skipped':
      return 'Skipped';
    case 'failed':
      return 'Failed';
  }
}

/** The pill tone for one firing's outcome. */
export function outcomeTone(o: TriggerFiringOutcome): Tone {
  switch (o) {
    case 'started':
      return 'info';
    case 'skipped':
      return 'warn';
    case 'failed':
      return 'danger';
  }
}

/** The lamp colour for one firing's outcome. */
export function outcomeLamp(o: TriggerFiringOutcome): 'ok' | 'warn' | 'danger' {
  switch (o) {
    case 'started':
      return 'ok';
    case 'skipped':
      return 'warn';
    case 'failed':
      return 'danger';
  }
}

/** Why a firing happened. Short form for lists, long form for the trigger's own page. */
export function firingWhy(f: TriggerFiring, timeZone?: string, long = false): string {
  if (f.cause === 'schedule') {
    const base = long ? 'Scheduled for' : 'scheduled';
    // shortcut: a firing with no readable time shows the bare word, not a time, to keep the line valid.
    const date = parseTime(f.scheduled_for || f.at);
    return date ? `${base} ${formatHourMinute(date, timeZone)}` : base;
  }
  if (f.cause === 'github_issue') {
    return long ? `Issue #${f.issue_number}` : `issue #${f.issue_number}`;
  }
  const who = (f.by ?? '').replace(/^owner:/, '');
  if (!who) return long ? 'Run now' : 'run now';
  return long ? `Run now by ${who}` : `run now by ${who}`;
}

/** "05:00" in the given IANA zone; the browser's zone when it is empty or not a zone the browser knows. */
export function formatHourMinute(date: Date, timeZone?: string): string {
  const p = partsOf(date, { hour: '2-digit', minute: '2-digit' }, timeZone);
  return `${hourText(p)}:${p.minute}`;
}

/** "Thu 12 Nov 05:00", same zone rule. */
export function formatDayTime(date: Date, timeZone?: string): string {
  const p = partsOf(
    date,
    { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' },
    timeZone,
  );
  return `${p.weekday} ${p.day} ${p.month} ${hourText(p)}:${p.minute}`;
}

/** "Today", "Yesterday" or "Tue 10 Nov", in the browser's zone. */
export function dayLabel(date: Date, now: number): string {
  const today = new Date(now);
  const yesterday = new Date(now);
  yesterday.setDate(today.getDate() - 1);
  if (sameDay(date, today)) return 'Today';
  if (sameDay(date, yesterday)) return 'Yesterday';
  const p = partsOf(date, { weekday: 'short', day: 'numeric', month: 'short' });
  return `${p.weekday} ${p.day} ${p.month}`;
}

/** Firings grouped by day label, keeping their order; a firing with no readable time is left out. */
export function groupByDay(
  firings: TriggerFiring[],
  now: number,
): { label: string; firings: TriggerFiring[] }[] {
  const groups: { label: string; firings: TriggerFiring[] }[] = [];
  for (const f of firings) {
    const date = parseTime(f.at);
    if (!date) continue;
    const label = dayLabel(date, now);
    const last = groups.at(-1);
    if (last?.label === label) last.firings.push(f);
    else groups.push({ label, firings: [f] });
  }
  return groups;
}

/** The zone as the page says it: the name, or "daemon's time zone" when empty. */
export function zoneWords(timeZone: string | undefined): string {
  return timeZone || "daemon's time zone";
}

// Reads the requested date fields as text, in the zone given. An unknown zone makes Intl throw,
// so it falls back to the browser's zone.
function partsOf(
  date: Date,
  fields: Intl.DateTimeFormatOptions,
  timeZone?: string,
): Record<string, string> {
  let parts: Intl.DateTimeFormatPart[];
  try {
    const options = { ...fields, hour12: false, timeZone: timeZone || undefined };
    parts = new Intl.DateTimeFormat('en-GB', options).formatToParts(date);
  } catch {
    parts = new Intl.DateTimeFormat('en-GB', { ...fields, hour12: false }).formatToParts(date);
  }
  return Object.fromEntries(parts.map((p) => [p.type, p.value]));
}

// Some engines write midnight as "24" even with a 24-hour clock; the page shows "00".
function hourText(p: Record<string, string>): string {
  return p.hour === '24' ? '00' : p.hour;
}

function sameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

/** The line shown after "Run now": what the firing it recorded came to. */
export function runNote(name: string, f: TriggerFiring): string {
  const because = f.reason ? `: ${f.reason}` : '';
  switch (f.outcome) {
    case 'started':
      return `Fired ${name}.`;
    case 'skipped':
      return `Skipped ${name}${because}.`;
    case 'failed':
      return `Could not fire ${name}${because}.`;
  }
}
