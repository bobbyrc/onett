// One row of a project's lane on the Overview board: a problem or an approval that needs the owner,
// with the words that say how long it has waited and the buttons that act on it.
import { Fragment, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { shortTime } from '../lib/activity';
import { cx, humanize } from '../lib/format';
import { loopLabel } from '../lib/loopLabel';
import type { Need } from '../lib/needs';
import { formatRelative, parseTime } from '../lib/time';
import { useTask } from '../queries';
import type { Approval, Problem, Task } from '../types';
import { RetryTasksButton } from './RetryTasksButton';
import { RiskBadge } from './RiskBadge';

interface Shape {
  title: string;
  href: string;
  meta: ReactNode[];
  line?: { text: string; mono: boolean };
  actions: ReactNode[];
}

const MINUTE = 60_000;

function capFirst(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

// Parts that are empty (or unparsable times) are left out of the meta line by joinParts.
function joinParts(parts: ReactNode[]): ReactNode {
  const shown = parts.filter((part) => part !== '' && part !== null && part !== undefined && part !== false);
  return shown.map((part, index) => (
    <Fragment key={index}>
      {index > 0 ? ' · ' : null}
      {part}
    </Fragment>
  ));
}

function rel(value: string | undefined, now: number): string {
  const date = parseTime(value);
  return date ? formatRelative(date, now) : '';
}

function since(value: string, now: number): string {
  const date = parseTime(value);
  return date ? `since ${shortTime(date, now)} (${formatRelative(date, now)})` : '';
}

function told(problem: Problem, now: number): string {
  const date = parseTime(problem.told_at);
  return date ? `told you ${shortTime(date, now)}` : '';
}

function problemShape(problem: Problem, now: number): Shape {
  const groupHref = `/tasks?project=${encodeURIComponent(problem.project)}&status=blocked`;
  const tasks = problem.tasks ?? [];

  if (problem.kind === 'blocked_tasks' && problem.count >= 2) {
    const last = parseTime(problem.last_at);
    const first = parseTime(problem.since);
    const lastBlocked = last && first && last.getTime() - first.getTime() >= MINUTE ? `last blocked ${rel(problem.last_at, now)}` : '';
    const retryLabel = tasks.length === problem.count ? `Retry all ${problem.count}` : `Retry ${tasks.length} of ${problem.count}`;
    return {
      title: capFirst(problem.title),
      href: groupHref,
      meta: [`${problem.count} tasks blocked`, since(problem.since, now), told(problem, now), lastBlocked],
      line: problem.detail ? { text: problem.detail, mono: true } : undefined,
      actions: tasks.length
        ? [
            <RetryTasksButton key="check" ids={[tasks[0].id]} label="Check again" />,
            <RetryTasksButton key="all" ids={tasks.map((t) => t.id)} label={retryLabel} confirm />,
          ]
        : [],
    };
  }

  if (problem.kind === 'blocked_tasks') {
    const task = tasks[0];
    return {
      title: task ? `Blocked: ${task.title}` : `Blocked: ${capFirst(problem.title)}`,
      href: task ? `/tasks/${task.id}` : groupHref,
      meta: [task ? `attempt ${task.attempts} of ${task.max_attempts}` : '', rel(problem.since, now), told(problem, now)],
      line: problem.detail ? { text: problem.detail, mono: true } : problem.title ? { text: problem.title, mono: false } : undefined,
      actions: task
        ? [
            <RetryTasksButton key="retry" ids={[task.id]} label="Retry" />,
            <Link key="open" className="btn btn-secondary" to={`/tasks/${task.id}`}>
              Open
            </Link>,
          ]
        : [],
    };
  }

  if (problem.kind === 'failing_loops') {
    const loops = problem.loops ?? [];
    const names = loops.map((loop) => loopLabel(loop.name));
    const title =
      loops.length === 0
        ? 'Loops are failing'
        : loops.length === 1
          ? `1 loop is failing: ${names[0]}`
          : `${loops.length} loops are failing: ${names.join(', ')}`;
    const fails = loops.map((loop) => loop.fails);
    const same = loops.length > 0 && fails.every((f) => f === fails[0]);
    // With no loops listed there is nothing to count, so the part is left out.
    const failedRuns = same
      ? `${fails[0]} failed run${fails[0] === 1 ? '' : 's'}${loops.length >= 2 ? ' each' : ''}`
      : loops.length > 0
        ? `up to ${problem.count} failed runs`
        : '';
    return {
      title,
      href: '/loops',
      meta: [since(problem.since, now), failedRuns, problem.told_at ? told(problem, now) : 'not told yet'],
      line: problem.detail ? { text: problem.detail, mono: true } : undefined,
      actions: [
        <Link key="open" className="btn btn-secondary" to="/loops">
          Open Loops
        </Link>,
      ],
    };
  }

  const href = `/goals/${problem.goal_id}`;
  return {
    title: `Goal blocked: ${problem.title}`,
    href,
    meta: [rel(problem.since, now), told(problem, now)],
    line: problem.detail ? { text: problem.detail, mono: false } : undefined,
    actions: [
      <Link key="open" className="btn btn-secondary" to={href}>
        Open goal
      </Link>,
    ],
  };
}

function approvalShape(approval: Approval, task: Task | undefined, now: number): Shape {
  const reasons = task?.risk_reasons ?? [];
  let line: Shape['line'];
  if (reasons.length > 0) {
    const joined = reasons.join('. ');
    line = { text: /[.!?]$/.test(joined) ? joined : `${joined}.`, mono: false };
  } else {
    const reason = approval.reason.replace(/\s+/g, ' ').trim();
    if (reason) line = { text: reason, mono: false };
  }
  const asked = rel(approval.created_at, now);
  return {
    title: `${capFirst(humanize(approval.kind))} approval: ${approval.title}`,
    href: `/tasks/${approval.task_id}`,
    meta: [
      task?.risk ? (
        <>
          <RiskBadge risk={task.risk} /> risk
        </>
      ) : '',
      task?.pr_number ? `PR #${task.pr_number}` : '',
      asked ? `asked ${asked}` : '',
    ],
    line,
    actions: [
      <Link key="review" className="btn btn-secondary" to={`/tasks/${approval.task_id}`}>
        Review
      </Link>,
    ],
  };
}

/** `first` marks the row to start with ("Start here"); the page passes it for the most urgent need. */
export function NeedRow({ need, first, now }: { need: Need; first: boolean; now: number }) {
  const task = useTask(need.kind === 'approval' ? need.approval.task_id : '').data?.task;
  const shape = need.kind === 'approval' ? approvalShape(need.approval, task, now) : problemShape(need.problem, now);
  return (
    <div className="ov-need">
      <span className={`lamp lamp-${need.lamp}`} aria-hidden="true" />
      <div className="ov-need-text">
        <p className="ov-need-title">
          {first ? <span className="badge tone-warn">Start here</span> : null}
          <Link to={shape.href}>{shape.title}</Link>
        </p>
        <p className="ov-need-meta">{joinParts(shape.meta)}</p>
        {shape.line ? <p className={cx('ov-need-line', shape.line.mono && 'mono')}>{shape.line.text}</p> : null}
      </div>
      <div className="ov-need-actions">{shape.actions}</div>
    </div>
  );
}
