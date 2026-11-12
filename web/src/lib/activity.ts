// The Activity feed on the Overview page: each event becomes one plain sentence, newest first.
// Repeats of the same trouble within ten minutes fold into one row.
import { agentLabel } from './agentLabel';
import { loopLabel } from './loopLabel';
import { parseTime } from './time';
import type { OnettEvent, Problem } from '../types';

/** A piece of a row's sentence; with `to` it is a link. */
export interface FeedPart {
  text: string;
  to?: string;
}

export interface FeedRow {
  key: string;
  time: Date | null;
  /** A lamp before the sentence; the sentence always says the same in words. */
  lamp?: 'stop' | 'call';
  parts: FeedPart[];
  /** A muted addition after the sentence: "7 tasks · 10:21 to 10:27". */
  note?: string;
  project?: string;
}

const MINUTE = 60_000;
const FOLD_WINDOW = 10 * MINUTE;
// Events that only feed the live views; they never make a row.
const PLUMBING = new Set(['run.log', 'chat.delta', 'chat.message', 'conversation.updated', 'trigger.updated']);
const FAILED_RUN = new Set(['failed', 'stalled', 'timed_out', 'denied']);

type Data = Record<string, unknown>;

interface Context {
  titles: Map<string, string>;
  problems: Problem[];
  now: number;
}

interface Fold {
  key: string;
  // Members with the same subject are one thing said again; otherwise the sentence counts them.
  subject: string;
  /** `subjects` is how many different subjects the group has. */
  sentence: (count: number, newest: OnettEvent, subjects: number) => string;
}

interface Entry {
  ev: OnettEvent;
  draft: Draft;
  key: string;
  time: Date | null;
}

interface Draft {
  lamp?: 'stop' | 'call';
  parts: FeedPart[];
  note?: string;
  fold?: Fold;
  /** Replaces the event's project, for a notification that names its own. */
  project?: string;
}

const pad = (n: number) => String(n).padStart(2, '0');

/** A short clock time: "09:05" today, "Mon 09:05" within six days, "22 Oct" further back. */
export function shortTime(date: Date, now: number): string {
  const hhmm = `${pad(date.getHours())}:${pad(date.getMinutes())}`;
  const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((startOfDay(new Date(now)) - startOfDay(date)) / (24 * 60 * MINUTE));
  if (days === 0) return hhmm;
  if (days >= 1 && days <= 6) return `${date.toLocaleDateString('en-GB', { weekday: 'short' })} ${hhmm}`;
  return date.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' });
}

// A field the rule needs as a non-empty string, or undefined when it is missing or another type.
const str = (value: unknown): string | undefined => (typeof value === 'string' && value !== '' ? value : undefined);

function dataOf(ev: OnettEvent): Data {
  const data = ev.data;
  return data && typeof data === 'object' && !Array.isArray(data) ? (data as Data) : {};
}

// Keeps at most n characters in all, ending with "…" when it cut.
function clip(text: string, n: number): string {
  return text.length > n ? `${text.slice(0, n - 1)}…` : text;
}

