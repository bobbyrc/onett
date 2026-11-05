import { Link } from 'react-router-dom';
import { useLoops } from '../queries';
import { parseTime } from '../lib/time';
import type { LoopView } from '../types';
import { ErrorLine } from './ErrorLine';
import { RelTime } from './RelTime';

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
 * The Overview's Heartbeat card: whether the orchestrator's heartbeat runs, when, what it found
 * and what it did in the last day. The loop runs per project, so the numbers add up across them.
 * `loops` is the heartbeat entries of GET /loops; with none (an older daemon) it shows nothing.
 */
export function HeartbeatStatus({ loops }: { loops: LoopView[] }) {
  if (loops.length === 0) return null;
  const enabled = loops.some((l) => l.enabled);
  const quiet = enabled && loops.filter((l) => l.enabled).every((l) => l.quiet_now);
  const sum = (pick: (l: LoopView) => number | undefined) => loops.reduce((n, l) => n + (pick(l) ?? 0), 0);
  const items = sum((l) => l.item_count);
  const error = loops.find((l) => l.last_error)?.last_error;
  const ops = loops.find((l) => l.operations_conversation_id)?.operations_conversation_id;
  const lastRun = latest(loops.map((l) => l.last_run_at));
  const nextRun = enabled ? soonest(loops.filter((l) => l.enabled).map((l) => l.next_run_at)) : '';

  return (
    <section className="section heartbeat-card" aria-labelledby="heartbeat">
      <h2 id="heartbeat">
        Heartbeat
        <span className="section-note">{!enabled ? 'disabled' : quiet ? 'quiet hours' : 'on'}</span>
      </h2>
      <dl className="kv">
        <dt>Last run</dt>
        <dd>{lastRun ? <RelTime value={lastRun} /> : <span className="muted">never</span>}</dd>
        <dt>Next run</dt>
        <dd>{nextRun ? <RelTime value={nextRun} /> : <span className="muted">—</span>}</dd>
        <dt>Needs attention</dt>
        <dd>{items}</dd>
        <dt>Actions, 24h</dt>
        <dd>{sum((l) => l.actions_24h)}</dd>
        <dt>Escalations, 24h</dt>
        <dd>{sum((l) => l.escalations_24h)}</dd>
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

/** The card as the Overview shows it: hidden until the loops load, and when there is no heartbeat loop. */
export function HeartbeatCard() {
  const loops = useLoops();
  const heartbeat = (loops.data ?? []).filter((l) => l.name === 'heartbeat');
  if (loops.error) return <ErrorLine error={loops.error} />;
  return <HeartbeatStatus loops={heartbeat} />;
}
