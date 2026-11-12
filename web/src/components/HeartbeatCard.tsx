import { Link } from 'react-router-dom';
import { useLoops } from '../queries';
import { formatGoDuration } from '../lib/format';
import { parseTime } from '../lib/time';
import type { LoopView } from '../types';
import { Empty } from './Empty';
import { ErrorLine } from './ErrorLine';
import { RelTime } from './RelTime';

const times = (n: number) => (n === 1 ? '1 time' : `${n} times`);

/** The newest of the given timestamps, or '' when none parses. */
function latest(values: (string | undefined)[]): string {
  const times = values.map((v) => parseTime(v)).filter((d): d is Date => d !== null);
  return times.length ? new Date(Math.max(...times.map((d) => d.getTime()))).toISOString() : '';
}

/** The soonest next run among the loops that are on. */
function soonest(values: (string | undefined)[]): string {
  const times = values.map((v) => parseTime(v)).filter((d): d is Date => d !== null);
  return times.length ? new Date(Math.min(...times.map((d) => d.getTime()))).toISOString() : '';
}

/**
 * The Overview's Orchestrator card: whether the orchestrator's heartbeat runs, when it last and
 * next checks, and how often it told the owner and acted in the last day. The loop runs per
 * project, so the numbers add up across them. Each row of the page says whether the owner was
 * told, so the card no longer counts items. `loops` is the heartbeat entries of GET /loops; with
 * none (an older daemon) it shows nothing.
 */
export function HeartbeatStatus({ loops }: { loops: LoopView[] }) {
  if (loops.length === 0) return null;
  const enabled = loops.some((l) => l.enabled);
  const quiet = enabled && loops.filter((l) => l.enabled).every((l) => l.quiet_now);
  const sum = (pick: (l: LoopView) => number | undefined) => loops.reduce((n, l) => n + (pick(l) ?? 0), 0);
  const error = loops.find((l) => l.last_error)?.last_error;
  const ops = loops.find((l) => l.operations_conversation_id)?.operations_conversation_id;
  const lastRun = latest(loops.map((l) => l.last_run_at));
  const nextRun = enabled ? soonest(loops.filter((l) => l.enabled).map((l) => l.next_run_at)) : '';

  return (
    <section className="section heartbeat-card" aria-labelledby="heartbeat">
      <h2 id="heartbeat">
        Orchestrator
        <span className="section-note">
          {!enabled ? 'heartbeat disabled' : quiet ? 'heartbeat on · quiet hours' : 'heartbeat on'}
          {nextRun ? (
            <>
              {' '}
              · next check <RelTime value={nextRun} />
            </>
          ) : null}
        </span>
      </h2>
      <p className="ov-lead">
        Every {formatGoDuration(loops[0].every)} it looks over each project, deals with what it can and tells you about the
        rest. Each row above says whether you were told.
      </p>
      <dl className="kv">
        <dt>Last check</dt>
        <dd>{lastRun ? <RelTime value={lastRun} /> : <span className="muted">never</span>}</dd>
        <dt>Told you, 24h</dt>
        <dd>{times(sum((l) => l.escalations_24h))}</dd>
        <dt>Acted itself, 24h</dt>
        <dd>{times(sum((l) => l.actions_24h))}</dd>
      </dl>
      {error ? (
        <p className="danger-text heartbeat-error" role="alert">
          Last error: {error}
        </p>
      ) : null}
      {ops ? (
        <p className="heartbeat-link">
          <Link to={`/chat/${ops}`}>Open Operations</Link>
        </p>
      ) : null}
    </section>
  );
}

/** The card as the Overview shows it: nothing while the loops load; a note when there is no heartbeat loop. */
export function HeartbeatCard() {
  const loops = useLoops();
  const heartbeat = (loops.data ?? []).filter((l) => l.name === 'heartbeat');
  if (loops.error) return <ErrorLine error={loops.error} />;
  if (!loops.isSuccess) return null;
  if (heartbeat.length === 0) {
    return (
      <section className="section" aria-labelledby="heartbeat">
        <h2 id="heartbeat">Orchestrator</h2>
        <Empty>The heartbeat starts once a project exists.</Empty>
      </section>
    );
  }
  return <HeartbeatStatus loops={heartbeat} />;
}
