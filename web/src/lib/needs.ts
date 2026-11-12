// What needs the owner, in the order the Overview page lists it: the problems first, then the
// approvals waiting for a decision.
import type { Approval, Problem, ProblemKind } from '../types';

/** One thing that needs the owner: a problem (red) or an approval waiting (yellow). */
export type Need =
  | { kind: 'problem'; key: string; project: string; lamp: 'stop'; since: string; problem: Problem }
  | { kind: 'approval'; key: string; project: string; lamp: 'call'; since: string; approval: Approval };

const KIND_RANK: Record<ProblemKind, number> = { blocked_tasks: 0, failing_loops: 1, blocked_goal: 2 };
const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

// A value that does not parse sorts last.
function timeOf(value: string): number {
  const t = Date.parse(value);
  return Number.isNaN(t) ? Infinity : t;
}

function compare(a: Need, b: Need): number {
  if (a.kind !== b.kind) return a.kind === 'problem' ? -1 : 1;
  if (a.kind === 'problem' && b.kind === 'problem') {
    const byKind = KIND_RANK[a.problem.kind] - KIND_RANK[b.problem.kind];
    if (byKind !== 0) return byKind;
    if (a.problem.kind === 'blocked_tasks' && a.problem.count !== b.problem.count) {
      return b.problem.count - a.problem.count;
    }
  }
  const ta = timeOf(a.since);
  const tb = timeOf(b.since);
  if (ta !== tb) return ta < tb ? -1 : 1;
  return a.key < b.key ? -1 : a.key > b.key ? 1 : 0;
}

/** The needs for the page, most urgent first. Pure: the arguments are not changed. */
export function buildNeeds(problems: Problem[], approvals: Approval[]): Need[] {
  const needs: Need[] = [
    ...problems.map((problem): Need => ({
      kind: 'problem',
      key: problem.key,
      project: problem.project,
      lamp: 'stop',
      since: problem.since,
      problem,
    })),
    ...approvals
      .filter((approval) => approval.status === 'pending')
      .map((approval): Need => ({
        kind: 'approval',
        key: 'approval:' + approval.id,
        project: approval.project,
        lamp: 'call',
        since: approval.created_at,
        approval,
      })),
  ];
  return needs.sort(compare);
}

/** Groups needs by project. Each group keeps the order of `needs`; projects in the order they first appear. */
export function needsByProject(needs: Need[]): Map<string, Need[]> {
  const groups = new Map<string, Need[]>();
  for (const need of needs) {
    const group = groups.get(need.project);
    if (group) group.push(need);
    else groups.set(need.project, [need]);
  }
  return groups;
}

/** A coarse age with no "ago": "<n>m", "<n>h" or "<n>d", floored. */
export function ageWords(ms: number): string {
  if (!Number.isFinite(ms) || ms < MINUTE) return 'under a minute';
  if (ms < HOUR) return `${Math.floor(ms / MINUTE)}m`;
  if (ms < 48 * HOUR) return `${Math.floor(ms / HOUR)}h`;
  return `${Math.floor(ms / DAY)}d`;
}
