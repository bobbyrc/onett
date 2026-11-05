import type { PauseView } from '../types';

const shortId = (id: string): string => (id.length > 8 ? `${id.slice(0, 8)}…` : id);

/**
 * One sentence on why the factory is paused, e.g. "Paused for self-update to 69f1660 since 22:36,
 * waiting for 1 run (task 01a1a9c6…)." Mirrors `onett status`.
 */
export function describePause(p: PauseView): string {
  const t = new Date(p.since);
  const since = Number.isNaN(t.getTime()) || t.getUTCFullYear() < 2000 ? '' : ` since ${t.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })}`;
  if (p.reason !== 'self-update') return p.by ? `Paused by ${p.by}${since}.` : `Paused${since}.`;
  const head = `Paused for self-update${p.detail ? ` to ${p.detail}` : ''}${since}`;
  const w = p.waiting ?? [];
  if (w.length === 0) return `${head}, swapping the binary.`;
  if (w.length === 1) return `${head}, waiting for 1 run (task ${shortId(w[0].task_id)}).`;
  const ids = w.slice(0, 3).map((x) => shortId(x.task_id)).join(', ');
  return `${head}, waiting for ${w.length} runs (tasks ${ids}${w.length > 3 ? ', …' : ''}).`;
}
