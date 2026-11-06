import { Link, useParams } from 'react-router-dom';
import type { Task } from '../types';
import { useGoal } from '../queries';
import { Callout } from '../components/Callout';
import { ErrorLine } from '../components/ErrorLine';
import { GoalActions } from '../components/GoalActions';
import { IdChip } from '../components/IdChip';
import { Markdown } from '../components/Markdown';
import { PageHeader } from '../components/PageHeader';
import { RelTime } from '../components/RelTime';
import { StatusPill } from '../components/StatusPill';
import { Table, type Column } from '../components/Table';
import './GoalDetail.css';

const columns: Column<Task>[] = [
  { key: 'status', header: 'Status', cell: (t) => <StatusPill status={t.status} /> },
  { key: 'title', header: 'Task', wide: true, cell: (t) => <Link to={`/tasks/${t.id}`}>{t.title}</Link> },
  { key: 'kind', header: 'Kind', cell: (t) => t.kind },
  { key: 'updated', header: 'Updated', cell: (t) => <RelTime value={t.updated_at} /> },
];

export function GoalDetail() {
  const { id = '' } = useParams();
  const detail = useGoal(id);
  if (detail.isError) return <ErrorLine error={detail.error} />;
  if (!detail.data) return <p className="muted">Loading goal…</p>;

  const { goal, tasks } = detail.data;
  return (
    <>
      <PageHeader
        title={
          <>
            {goal.title} <StatusPill kind="goal" status={goal.status} />
          </>
        }
        actions={<GoalActions goal={goal} />}
      />
      <p className="task-meta muted">
        <span>
          {goal.project} · {goal.turns} orchestrator {goal.turns === 1 ? 'turn' : 'turns'}
        </span>
        <IdChip id={goal.id} />
        {goal.conversation_id ? <Link to={`/chat/${goal.conversation_id}`}>Open conversation</Link> : null}
      </p>
      {goal.summary ? (
        <Callout tone={goal.status === 'blocked' ? 'danger' : 'info'} title={goal.status === 'blocked' ? 'Blocked' : 'Summary'}>{goal.summary}</Callout>
      ) : null}
      {goal.body ? (
        <div className="goal-body">
          <Markdown text={goal.body} />
        </div>
      ) : null}
      <section className="section">
        <h2>Tasks</h2>
        <Table columns={columns} rows={tasks} rowKey={(t) => t.id} empty="No tasks have been queued for this goal yet." />
      </section>
    </>
  );
}
