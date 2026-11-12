// One live run on the Overview board: its title, how long it has run, and a bar showing whether
// output is still flowing.
import { Link } from 'react-router-dom';
import { useCan } from '../auth';
import { useCancelRun, useTask } from '../queries';
import { chatRunHref } from '../lib/chatRunLink';
import { agentLabel } from '../lib/agentLabel';
import { runBar } from '../lib/runbar';
import { elapsedMs, formatDuration, parseTime } from '../lib/time';
import type { Run } from '../types';
import { Button } from './Button';
import { ErrorLine } from './ErrorLine';
import { RelTime } from './RelTime';

const STALE_OUTPUT_MS = 5 * 60_000;

/** `inLane`: the run sits under its project's lane, so the meta line leaves the project out. */
export function RunItem({ run, scale, now, inLane = false }: { run: Run; scale: number; now: number; inLane?: boolean }) {
  const task = useTask(run.task_id);
  const cancel = useCancelRun(run.id);
  const canOperate = useCan('operate');
  const started = parseTime(run.started_at);
  const lastOutput = parseTime(run.last_output_at);
  const quietMs = lastOutput ? now - lastOutput.getTime() : started ? now - started.getTime() : 0;
  const stale = quietMs > STALE_OUTPUT_MS;
  const bar = started ? runBar(started.getTime(), lastOutput?.getTime() ?? null, now, scale, STALE_OUTPUT_MS) : { flow: 0, quiet: 0 };
  const who = inLane ? (run.task_id ? agentLabel(run.role) : 'Chat') : [run.task_id ? agentLabel(run.role) : '', run.project].filter(Boolean).join(' · ');

  return (
    <li className="run-item">
      <div className="run-item-head">
        <Link to={run.task_id ? `/runs/${run.id}` : chatRunHref(run)}>
          {run.task_id ? (task.data?.task.title ?? 'Loading task…') : `Chat with ${agentLabel(run.role)}`}
        </Link>
        {canOperate ? (
          <Button variant="ghost" confirm disabled={cancel.isPending} onClick={() => cancel.mutate()}>
            Cancel
          </Button>
        ) : null}
      </div>
      <div className="run-item-meta">
        <span>{who}</span>
        <span>
          {started ? formatDuration(elapsedMs(started, null, now)) : 'starting'}
          {' · '}
          {stale ? (
            <span className="warn-text">quiet for {formatDuration(quietMs)}</span>
          ) : (
            <>output {lastOutput ? <RelTime value={run.last_output_at} /> : 'none yet'}</>
          )}
        </span>
      </div>
      <div className="run-track" aria-hidden="true">
        <span className="run-flow" style={{ width: `${bar.flow}%` }} />
        {bar.quiet > 0 ? <span className="run-quiet" style={{ left: `${bar.flow}%`, width: `${bar.quiet}%` }} /> : null}
      </div>
      <ErrorLine error={cancel.error} />
    </li>
  );
}
