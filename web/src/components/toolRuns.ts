// Pure logic for folding consecutive tool calls into one collapsible run in the conversation view.
import { parseTime } from '../lib/time';
import type { Entry } from '../lib/transcript';

export type ToolEntry = Extract<Entry, { kind: 'tool' }>;

export type ToolBlock = { type: 'entry'; entry: Entry } | { type: 'run'; id: string; calls: ToolEntry[] };

/**
 * Folds every sequence of two or more consecutive tool entries into one run. A lone call, and
 * every other entry, stays as it is; any non-tool entry between two calls splits them.
 */
export function groupToolRuns(entries: Entry[]): ToolBlock[] {
  const out: ToolBlock[] = [];
  let i = 0;
  while (i < entries.length) {
    const e = entries[i];
    if (e.kind !== 'tool') {
      out.push({ type: 'entry', entry: e });
      i++;
      continue;
    }
    let j = i;
    const calls: ToolEntry[] = [];
    while (j < entries.length) {
      const n = entries[j];
      if (n.kind !== 'tool') break;
      calls.push(n);
      j++;
    }
    out.push(calls.length === 1 ? { type: 'entry', entry: e } : { type: 'run', id: calls[0].id, calls });
    i = j;
  }
  return out;
}

export type ToolKind = 'command' | 'read' | 'edit' | 'search' | 'other';

// Names the Claude harness reports (Bash, Read, Edit, ...) and the Codex one (shell, edit), plus
// common aliases. Compared in lower case.
const KINDS: Record<string, ToolKind> = {
  bash: 'command',
  shell: 'command',
  exec: 'command',
  exec_command: 'command',
  command: 'command',
  read: 'read',
  view: 'read',
  cat: 'read',
  notebookread: 'read',
  edit: 'edit',
  write: 'edit',
  multiedit: 'edit',
  notebookedit: 'edit',
  apply_patch: 'edit',
  grep: 'search',
  glob: 'search',
  search: 'search',
  websearch: 'search',
};

export const toolKind = (name: string): ToolKind => KINDS[name.toLowerCase()] ?? 'other';

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/** "Ran 3 commands, read 2 files, edited 1 file, searched 2 times, used 4 tools". */
export function summarizeRun(calls: ToolEntry[]): string {
  const n: Record<ToolKind, number> = { command: 0, read: 0, edit: 0, search: 0, other: 0 };
  for (const c of calls) n[toolKind(c.tool.name)]++;
  const parts: string[] = [];
  if (n.command) parts.push(`ran ${plural(n.command, 'command', 'commands')}`);
  if (n.read) parts.push(`read ${plural(n.read, 'file', 'files')}`);
  if (n.edit) parts.push(`edited ${plural(n.edit, 'file', 'files')}`);
  if (n.search) parts.push(`searched ${plural(n.search, 'time', 'times')}`);
  if (n.other) parts.push(`used ${plural(n.other, 'tool', 'tools')}`);
  const text = parts.join(', ');
  return text.charAt(0).toUpperCase() + text.slice(1);
}

export interface ChangedFile {
  path: string;
  added: number;
  removed: number;
}

/** Files touched by edit calls, once each, with the lines added and removed summed across calls. */
export function changedFiles(calls: ToolEntry[]): ChangedFile[] {
  const files = new Map<string, ChangedFile>();
  for (const c of calls) {
    if (toolKind(c.tool.name) !== 'edit') continue;
    // Codex lists every changed path in one call, joined by ", "; Claude's calls name one file.
    const paths = c.tool.name.toLowerCase() === 'edit' && c.tool.arg.includes(', ') ? c.tool.arg.split(', ') : [c.tool.arg];
    for (const path of paths.filter(Boolean)) {
      const f = files.get(path) ?? { path, added: 0, removed: 0 };
      f.added += c.tool.added ?? 0;
      f.removed += c.tool.removed ?? 0;
      files.set(path, f);
    }
  }
  return [...files.values()];
}

/** The header while a run is live: what its latest call is doing. */
export function liveLabel(call: ToolEntry): string {
  const { name, arg } = call.tool;
  const verb = { command: 'Running', read: 'Reading', edit: 'Editing', search: 'Searching', other: name }[toolKind(name)];
  return arg ? `${verb} ${arg}` : verb;
}

/** A run is live while its last call has no result yet and the run it belongs to is still going. */
export const isLiveRun = (calls: ToolEntry[]): boolean => calls[calls.length - 1].result === undefined;

export interface TurnWork {
  /** Id of the turn's first work entry; the "Worked for" toggle sits before it. */
  key: string;
  /** Ids of the entries a collapsed turn hides: the narration and tool calls before the answer. */
  ids: Set<string>;
  /** First work entry to last entry of the turn, in ms; 0 when a time is unknown. */
  ms: number;
}

/**
 * The finished turns that have work to fold. A turn runs from an owner message (or prompt) to the
 * next one. Its answer is its last text entry; the text and tool calls before it are the work, and
 * only when at least one is a tool call. Denials, errors and notes stay visible. The turn that is
 * still producing output (the last one, with `liveRunId` among its entries, or without `liveRunId`
 * a trailing call that has no result) is not folded, so live work is never hidden.
 */
export function turnWork(entries: Entry[], liveRunId?: string): TurnWork[] {
  const out: TurnWork[] = [];
  let i = 0;
  while (i < entries.length) {
    if (entries[i].kind === 'owner' || entries[i].kind === 'prompt') {
      i++;
      continue;
    }
    let j = i;
    while (j < entries.length && entries[j].kind !== 'owner' && entries[j].kind !== 'prompt') j++;
    const turn = entries.slice(i, j);
    const lastEntry = turn[turn.length - 1];
    const live =
      j === entries.length &&
      (liveRunId !== undefined
        ? turn.some((e) => e.runId === liveRunId)
        : lastEntry.kind === 'tool' && lastEntry.result === undefined);
    let answer = -1;
    turn.forEach((e, k) => {
      if (e.kind === 'text') answer = k;
    });
    const work = turn.slice(0, Math.max(answer, 0)).filter((e) => e.kind === 'text' || e.kind === 'tool');
    if (!live && answer > 0 && work.some((e) => e.kind === 'tool')) {
      const a = parseTime(work[0].time);
      const b = parseTime(lastEntry.time);
      out.push({ key: work[0].id, ids: new Set(work.map((e) => e.id)), ms: a && b ? b.getTime() - a.getTime() : 0 });
    }
    i = j;
  }
  return out;
}
