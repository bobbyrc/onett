import type { PauseView } from '../types';

/** One sentence on who paused the factory, e.g. "Paused by bob since 22:36." Mirrors `onett status`. */
export function describePause(p: PauseView): string {
  const t = new Date(p.since);
  const since = Number.isNaN(t.getTime()) || t.getUTCFullYear() < 2000 ? '' : ` since ${t.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })}`;
  return p.by ? `Paused by ${p.by}${since}.` : `Paused${since}.`;
}
