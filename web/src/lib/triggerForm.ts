// The trigger dialog's rules, without the screen: what the form starts with, when it may be sent,
// and what it sends. Pure functions only.
import { cronToPreset, defaultPreset, describeCron, presetToCron, type SchedulePreset } from './triggerSchedule';
import type { Trigger, TriggerCreate, TriggerKind, TriggerPatch } from '../types';

/** A starting point for a new trigger: the examples on an empty page. */
export interface TriggerStarter {
  kind: TriggerKind;
  prompt: string;
  cron?: string;
}

/** What the dialog holds while it is open. */
export interface TriggerFormValues {
  name: string;
  project: string;
  kind: TriggerKind;
  preset: SchedulePreset;
  timeZone: string; // '' is the daemon's zone
  label: string;
  prompt: string;
  enabled: boolean;
}

/** The form's first values: an edited trigger's own, or a new trigger's defaults. */
export function initialValues(
  trigger: Trigger | undefined,
  starter: TriggerStarter | undefined,
  browserZone: string,
  firstProject: string,
): TriggerFormValues {
  if (trigger) {
    return {
      name: trigger.name,
      project: trigger.project,
      kind: trigger.kind,
      preset: trigger.kind === 'schedule' ? cronToPreset(trigger.cron ?? '') : defaultPreset(),
      timeZone: trigger.time_zone ?? '',
      label: trigger.label ?? '',
      prompt: trigger.prompt,
      enabled: trigger.enabled,
    };
  }
  return {
    name: '',
    project: firstProject,
    kind: starter?.kind ?? 'schedule',
    preset: starter?.cron ? cronToPreset(starter.cron) : defaultPreset(),
    timeZone: browserZone,
    label: '',
    prompt: starter?.prompt ?? '',
    enabled: true,
  };
}

/** Why the schedule cannot be saved yet, or null when it can. */
export function scheduleProblem(preset: SchedulePreset): string | null {
  if (presetToCron(preset) !== null) return null;
  switch (preset.repeat) {
    case 'days':
      return preset.days.length === 0
        ? 'Choose at least one day.'
        : 'Write the time as HH:MM, for example 09:30.';
    case 'daily':
      return 'Write the time as HH:MM, for example 09:30.';
    case 'custom':
      return 'A cron expression has five fields: minute, hour, day of month, month, day of week.';
    default:
      return 'Choose how often it repeats.';
  }
}

/** The first thing that stops the form from being sent, or null. */
export function formProblem(v: TriggerFormValues): string | null {
  if (!v.name.trim()) return 'A name is required.';
  if (!v.project) return 'Choose a project first.';
  if (v.kind === 'schedule') {
    const problem = scheduleProblem(v.preset);
    if (problem) return problem;
  }
  if (!v.prompt.trim()) return 'A prompt is required.';
  return null;
}

/** The request for a new trigger. formProblem has passed, so the schedule has a cron. */
export function createBody(v: TriggerFormValues): TriggerCreate {
  const body: TriggerCreate = {
    name: v.name.trim(),
    kind: v.kind,
    project: v.project,
    prompt: v.prompt.trim(),
    enabled: v.enabled,
  };
  if (v.kind === 'schedule') {
    body.cron = presetToCron(v.preset) ?? '';
    if (v.timeZone !== '') body.time_zone = v.timeZone;
  } else {
    const label = v.label.trim();
    if (label !== '') body.label = label;
  }
  return body;
}

/**
 * Only the fields that differ from the trigger, so nothing is re-armed or rewritten by accident.
 * A cleared zone or label is sent as '' so the daemon clears it too.
 */
export function patchBody(trigger: Trigger, v: TriggerFormValues): TriggerPatch {
  const patch: TriggerPatch = {};
  if (v.name.trim() !== trigger.name) patch.name = v.name.trim();
  if (v.prompt.trim() !== trigger.prompt) patch.prompt = v.prompt.trim();
  if (v.enabled !== trigger.enabled) patch.enabled = v.enabled;
  if (trigger.kind === 'schedule') {
    const cron = presetToCron(v.preset) ?? '';
    if (cron !== (trigger.cron ?? '')) patch.cron = cron;
    if (v.timeZone !== (trigger.time_zone ?? '')) patch.time_zone = v.timeZone;
  } else {
    const label = v.label.trim();
    if (label !== (trigger.label ?? '')) patch.label = label;
  }
  return patch;
}

/** The schedule said back in a sentence, for the line under the fields. */
export function firesSentence(cron: string, timeZone: string): string {
  const zone = timeZone ? `${timeZone} time` : "daemon's time";
  const words = describeCron(cron);
  if (!words) return `Fires on the cron expression above, ${zone}.`;
  return `Fires ${words.replace(/^Every/, 'every')}, ${zone}.`;
}

/** The zones the Time zone list offers: the browser's list, plus `current` and `browserZone` when missing, sorted, without ''. */
export function timeZoneChoices(current: string, browserZone: string): string[] {
  const zones = typeof Intl.supportedValuesOf === 'function' ? [...Intl.supportedValuesOf('timeZone')] : [];
  for (const zone of [current, browserZone]) {
    if (zone && !zones.includes(zone)) zones.push(zone);
  }
  return zones.sort((a, b) => a.localeCompare(b));
}