function capFirst(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

function words(text: string): string {
  return text.replace(/_/g, ' ');
}

const plain = (text: string): FeedPart => ({ text });
const link = (text: string, to: string): FeedPart => ({ text, to });

function defaultDraft(ev: OnettEvent): Draft {
  const parts: FeedPart[] = [plain(capFirst(clip(ev.message, 140)))];
  if (ev.task_id) parts.push(plain(' '), link('task', `/tasks/${ev.task_id}`));
  else if (ev.run_id) parts.push(plain(' '), link('run', `/runs/${ev.run_id}`));
  // A repeated message folds; the subject is the message, so a count sentence is never used.
  const fold: Fold = { key: `default|${ev.type}|${ev.project ?? ''}|${ev.message}`, subject: ev.message, sentence: () => '' };
  return { parts, fold };
}

function runDraft(ev: OnettEvent, data: Data, ctx: Context): Draft | null {
  const status = str(data.status);
  if (!status) return defaultDraft(ev);
  if (status === 'running' || status === 'succeeded') return null;
  const id = str(data.id);
  const role = str(data.role);
  const taskId = str(data.task_id);
  const failed = FAILED_RUN.has(status);
  if (!taskId) {
    // A chat turn: only a failed one is news.
    if (!failed) return null;
    if (!role || !id) return defaultDraft(ev);
    return { lamp: 'stop', parts: [plain(`${agentLabel(role)} chat run ${words(status)}: `), link('run', `/runs/${id}`)] };
  }
  if (!role || !id) return defaultDraft(ev);
  const title = ctx.titles.get(taskId) ?? 'a task';
  const project = ev.project ?? '';
  if (status === 'starting') {
    return {
      parts: [plain(`${agentLabel(role)} started on `), link(title, `/runs/${id}`)],
      fold: { key: `start|${project}|${role}`, subject: taskId, sentence: (n) => `${agentLabel(role)} started ${n} runs` },
    };
  }
  return {
    lamp: failed ? 'stop' : undefined,
    parts: [plain(`${agentLabel(role)} run ${words(status)}: `), link(title, `/runs/${id}`)],
    fold: {
      key: `run|${project}|${role}|${status}`,
      subject: taskId,
      sentence: (n) => `${n} ${agentLabel(role)} runs ${words(status)}`,
    },
  };
}

function taskDraft(ev: OnettEvent, data: Data): Draft | null {
  const id = str(data.id);
  const title = str(data.title);
  if (!id || !title) return null;
  if (ev.type === 'task.created') {
    return {
      parts: [plain('Queued: '), link(title, `/tasks/${id}`)],
      fold: { key: `created|${ev.project ?? ''}`, subject: id, sentence: (_n, _newest, subjects) => `${subjects} tasks queued` },
    };
  }
  const msg = ev.message.startsWith(`${title}: `) ? ev.message.slice(title.length + 2) : ev.message;
  // Only the daemon's block message is a "Blocked" row; a later update of a blocked task is not.
  if (data.status === 'blocked' && msg.startsWith('blocked:')) {
    const cause = str(data.blocked_cause) ?? str(data.blocked_reason) ?? '';
    return {
      lamp: 'stop',
      parts: [plain('Blocked: '), link(title, `/tasks/${id}`)],
      fold: {
        key: `blocked|${ev.project ?? ''}|${cause}`,
        subject: id,
        sentence: (_n, _newest, subjects) => (cause ? `${subjects} tasks blocked: ${clip(cause, 80)}` : `${subjects} tasks blocked`),
      },
    };
  }
  return {
    parts: [link(title, `/tasks/${id}`), plain(`: ${clip(msg, 120)}`)],
    fold: {
      key: `task|${ev.project ?? ''}|${msg}`,
      subject: id,
      sentence: (_n, _newest, subjects) => `${subjects} tasks: ${clip(msg, 100)}`,
    },
  };
}

function loopDraft(ev: OnettEvent, data: Data, ctx: Context): Draft | null {
  const loop = str(data.loop);
  const error = str(data.error);
  if (!loop || !error) return null;
  const streak = ctx.problems
    .find((p) => p.kind === 'failing_loops' && p.project === ev.project)
    ?.loops?.find((l) => l.name === loop);
  let note: string | undefined;
  const since = streak ? parseTime(streak.since) : null;
  if (streak && streak.fails > 1 && since) note = `${streak.fails} times since ${shortTime(since, ctx.now)}`;
  return {
    lamp: 'stop',
    parts: [link(loopLabel(loop), '/loops'), plain(' failed')],
    note,
    // The subject is the loop itself, so repeats fold; a count sentence is never used.
    fold: { key: `loop|${ev.project ?? ''}|${loop}`, subject: loop, sentence: () => '' },
  };
}

// Notification targets by the kind the daemon names in "Needs you: <kind> <id> (<project>)".
const NEEDS_TARGET = new Map<string, string>([
  ['blocked_task', '/tasks/'],
  ['failed_task', '/tasks/'],
  ['dependency', '/tasks/'],
  ['stale_queued', '/tasks/'],
  ['agent_question', '/tasks/'],
  ['blocked_goal', '/goals/'],
  ['run', '/runs/'],
]);
const NEEDS_YOU = /^Needs you: (\S+) (\S+) \((.+)\)$/;

function notificationDraft(ev: OnettEvent): Draft | null {
  if (ev.message.startsWith('Blocked: ')) return null;
  const needs = NEEDS_YOU.exec(ev.message);
  const fold: Fold = {
    key: `notification|${needs ? needs[3] : (ev.project ?? '')}`,
    subject: ev.message,
    sentence: (n, newest) => `Told you ${n} times, latest: ${clip(newest.message, 100)}`,
  };
  if (!needs) return { parts: [plain(`Told you: ${clip(ev.message, 140)}`)], fold };
  const [, kind, id, project] = needs;
  const text = words(kind);
  let to: string | undefined;
  if (kind === 'approval') to = '/approvals';
  else if (kind === 'loop_error') to = '/loops';
  else if (NEEDS_TARGET.has(kind)) to = `${NEEDS_TARGET.get(kind)}${id}`;
  return {
    parts: [plain('Told you: '), to ? link(text, to) : plain(text), plain(` on ${project}`)],
    project,
    fold,
  };
}

function draftFor(ev: OnettEvent, ctx: Context): Draft | null {
  if (PLUMBING.has(ev.type)) return null;
  const data = dataOf(ev);
  switch (ev.type) {
    case 'run.updated':
      return runDraft(ev, data, ctx);
    case 'task.created':
    case 'task.updated':
      return taskDraft(ev, data) ?? defaultDraft(ev);
    case 'approval.requested':
    case 'approval.decided': {
      const kind = str(data.kind);
      const title = str(data.title);
      const taskId = str(data.task_id);
      if (!kind || !title || !taskId) return defaultDraft(ev);
      const what = ev.type === 'approval.requested' ? 'asked' : words(str(data.status) ?? '');
      return {
        lamp: ev.type === 'approval.requested' ? 'call' : undefined,
        parts: [plain(`${capFirst(words(kind))} approval ${what}: `), link(title, `/tasks/${taskId}`)],
      };
    }
    case 'goal.updated': {
      const id = str(data.id);
      const title = str(data.title);
      if (!id || !title || data.status !== 'blocked') return defaultDraft(ev);
      return { lamp: 'stop', parts: [plain('Goal blocked: '), link(title, `/goals/${id}`)] };
    }
    case 'loop.error':
      return loopDraft(ev, data, ctx) ?? defaultDraft(ev);
    case 'notification':
      return notificationDraft(ev);
    case 'deploy.updated': {
      const failed = data.state === 'failed' || data.state === 'rolled_back';
      return { ...defaultDraft(ev), lamp: failed ? 'stop' : undefined };
    }
    default:
      return defaultDraft(ev);
  }
}

// The task titles seen in task and approval events, so a run can name its task.
function titlesFrom(events: OnettEvent[]): Map<string, string> {
  const titles = new Map<string, string>();
  for (const ev of events) {
    const data = dataOf(ev);
    if (ev.type.startsWith('task.')) {
      const id = str(data.id);
      const title = str(data.title);
      if (id && title) titles.set(id, title);
    } else if (ev.type.startsWith('approval.')) {
      const id = str(data.task_id);
      const title = str(data.title);
      if (id && title) titles.set(id, title);
    }
  }
  return titles;
}

function timeOf(ev: OnettEvent): number {
  return parseTime(ev.time)?.getTime() ?? -Infinity;
}

/** The feed rows for the events, newest first. The caller has already removed duplicate events. */
export function feedRows(events: OnettEvent[], problems: Problem[], now: number): FeedRow[] {
  const ctx: Context = { titles: titlesFrom(events), problems, now };
  const sorted = [...events].sort((a, b) => {
    const ta = timeOf(a);
    const tb = timeOf(b);
    if (ta !== tb) return tb - ta;
    return b.id - a.id;
  });

  // Each slot is one row, or the members of a group that folds into one. Members are newest first.
  const slots: Entry[][] = [];
  const open = new Map<string, { slot: Entry[]; oldest: number }>();
  for (const ev of sorted) {
    const draft = draftFor(ev, ctx);
    if (!draft) continue;
    const time = parseTime(ev.time);
    const entry: Entry = {
      ev,
      draft,
      key: ev.id > 0 ? `id:${ev.id}` : `${ev.type}:${ev.time}:${ev.message}`,
      time,
    };
    const fold = draft.fold;
    const group = fold && time ? open.get(fold.key) : undefined;
    if (fold && time && group && group.oldest - time.getTime() <= FOLD_WINDOW) {
      group.slot.push(entry);
      group.oldest = time.getTime();
      continue;
    }
    const slot = [entry];
    slots.push(slot);
    if (fold && time) open.set(fold.key, { slot, oldest: time.getTime() });
  }

  return slots.map((slot): FeedRow => {
    const newest = slot[0];
    const base = {
      key: newest.key,
      time: newest.time,
      lamp: newest.draft.lamp,
      project: newest.draft.project ?? (newest.ev.project || undefined),
    };
    const fold = newest.draft.fold;
    if (slot.length === 1 || !fold) {
      return { ...base, parts: newest.draft.parts, note: newest.draft.note };
    }
    const oldest = slot[slot.length - 1];
    const range =
      newest.time && oldest.time && newest.time.getTime() - oldest.time.getTime() >= MINUTE
        ? `${shortTime(oldest.time, now)} to ${shortTime(newest.time, now)}`
        : undefined;
    const sameSubject = slot.every((entry) => entry.draft.fold?.subject === fold.subject);
    if (sameSubject) {
      // The same thing again: keep its sentence and say how often.
      const count = newest.draft.note ?? `${slot.length} times`;
      return { ...base, parts: newest.draft.parts, note: range ? `${count} · ${range}` : count };
    }
    const subjects = new Set(slot.map((entry) => entry.draft.fold?.subject)).size;
    return { ...base, parts: [plain(fold.sentence(slot.length, newest.ev, subjects))], note: range };
  });
}
