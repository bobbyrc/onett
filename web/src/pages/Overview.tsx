import './Overview.css';
import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { getEventsHistory, listProblems } from '../api';
import { subscribe } from '../events';
import { keys } from '../keys';
import { useCan } from '../auth';
import { feedRows, shortTime } from '../lib/activity';
import { formatDateTime, parseTime } from '../lib/time';
import { axisMinutes } from '../lib/runbar';
import { useNow } from '../lib/clock';
import { buildNeeds } from '../lib/needs';
import { verdictFor } from '../lib/verdict';
import { AndonBoard } from '../components/AndonBoard';
import { BoardVerdict } from '../components/BoardVerdict';
import { Empty } from '../components/Empty';
import { ErrorLine } from '../components/ErrorLine';
import { HeartbeatCard } from '../components/HeartbeatCard';
import { HoldBanner } from '../components/HoldBanner';
import { OwnerUnreachableBanner } from '../components/OwnerUnreachableBanner';
import { SetupChecklist } from '../components/SetupChecklist';
import { LIST_REFRESH_MS, useApprovals, useControl, useProjects, useRuns, useStats } from '../queries';
import type { OnettEvent, Problem } from '../types';

export { RunItem } from '../components/RunItem';

const ACTIVITY_LIMIT = 100;
/** Rows shown before "Show all": enough to see what just happened without burying the page. */
const ACTIVITY_SHOWN = 10;

export function Overview() {
  const owner = useCan('administer');
  const projects = useProjects();
  const problems = useQuery({ queryKey: keys.problemsAll, queryFn: listProblems, refetchInterval: LIST_REFRESH_MS });
  const approvals = useApprovals('pending');
  const runs = useRuns({ status: 'starting,running', limit: 50 });
  const stats = useStats();
  const control = useControl();
  const now = useNow();
  const needs = useMemo(() => buildNeeds(problems.data ?? [], approvals.data ?? []), [problems.data, approvals.data]);
  const longest = Math.max(0, ...(runs.data ?? []).map((r) => now - (parseTime(r.started_at)?.getTime() ?? now)));
  // The sentence waits for both lists, so it never says "nothing needs you" from half the news.
  const settled = !problems.isPending && !approvals.isPending;
  const count = projects.data?.length;
  const verdict = verdictFor({
    projects: count === undefined ? undefined : count === 0 || settled ? count : undefined,
    owner,
    needs,
    activeRuns: stats.data?.active_runs ?? runs.data?.length ?? 0,
    paused: control.data?.paused ?? false,
    now,
    failed: (problems.isError && !problems.data) || (approvals.isError && !approvals.data),
  });

  return (
    <div className="overview">
      <h1 className="sr-only">Overview</h1>
      <BoardVerdict verdict={verdict} />
      <ErrorLine error={problems.error ?? approvals.error ?? runs.error} />
      {count ? <OwnerUnreachableBanner /> : null}
      <HoldBanner />
      <SetupChecklist form="stations" />
      <AndonBoard needs={needs} runs={runs.data ?? []} scale={axisMinutes(longest)} now={now} />
      <SetupChecklist form="strip" />
      <div className="ov-foot">
        <HeartbeatCard />
        <Activity problems={problems.data ?? []} />
      </div>
    </div>
  );
}

export function Activity({ problems }: { problems: Problem[] }) {
  const history = useQuery({
    queryKey: keys.eventsHistory,
    queryFn: () => getEventsHistory({ limit: ACTIVITY_LIMIT }),
    refetchInterval: 30_000,
  });
  const [live, setLive] = useState<OnettEvent[]>([]);
  const [showAll, setShowAll] = useState(false);

  useEffect(
    () =>
      subscribe((ev) => {
        // Output and chat deltas are not activity; they would flood the feed.
        if (ev.type === 'run.log' || ev.type === 'chat.delta') return;
        setLive((prev) => [ev, ...prev].slice(0, ACTIVITY_LIMIT));
      }),
    [],
  );

  const events = useMemo(() => {
    const seen = new Set<string>();
    const merged: OnettEvent[] = [];
    for (const ev of [...live, ...(history.data ?? [])]) {
      const key = ev.id > 0 ? `id:${ev.id}` : `${ev.type}:${ev.time}:${ev.message}`;
      if (seen.has(key)) continue;
      seen.add(key);
      merged.push(ev);
    }
    return merged
      .sort((a, b) => (parseTime(b.time)?.getTime() ?? 0) - (parseTime(a.time)?.getTime() ?? 0))
      .slice(0, ACTIVITY_LIMIT);
  }, [live, history.data]);
  const now = useNow();
  // Whole minutes: the rows only change their words when the minute does.
  const minute = Math.floor(now / 60_000) * 60_000;
  const rows = useMemo(() => feedRows(events, problems, minute), [events, problems, minute]);

  return (
    <section className="section" aria-labelledby="activity">
      <h2 id="activity">
        Activity <span className="section-note">newest first</span>
      </h2>
      <ErrorLine error={history.error} />
      {history.isSuccess && rows.length === 0 ? <Empty>No activity yet. Events appear here as work moves.</Empty> : null}
      <ol className="feed ov-feed">
        {(showAll ? rows : rows.slice(0, ACTIVITY_SHOWN)).map((row) => (
          <li key={row.key} className="feed-row">
            <time className="feed-time" dateTime={row.time?.toISOString()} title={row.time ? formatDateTime(row.time) : undefined}>
              {row.time ? shortTime(row.time, minute) : '—'}
            </time>
            <span className="feed-message">
              {row.lamp ? <span className={`lamp lamp-${row.lamp}`} aria-hidden="true" /> : null}
              {row.parts.map((part, i) =>
                part.to ? (
                  <Link key={i} to={part.to}>
                    {part.text}
                  </Link>
                ) : (
                  <span key={i}>{part.text}</span>
                ),
              )}
              {row.note ? <span className="ov-feed-note"> · {row.note}</span> : null}
            </span>
            {row.project ? <span className="ov-feed-where">{row.project}</span> : null}
          </li>
        ))}
      </ol>
      {rows.length > ACTIVITY_SHOWN ? (
        <button type="button" className="link-button small feed-more" aria-expanded={showAll} onClick={() => setShowAll(!showAll)}>
          {showAll ? 'Show fewer' : `Show all ${rows.length}`}
        </button>
      ) : null}
    </section>
  );
}
