import './ActivityLine.css';
import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { cx } from '../lib/format';
import { formatDuration, parseTime } from '../lib/time';
import type { LogLine, Run } from '../types';

/** A run with no output for this long gets the "output Ns ago" stall hint. */
export const STALL_HINT_MS = 30_000;

export type ActivityKind = 'working' | 'worked' | 'stopped' | 'failed';

/** Why a thread that is not working needs a person: an approval to give, or a block to clear. */
export type Attention = 'waiting' | 'blocked';

/** Which attention label a task status earns, if any. */
export function attentionFor(status: string | undefined): Attention | undefined {
  if (status === 'awaiting_approval') return 'waiting';
  if (status === 'blocked') return 'blocked';
  return undefined;
}

/** How a finished run reads: done, stopped by someone, or failed. */
export function finishedKind(status: Run['status']): Exclude<ActivityKind, 'working'> {
  if (status === 'succeeded') return 'worked';
  if (status === 'cancelled' || status === 'interrupted') return 'stopped';
  return 'failed';
}

/** "1 minute 40 seconds": the coarse spoken form of a duration, for the live region. */
export function spokenDuration(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const part = (n: number, unit: string) => `${n} ${unit}${n === 1 ? '' : 's'}`;
  const parts: string[] = [];
  if (h) parts.push(part(h, 'hour'));
  if (m) parts.push(part(m, 'minute'));
  if (s || parts.length === 0) parts.push(part(s, 'second'));
  return parts.join(' ');
}

/** A second-resolution clock; idle while inactive. */
function useSecond(active: boolean): number {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!active) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [active]);
  return now;
}

/**
 * The "Worked for 1m 40s ›" line above a finished turn's answer. The button collapses or expands the
 * work between the owner's message and the answer; a hairline follows it. The chevron is drawn in CSS.
 */
export function WorkedToggle({
  duration,
  open,
  onToggle,
}: {
  /** Formatted time the turn took, when known. */
  duration?: string;
  open: boolean;
  onToggle(): void;
}) {
  return (
    <div className="activity-worked">
      <button type="button" className="activity-toggle" aria-expanded={open} onClick={onToggle}>
        {duration ? `Worked for ${duration}` : 'Worked'}
        <span className={cx('activity-chev', open && 'open')} aria-hidden="true" />
      </button>
      <span className="activity-hair" aria-hidden="true" />
    </div>
  );
}

/**
 * The line at the tail of a transcript (docs/design/chat-visual.md, "Status and activity line").
 * While the turn runs: a lamp and "Working · 1m 12s". After it: "Stopped after 40s" or "Failed after
 * 1m 02s". A turn that succeeded shows no line here: the transcript shows its WorkedToggle, which
 * collapses that turn's work. One polite live region whose text changes only at
 * those transitions: the ticking numbers sit in aria-hidden spans and the spoken text is coarse.
 */
export function ActivityLine({
  run,
  working,
  label,
  lines,
  attention,
}: {
  run?: Run;
  working: boolean;
  label: string;
  lines?: LogLine[];
  /** Set when the thread is idle but waits on the owner; it replaces the finished-run line. */
  attention?: Attention;
}) {
  const now = useSecond(working);
  const started = parseTime(run?.started_at);
  if (!working && attention) {
    // No timer: the wait is not work. The lamp is static, since nothing is running.
    return (
      <p className={cx('activity-line', `activity-${attention}`)} role="status" aria-live="polite">
        <span className="activity-lamp" aria-hidden="true" />
        <span className="activity-text">{attention === 'waiting' ? 'Waiting for you' : 'Blocked'}</span>
      </p>
    );
  }
  if (!working) {
    const ended = parseTime(run?.ended_at);
    if (!run || !started || !ended) return null;
    const kind = finishedKind(run.status);
    const ms = ended.getTime() - started.getTime();
    const visible = formatDuration(ms);
    const spoken = spokenDuration(ms);
    if (kind === 'worked') return null;
    const text = kind === 'stopped' ? 'Stopped after' : 'Failed after';
    return (
      <p className={cx('activity-line', `activity-${kind}`)} role="status" aria-live="polite">
        <span className="activity-text">
          {text} <span aria-hidden="true">{visible}</span>
          <span className="activity-sr">{spoken}</span>
        </span>
      </p>
    );
  }
  const lastLine = lines && lines.length > 0 ? lines[lines.length - 1].time : undefined;
  const lastOutput = parseTime(lastLine) ?? parseTime(run?.last_output_at);
  const quietMs = lastOutput ? now - lastOutput.getTime() : 0;
  const stalled = quietMs > STALL_HINT_MS;
  return (
    <p
      className={cx('activity-line', 'activity-working', stalled && 'activity-stalled')}
      role="status"
      aria-live="polite"
    >
      <span className="activity-lamp" aria-hidden="true" />
      <span className="activity-text">
        {started ? 'Working' : `${label} is working`}
        {started ? <span aria-hidden="true"> · {formatDuration(now - started.getTime())}</span> : null}
        {stalled ? (
          <>
            <span className="activity-sr">No recent output</span>
            <span className="activity-quiet" aria-hidden="true">
              {' '}
              · output {formatDuration(quietMs)} ago
            </span>
          </>
        ) : null}
        {run ? (
          <span className="activity-link">
            {' · '}
            <Link to={`/runs/${run.id}`}>view run</Link>
          </span>
        ) : null}
      </span>
    </p>
  );
}
