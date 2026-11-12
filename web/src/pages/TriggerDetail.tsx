import { useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import './Triggers.css';
import type { TriggerFiring } from '../types';
import { useCan } from '../auth';
import { Button } from '../components/Button';
import { ErrorLine } from '../components/ErrorLine';
import { NotFound, isNotFound } from '../components/NotFound';
import { PageHeader } from '../components/PageHeader';
import { RelTime } from '../components/RelTime';
import { Table, type Column } from '../components/Table';
import { TriggerDialog } from '../components/TriggerDialog';
import { useNow } from '../lib/clock';
import { cx, safeHref } from '../lib/format';
import { repoUrl } from '../lib/projects';
import { formatRelative, parseTime } from '../lib/time';
import { describeCron } from '../lib/triggerSchedule';
import { firingWhy, formatDayTime, outcomeTone, runNote, triggerStatus } from '../lib/triggers';
import { useProjects } from '../queries';
import { useDeleteTrigger, usePatchTrigger, useRunTrigger, useTrigger, useTriggerFirings } from '../triggerQueries';

const CONFIRM_MS = 3_000;

/**
 * One trigger: its schedule or issue rule, its prompt, the link to its one conversation and every
 * firing. Only an owner sees Run now, Edit, Turn off and Delete. Delete asks first, because it also
 * removes the firing history.
 */
export function TriggerDetail() {
  const { id = '' } = useParams();
  const detail = useTrigger(id);
  const firings = useTriggerFirings(id);
  const projects = useProjects();
  const run = useRunTrigger();
  const patch = usePatchTrigger();
  const remove = useDeleteTrigger();
  const canAdminister = useCan('administer');
  const navigate = useNavigate();
  const now = useNow();
  const [editing, setEditing] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const [asking, setAsking] = useState(false);

  // The Delete button's "Confirm?" turns itself off after a few seconds, as Button's own does.
  useEffect(() => {
    if (!asking) return;
    const timer = window.setTimeout(() => setAsking(false), CONFIRM_MS);
    return () => window.clearTimeout(timer);
  }, [asking]);

  if (detail.isError) return isNotFound(detail.error) ? <NotFound what="trigger" /> : <ErrorLine error={detail.error} />;
  if (!detail.data) return <p className="muted">Loading trigger…</p>;

  const t = detail.data;
  const status = triggerStatus(t);
  const schedule = t.kind === 'schedule';
  const repo = projects.data?.find((p) => p.name === t.project)?.repo ?? '';
  const url = repoUrl(repo);

  const runNow = () => {
    setNote(null);
    run.mutate(t.id, { onSuccess: (f) => setNote(runNote(t.name, f)) });
  };
  const toggle = () => patch.mutate({ id: t.id, patch: { enabled: !t.enabled } });
  const del = () => {
    if (!asking) {
      setAsking(true);
      return;
    }
    remove.mutate(t.id, { onSuccess: () => navigate('/triggers') });
  };

  const columns: Column<TriggerFiring>[] = [
    {
      key: 'when',
      header: 'When',
      cell: (f) => {
        const date = parseTime(f.at);
        return (
          <>
            <span className="trg-when-abs">{date ? formatDayTime(date, t.time_zone) : '—'}</span>{' '}
            <RelTime value={f.at} className="muted trg-rel" />
          </>
        );
      },
    },
    {
      key: 'why',
      header: 'Why',
      cell: (f) => {
        const href = f.cause === 'github_issue' ? safeHref(f.issue_url) : undefined;
        return href ? (
          <a href={href} target="_blank" rel="noopener noreferrer">
            Issue #{f.issue_number}
          </a>
        ) : (
          firingWhy(f, t.time_zone, true)
        );
      },
    },
    {
      key: 'outcome',
      header: 'Outcome',
      wide: true,
      cell: (f) => (
        <div className="trg-outcome">
          <span className={cx('pill', `tone-${outcomeTone(f.outcome)}`)}>
            <span className="dot" aria-hidden="true" />
            {f.outcome}
          </span>
          {f.reason ? <span className="trg-rel">{f.reason}</span> : null}
        </div>
      ),
    },
  ];

  const next = parseTime(t.next_run_at);
  const since = parseTime(t.issues_after);

  return (
    <>
      <PageHeader
        title={
          <>
            {t.name} <span className={cx('pill', `tone-${status.tone}`)}><span className="dot" aria-hidden="true" />{status.word}</span>
          </>
        }
        actions={
          canAdminister ? (
            <>
              {schedule ? (
                <Button disabled={run.isPending} aria-busy={run.isPending} onClick={runNow}>
                  {run.isPending ? 'Running…' : 'Run now'}
                </Button>
              ) : null}
              <Button onClick={() => setEditing(true)}>Edit</Button>
              <Button disabled={patch.isPending} onClick={toggle}>
                {t.enabled ? 'Turn off' : 'Turn on'}
              </Button>
              <Button variant="danger" disabled={remove.isPending} onClick={del}>
                {asking ? 'Confirm?' : 'Delete'}
              </Button>
            </>
          ) : undefined
        }
      />
      <p className="trg-meta-line muted">
        <span>
          {schedule ? 'Schedule' : 'GitHub issue'} · {t.project}
        </span>
        {t.conversation_id ? <Link to={`/chat/${t.conversation_id}`}>Open conversation</Link> : null}
      </p>
      {asking ? (
        <p className="muted small trg-note" role="status">
          Deletes the trigger and its firing history. Its conversation stays in Chat.
        </p>
      ) : null}
      {note ? (
        <p className="muted small trg-note" role="status">
          {note}
        </p>
      ) : null}
      <ErrorLine error={run.error ?? patch.error ?? remove.error} />
      <dl className="meta">
        {schedule ? (
          <>
            <div className="meta-item">
              <dt>When</dt>
              <dd>{describeCron(t.cron ?? '') ?? 'Custom schedule'}</dd>
            </div>
            <div className="meta-item">
              <dt>Time zone</dt>
              <dd>{t.time_zone || "Daemon's time zone"}</dd>
            </div>
            <div className="meta-item">
              <dt>Next firing</dt>
              <dd>
                {next ? (
                  <>
                    {formatDayTime(next, t.time_zone)} <span className="muted trg-rel">{formatRelative(next, now)}</span>
                  </>
                ) : (
                  <span className="muted">none while it is off</span>
                )}
              </dd>
            </div>
            <div className="meta-item">
              <dt>Cron</dt>
              <dd>
                <code>{t.cron}</code>
              </dd>
            </div>
          </>
        ) : (
          <>
            <div className="meta-item">
              <dt>When</dt>
              <dd>When an issue is opened</dd>
            </div>
            <div className="meta-item">
              <dt>Repository</dt>
              <dd>
                {url ? (
                  <a href={url} target="_blank" rel="noopener noreferrer">
                    {repo}
                  </a>
                ) : (
                  repo || t.project
                )}
              </dd>
            </div>
            <div className="meta-item">
              <dt>Label</dt>
              <dd>{t.label || 'any label'}</dd>
            </div>
            <div className="meta-item">
              <dt>Issues since</dt>
              <dd>{since ? formatDayTime(since) : <span className="muted">—</span>}</dd>
            </div>
          </>
        )}
      </dl>
      <section className="section">
        <h2>Prompt</h2>
        <p className="card trg-prompt">{t.prompt}</p>
      </section>
      <section className="section">
        <h2>
          Firings <span className="section-note">newest first</span>
        </h2>
        {firings.isSuccess ? (
          <Table columns={columns} rows={firings.data} rowKey={(f) => f.id} empty="This trigger has not fired yet." />
        ) : null}
        <ErrorLine error={firings.error} />
      </section>
      {editing ? <TriggerDialog trigger={t} onClose={() => setEditing(false)} /> : null}
    </>
  );
}
