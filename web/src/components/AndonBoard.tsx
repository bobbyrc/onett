import type { CSSProperties } from 'react';
import { Link } from 'react-router-dom';
import { DEPLOYMENT_LIMIT, useControl, useDeployments, useProjects, useStats } from '../queries';
import { formatCost } from '../lib/format';
import { lampDetail, projectLamp } from '../lib/lamp';
import { deployStopped, deployWords, latestDeploys } from '../lib/boardDeploy';
import type { Deployment, ProjectView } from '../types';
import { ErrorLine } from './ErrorLine';
import { RelTime } from './RelTime';
import './AndonBoard.css';

/**
 * The Overview's lamp board: one window per project, lit by what the project needs, and the line
 * window that says whether the factory runs. A window links to that project's open tasks.
 */
export function AndonBoard() {
  const projects = useProjects();
  const deployments = useDeployments('');
  const latest = latestDeploys(deployments.data ?? []);
  // Only a loaded list shorter than the limit can say a project never deployed.
  const allDeploys = deployments.isSuccess && deployments.data.length < DEPLOYMENT_LIMIT;
  const count = projects.data?.length ?? 0;
  // The board is as wide as its windows (styles.css), so one project is not a wide empty board.
  const width = count > 0 ? ({ '--board-windows': count } as CSSProperties) : undefined;

  return (
    <section className="board" aria-label="Projects" style={width}>
      <div className="board-windows">
        {projects.data?.map((project) => {
          const state = projectLamp(project);
          const detail = lampDetail(project, state);
          return (
            <Link key={project.name} to={`/tasks?project=${encodeURIComponent(project.name)}`} className={`board-window lamp-${state.lamp}`}>
              <span className="board-lamp" aria-hidden="true" />
              <span className="board-name">{project.name}</span>
              <span className="board-count">{state.label}</span>
              {detail ? <span className="board-detail">{detail}</span> : null}
              <DeployLine project={project} last={latest.get(project.name)} allDeploys={allDeploys} />
            </Link>
          );
        })}
        {projects.isSuccess && projects.data.length === 0 ? (
          <p className="board-empty">
            No projects yet. Add one on the <Link to="/projects">Projects page</Link> and it appears here.
          </p>
        ) : null}
        {projects.error ? (
          <div className="board-empty">
            <ErrorLine error={projects.error} />
          </div>
        ) : null}
      </div>
      <LineWindow />
    </section>
  );
}

/** The project's last deploy in words, or why there is none. */
function DeployLine({ project, last, allDeploys }: { project: ProjectView; last: Deployment | undefined; allDeploys: boolean }) {
  if (!last) {
    if (!project.deploy) return <span className="board-deploy">No deployer</span>;
    return allDeploys ? <span className="board-deploy">No deploys yet</span> : null;
  }
  return (
    <span className="board-deploy">
      <span className={deployStopped(last.state) ? 'board-stop-word' : undefined}>{deployWords(last.state)}</span>
      {' · '}
      <RelTime value={last.updated_at} />
    </span>
  );
}

/** Whether the line runs, and the factory-wide numbers that used to be stat cards. */
function LineWindow() {
  const control = useControl();
  const stats = useStats();
  const paused = control.data?.paused ?? false;
  const s = stats.data;

  return (
    <div className="board-line">
      <span className="board-line-state">
        <span className={`lamp ${paused ? 'lamp-stop' : 'lamp-run'}`} aria-hidden="true" />
        {control.data ? (paused ? 'Line paused' : 'Line running') : 'Line'}
      </span>
      <span className="board-line-stats">
        <span>
          <b>{s?.active_runs ?? '—'}</b> active
        </span>
        <span>
          <b>{s?.tasks_by_status.queued ?? 0}</b> queued
        </span>
        <span>
          <b>{s ? formatCost(s.cost_today_usd) : '—'}</b> today
        </span>
      </span>
    </div>
  );
}
