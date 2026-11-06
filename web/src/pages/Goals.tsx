import { Link } from 'react-router-dom';
import type { Goal } from '../types';
import { useGoals } from '../queries';
import { useCan } from '../auth';
import { goalActions } from '../lib/goals';
import { useParamState } from '../useParamState';
import { ErrorLine } from '../components/ErrorLine';
import { Field } from '../components/Field';
import { GoalActions } from '../components/GoalActions';
import { PageHeader } from '../components/PageHeader';
import { ProjectSelect } from '../components/ProjectSelect';
import { RelTime } from '../components/RelTime';
import { StatusPill } from '../components/StatusPill';
import { Table, type Column } from '../components/Table';

const STATUSES = ['active', 'paused', 'blocked', 'done'];

const baseColumns: Column<Goal>[] = [
  { key: 'status', header: 'Status', cell: (g) => <StatusPill kind="goal" status={g.status} /> },
  { key: 'title', header: 'Goal', cell: (g) => <Link to={`/goals/${g.id}`}>{g.title}</Link> },
  { key: 'project', header: 'Project', cell: (g) => g.project },
  { key: 'turns', header: 'Turns', cell: (g) => g.turns },
  { key: 'updated', header: 'Updated', cell: (g) => <RelTime value={g.updated_at} /> },
];
const controlsColumn: Column<Goal> = { key: 'actions', header: 'Controls', cell: (g) => <GoalActions goal={g} /> };

export function Goals() {
  const [project, setProject] = useParamState('project');
  const [status, setStatus] = useParamState('status');
  const goals = useGoals({ project: project || undefined, status: status || undefined });
  const canOperate = useCan('operate');
  // Done goals have no controls; skip the column when no row in view has any.
  const showControls = canOperate && (goals.data ?? []).some((g) => goalActions(g.status).length > 0);
  const columns = showControls ? [...baseColumns, controlsColumn] : baseColumns;
  return (
    <>
      <PageHeader title="Goals" />
      <div className="filter-bar">
        <ProjectSelect value={project} onChange={setProject} />
        <Field label="Status">
          <select value={status} onChange={(event) => setStatus(event.target.value)}>
            <option value="">All</option>
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        </Field>
      </div>
      <ErrorLine error={goals.error} />
      {goals.isSuccess ? (
        <Table
          columns={columns}
          rows={goals.data}
          rowKey={(g) => g.id}
          empty="No goals. A goal keeps the orchestrator working unattended on an objective you state."
        />
      ) : null}
    </>
  );
}
