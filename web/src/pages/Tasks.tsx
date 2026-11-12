import { useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import type { Task, TaskKind } from '../types';
import { useCan } from '../auth';
import { usePausedProjects, useTasks } from '../queries';
import { useParamState } from '../useParamState';
import { Button } from '../components/Button';
import { Empty } from '../components/Empty';
import { ErrorLine } from '../components/ErrorLine';
import { Field } from '../components/Field';
import { NewTaskModal } from '../components/NewTaskModal';
import { PageHeader } from '../components/PageHeader';
import { ProjectSelect } from '../components/ProjectSelect';
import { RelTime } from '../components/RelTime';
import { RiskBadge } from '../components/RiskBadge';
import { StatusPill } from '../components/StatusPill';
import { Table, type Column } from '../components/Table';
import { cx, safeHref } from '../lib/format';
import { groupTasks, queueLabel } from '../lib/taskGroups';
import { agentLabel } from '../lib/agentLabel';
import { taskSourceLabel } from '../lib/taskSource';

/** The statuses shown by default: everything the factory is still moving. */
const ACTIVE = ['queued', 'running', 'review', 'awaiting_approval', 'merged', 'deploying', 'blocked'];
const STATUSES = [...ACTIVE, 'done', 'failed', 'cancelled'];
const KINDS: TaskKind[] = ['feature', 'bugfix', 'issue', 'incident', 'review', 'chore', 'improve', 'question'];

/** Queued work in a paused project is not started, so the table says why next to the status. */
function makeColumns(paused: string[]): Column<Task>[] {
  // Every section is its own table; the set widths make their columns line up down the page.
  return [
    {
      key: 'status',
      header: 'Status',
      width: 10.5,
      cell: (t) => (
        <>
          <StatusPill status={t.status} />
          {t.status === 'queued' && queueLabel(t) ? <span className="small"> {queueLabel(t)}</span> : null}
          {t.status === 'queued' && (t.waiting_on?.length ?? 0) > 0 ? (
            <span className="muted small"> waiting for {t.waiting_on?.join(', ')}</span>
          ) : null}
          {t.status === 'queued' && paused.includes(t.project) ? <span className="muted small"> project paused</span> : null}
        </>
      ),
    },
    {
      key: 'title',
      header: 'Title',
      wide: true,
      cell: (t) => {
        const source = taskSourceLabel(t);
        return (
          <div className="cell-stack">
            <Link to={`/tasks/${t.id}`}>{t.title}</Link>
            <div className="muted small">
              <span className="tag">{t.kind}</span> {source.href ? <Link to={source.href}>{source.text}</Link> : source.text}
              {source.ref ? ` · ${source.ref}` : ''}
            </div>
          </div>
        );
      },
    },
    { key: 'project', header: 'Project', width: 7, cell: (t) => t.project },
    { key: 'role', header: 'Role', width: 8.5, cell: (t) => agentLabel(t.role) || '—' },
    { key: 'risk', header: 'Risk', width: 5.5, cell: (t) => <RiskBadge risk={t.risk} /> },
    {
      key: 'pr',
      header: 'PR',
      width: 4,
      cell: (t) =>
        t.pr_number ? (
          safeHref(t.pr_url) ? (
            <a href={safeHref(t.pr_url)} target="_blank" rel="noopener noreferrer">
              #{t.pr_number}
            </a>
          ) : (
            `#${t.pr_number}`
          )
        ) : (
          '—'
        ),
    },
    { key: 'attempts', header: 'Attempts', width: 5.5, cell: (t) => `${t.attempts}/${t.max_attempts}` },
    { key: 'updated', header: 'Updated', width: 7, cell: (t) => <RelTime value={t.updated_at} /> },
  ];
}

export function Tasks() {
  const [params, setParams] = useSearchParams();
  const [project, setProject] = useParamState('project');
  const [kind, setKind] = useParamState('kind');
  const [search, setSearch] = useParamState('q');
  const [sort, setSort] = useParamState('sort');
  const [creating, setCreating] = useState(false);
  const canOperate = useCan('operate');

  // An absent status param means the default active set; an empty one means every status.
  const statusParam = params.get('status');
  const selected = statusParam === null ? ACTIVE : statusParam.split(',').filter(Boolean);
  const setSelected = (next: string[]) =>
    setParams(
      (prev) => {
        const copy = new URLSearchParams(prev);
        copy.set('status', next.join(','));
        return copy;
      },
      { replace: true },
    );
  const toggleStatus = (status: string) =>
    setSelected(selected.includes(status) ? selected.filter((s) => s !== status) : [...selected, status]);
  const isActiveSet = selected.length === ACTIVE.length && ACTIVE.every((s) => selected.includes(s));

  // Exactly the active set means the "active" preset is on; only that chip is then highlighted.
  const paused = usePausedProjects();
  const columns = useMemo(() => makeColumns(paused), [paused]);

  const tasks = useTasks({
    project,
    kind,
    status: selected.join(','),
    sort: sort === 'created' ? 'created' : '',
    limit: 200,
  });

  const rows = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return (tasks.data ?? []).filter((t) => needle === '' || t.title.toLowerCase().includes(needle));
  }, [tasks.data, search]);

  const groups = useMemo(() => (sort === 'created' ? [] : groupTasks(rows)), [rows, sort]);
  const empty = 'No tasks match these filters. Clear a filter, or create a task with New task.';

  return (
    <>
      <PageHeader
        title="Tasks"
        actions={canOperate ? <Button variant="primary" onClick={() => setCreating(true)}>New task</Button> : undefined}
      />
      <div className="filter-bar">
        <ProjectSelect value={project} onChange={setProject} />
        <Field label="Kind">
          <select value={kind} onChange={(event) => setKind(event.target.value)}>
            <option value="">All kinds</option>
            {KINDS.map((k) => (
              <option key={k} value={k}>
                {k}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Order">
          <select value={sort === 'created' ? 'created' : ''} onChange={(event) => setSort(event.target.value)}>
            <option value="">Pickup order</option>
            <option value="created">Newest created</option>
          </select>
        </Field>
        <Field label="Search titles">
          <input type="search" value={search} onChange={(event) => setSearch(event.target.value)} />
        </Field>
      </div>
      <div className="chip-row" role="group" aria-label="Status filter">
        <button type="button" className={cx('chip', isActiveSet && 'chip-on')} aria-pressed={isActiveSet} onClick={() => setSelected(ACTIVE)}>
          active
        </button>
        {STATUSES.map((status) => {
          // While the active preset is on, a member chip is not highlighted; clicking it selects only that status.
          const on = !isActiveSet && selected.includes(status);
          const onClick = () => (isActiveSet ? setSelected([status]) : toggleStatus(status));
          return (
            <button key={status} type="button" className={cx('chip', on && 'chip-on')} aria-pressed={on} onClick={onClick}>
              {status.replace(/_/g, ' ')}
            </button>
          );
        })}
      </div>
      <ErrorLine error={tasks.error} />
      {tasks.isPending ? <p className="muted">Loading tasks…</p> : null}
      {tasks.isSuccess && sort === 'created' ? (
        <Table columns={columns} rows={rows} rowKey={(t) => t.id} empty={empty} />
      ) : null}
      {tasks.isSuccess && sort !== 'created' && groups.length === 0 ? <Empty>{empty}</Empty> : null}
      {tasks.isSuccess && sort !== 'created'
        ? groups.map((g) =>
            g.key === 'finished' ? (
              <details key={g.key} className="section">
                <summary>
                  {g.label} ({g.tasks.length})
                </summary>
                <Table columns={columns} rows={g.tasks} rowKey={(t) => t.id} empty={empty} />
              </details>
            ) : (
              <section key={g.key} className="section" aria-label={g.label}>
                <h2>
                  {g.label} ({g.tasks.length})
                </h2>
                <Table columns={columns} rows={g.tasks} rowKey={(t) => t.id} empty={empty} />
              </section>
            ),
          )
        : null}
      <NewTaskModal open={creating} onClose={() => setCreating(false)} defaultProject={project} />
    </>
  );
}
