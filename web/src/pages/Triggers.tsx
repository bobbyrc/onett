import './Triggers.css';
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useCan } from '../auth';
import { Button } from '../components/Button';
import { Empty } from '../components/Empty';
import { ErrorLine } from '../components/ErrorLine';
import { PageHeader } from '../components/PageHeader';
import { RelTime } from '../components/RelTime';
import { TriggerDialog } from '../components/TriggerDialog';
import { useNow } from '../lib/clock';
import { cx, safeHref } from '../lib/format';
import type { TriggerStarter } from '../lib/triggerForm';
import {
  firingWhy,
  formatHourMinute,
  groupByDay,
  outcomeLamp,
  outcomeWord,
  runNote,
  sortTriggers,
  triggerCounts,
  triggerState,
  zoneWords,
} from '../lib/triggers';
import { describeCron } from '../lib/triggerSchedule';
import { parseTime } from '../lib/time';
import { useParamState } from '../useParamState';
import { useProjects } from '../queries';
import { useAllTriggerFirings, usePatchTrigger, useRunTrigger, useTriggers } from '../triggerQueries';
import type { Trigger, TriggerFiring } from '../types';

/** The example triggers an empty page offers an owner. */
const STARTERS: { when: string; starter: TriggerStarter }[] = [
  {
    when: 'Every day at 05:00',
    starter: { kind: 'schedule', cron: '0 5 * * *', prompt: 'Address any open issues in this repository.' },
  },
  {
    when: 'A new issue is opened',
    starter: { kind: 'github_issue', prompt: 'Take a look at the new issue and say what you would do about it.' },
  },
];

/**
 * The Triggers page: a card per trigger beside one log of every trigger's recent firings. On a
 * phone the two are tabs. Only an owner sees the controls; the server refuses everyone else anyway.
 */
export function Triggers() {
  const canAdminister = useCan('administer');
  const triggers = useTriggers();
  const firings = useAllTriggerFirings();
  const projects = useProjects();
  const run = useRunTrigger();
  const patch = usePatchTrigger();
  const [tab, setTab] = useParamState('tab');
  const [dialog, setDialog] = useState<{ trigger?: Trigger; starter?: TriggerStarter } | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const now = useNow();

  const list = sortTriggers(triggers.data ?? []);
  const counts = triggerCounts(list);
  const byId = new Map(list.map((t) => [t.id, t]));
  const groups = groupByDay(firings.data ?? [], now);
  const repoOf = (project: string) => projects.data?.find((p) => p.name === project)?.repo || project;

  return (
    <>
      <PageHeader
        title="Triggers"
        actions={
          canAdminister ? (
            <Button variant="primary" onClick={() => setDialog({})}>
              New trigger
            </Button>
          ) : undefined
        }
      />
      {canAdminister ? null : <p className="muted small trg-note">Only an owner can add or change triggers.</p>}
      {note ? (
        <p className="muted small trg-note" role="status">
          {note}
        </p>
      ) : null}
      <ErrorLine error={triggers.error ?? firings.error} />
      <ErrorLine error={run.error ?? patch.error} />

      <div className="segmented trg-tabs" role="tablist" aria-label="Triggers and firings">
        <button
          type="button"
          role="tab"
          aria-selected={tab !== 'firings'}
          className={tab !== 'firings' ? 'on' : undefined}
          onClick={() => setTab('')}
        >
          Triggers
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'firings'}
          className={tab === 'firings' ? 'on' : undefined}
          onClick={() => setTab('firings')}
        >
          Firings
        </button>
      </div>

      {triggers.isSuccess ? (
        <div className={cx('trg-cols', tab === 'firings' ? 'tab-firings' : 'tab-rules')}>
          <section className="section trg-rules" aria-labelledby="trg-rules-h">
            <h2 id="trg-rules-h">
              Rules{' '}
              {list.length > 0 ? (
                <span className="section-note">
                  {counts.on} on · {counts.off} off ·{' '}
                  <span className={counts.failing > 0 ? 'danger-text' : undefined}>{counts.failing} failing</span>
                </span>
              ) : null}
            </h2>
            {list.length === 0 ? (
              <>
                <Empty>
                  No triggers. A trigger starts the orchestrator on its own, on a schedule or when a new issue is opened.
                </Empty>
                {canAdminister ? (
                  <>
                    <p className="muted small trg-starters-label">Or start from an example:</p>
                    {STARTERS.map((s) => (
                      <div className="trg-starter" key={s.when}>
                        <dl className="trg-rule-body">
                          <dt>When</dt>
                          <dd>{s.when}</dd>
                          <dt>Ask</dt>
                          <dd>{s.starter.prompt}</dd>
                        </dl>
                        <Button aria-label={`Use this example: ${s.when}`} onClick={() => setDialog({ starter: s.starter })}>
                          Use this
                        </Button>
                      </div>
                    ))}
                  </>
                ) : null}
              </>
            ) : (
              list.map((t) => (
                <TriggerCard
                  key={t.id}
                  trigger={t}
                  now={now}
                  canAdminister={canAdminister}
                  repo={repoOf(t.project)}
                  running={run.isPending && run.variables === t.id}
                  saving={patch.isPending && patch.variables?.id === t.id}
                  onRun={() => {
                    setNote(null);
                    run.mutate(t.id, { onSuccess: (f) => setNote(runNote(t.name, f)) });
                  }}
                  onEdit={() => setDialog({ trigger: t })}
                  onToggle={() => patch.mutate({ id: t.id, patch: { enabled: !t.enabled } })}
                />
              ))
            )}
          </section>

          <section className="section trg-log" aria-labelledby="trg-log-h">
            <h2 id="trg-log-h">
              Recent firings{' '}
              {(firings.data ?? []).length > 0 ? <span className="section-note">all triggers</span> : null}
            </h2>
            {firings.isSuccess && (firings.data ?? []).length === 0 ? (
              <Empty>Nothing has fired yet. Each firing is listed here with what started it and what came of it.</Empty>
            ) : (
              groups.map((g, i) => (
                <div key={g.label}>
                  <h3 className={cx('trg-day', i === 0 && 'trg-day-first')}>{g.label}</h3>
                  <ol className="trg-feed">
                    {g.firings.map((f) => (
                      <FiringRow key={f.id} firing={f} trigger={byId.get(f.trigger_id)} />
                    ))}
                  </ol>
                </div>
              ))
            )}
          </section>
        </div>
      ) : null}

      {dialog ? (
        <TriggerDialog trigger={dialog.trigger} starter={dialog.starter} onClose={() => setDialog(null)} />
      ) : null}
    </>
  );
}

/** One trigger's card: its lamp and words, the owner's controls, and what it does and last did. */
function TriggerCard({
  trigger: t,
  now,
  canAdminister,
  repo,
  running,
  saving,
  onRun,
  onEdit,
  onToggle,
}: {
  trigger: Trigger;
  now: number;
  canAdminister: boolean;
  repo: string;
  running: boolean;
  saving: boolean;
  onRun: () => void;
  onEdit: () => void;
  onToggle: () => void;
}) {
  const state = triggerState(t, now);
  const f = t.last_firing;
  const conversationId = f?.conversation_id ?? t.conversation_id;

  const when =
    t.kind === 'schedule' ? (
      <>
        {describeCron(t.cron ?? '') ?? <code>{t.cron}</code>} <span className="muted">· {zoneWords(t.time_zone)}</span>
      </>
    ) : (
      <>
        A new issue in {repo} <span className="muted">· {t.label ? `label ${t.label}` : 'any label'}</span>
      </>
    );

  return (
    <article className="trg-rule" data-lamp={state.lamp}>
      <header className="trg-rule-head">
        <div className="trg-rule-id">
          <h3 className="trg-rule-name">
            <Link to={`/triggers/${t.id}`}>{t.name}</Link> <span className="tag">{t.project}</span>
          </h3>
          <p className="trg-rule-state">
            <strong>{state.strong}</strong>
            {state.rest.map((r) => ` · ${r}`).join('')}
          </p>
        </div>
        {canAdminister ? (
          <div className="row-actions">
            {t.kind === 'schedule' ? (
              <Button
                disabled={running}
                aria-busy={running}
                aria-label={running ? undefined : `Run ${t.name} now`}
                onClick={onRun}
              >
                {running ? 'Running…' : 'Run now'}
              </Button>
            ) : null}
            <Button aria-label={`Edit ${t.name}`} onClick={onEdit}>
              Edit
            </Button>
            <Button
              variant="ghost"
              disabled={saving}
              aria-label={`${t.enabled ? 'Turn off' : 'Turn on'} ${t.name}`}
              onClick={onToggle}
            >
              {t.enabled ? 'Turn off' : 'Turn on'}
            </Button>
          </div>
        ) : null}
      </header>
      <dl className="trg-rule-body">
        <dt>When</dt>
        <dd>{when}</dd>
        <dt>Ask</dt>
        <dd className="trg-clamp">{t.prompt}</dd>
        <dt>Last</dt>
        <dd>
          {f ? <LastFiring trigger={t} firing={f} conversationId={conversationId} /> : <span className="muted">never</span>}
        </dd>
      </dl>
    </article>
  );
}

/** The last firing on a card, in one line, with links to its conversation and to the trigger's firings. */
function LastFiring({
  trigger: t,
  firing: f,
  conversationId,
}: {
  trigger: Trigger;
  firing: TriggerFiring;
  conversationId: string | undefined;
}) {
  return (
    <>
      <RelTime value={f.at} /> <span className="muted">· {firingWhy(f, t.time_zone)} ·</span> <Outcome firing={f} />
      {f.outcome === 'started' && conversationId ? (
        <>
          {' · '}
          <Link to={`/chat/${conversationId}`}>Open conversation</Link>
        </>
      ) : null}
      {' · '}
      <Link to={`/triggers/${t.id}`}>All firings</Link>
    </>
  );
}

/** A firing's outcome in words, with its reason when it has one. */
function Outcome({ firing: f }: { firing: TriggerFiring }) {
  if (f.outcome === 'started') return <>Started</>;
  if (f.outcome === 'skipped') {
    return <span className="warn-text">{f.reason ? `Skipped: ${f.reason}` : 'Skipped'}</span>;
  }
  return <span className="danger-text">{f.reason ? `Failed: ${f.reason}` : 'Failed'}</span>;
}

/** One line of the firings log: when, what came of it, which trigger, and what started it. */
function FiringRow({ firing: f, trigger: t }: { firing: TriggerFiring; trigger: Trigger | undefined }) {
  const date = parseTime(f.at);
  // The log is grouped by day from the same times, so a firing with no readable time never gets here.
  if (!date) return null;
  const href = safeHref(f.issue_url);
  const issue = href ? (
    <a href={href} target="_blank" rel="noopener noreferrer">
      issue #{f.issue_number}
    </a>
  ) : (
    `issue #${f.issue_number}`
  );

  return (
    <li className="trg-fire">
      <time className="trg-fire-time" dateTime={date.toISOString()}>
        {formatHourMinute(date)}
      </time>
      <span className="trg-status">
        <span className={`trg-lamp trg-lamp-${outcomeLamp(f.outcome)}`} aria-hidden="true" />
        {outcomeWord(f.outcome)}
      </span>
      <div className="trg-fire-body">
        <strong>{t?.name ?? 'Deleted trigger'}</strong>{' '}
        {f.cause === 'github_issue' ? (
          <>
            <span className="muted">·</span> {issue}
          </>
        ) : (
          <span className="muted">· {firingWhy(f, t?.time_zone)}</span>
        )}
        {f.outcome === 'started' && f.conversation_id ? (
          <span className="trg-sub">
            <Link to={`/chat/${f.conversation_id}`}>Open conversation</Link>
          </span>
        ) : null}
        {f.outcome === 'skipped' && f.reason ? <span className="trg-sub warn-text">{f.reason}</span> : null}
        {f.outcome === 'failed' && f.reason ? <span className="trg-sub danger-text">{f.reason}</span> : null}
      </div>
    </li>
  );
}
