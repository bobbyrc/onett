import { Fragment } from 'react';
import { Link } from 'react-router-dom';
import { useCan } from '../auth';
import { DEPLOYMENT_LIMIT, useControl, useDeployments, useProjects, useStats } from '../queries';
import { formatCost } from '../lib/format';
import { lampDetail } from '../lib/lamp';
import { boardLanes } from '../lib/lanes';
import type { Need } from '../lib/needs';
import { deployStopped, deployWords, latestDeploys } from '../lib/boardDeploy';
import type { Deployment, ProjectView, Run } from '../types';
import { ErrorLine } from './ErrorLine';
import { NeedRow } from './NeedRow';
import { RelTime } from './RelTime';
import { RunItem } from './RunItem';
import './AndonBoard.css';

/**
 * The Overview's board, one lane per project: its window (lit by what the project needs and
 * labelled with what it is doing), what needs the owner there, and what is running there. A
 * project with nothing going on folds into one quiet line. A window links to that project's
 * open tasks.
 */
export function AndonBoard({ needs = [], runs = [], scale = 0, now = 0 }: { needs?: Need[]; runs?: Run[]; scale?: number; now?: number }) {
  const projects = useProjects();
  const owner = useCan('administer');
  const deployments = useDeployments('');
  const latest = latestDeploys(deployments.data ?? []);
  // Only a loaded list shorter than the limit can say a project never deployed.
  const allDeploys = deployments.isSuccess && deployments.data.length < DEPLOYMENT_LIMIT;

  if (projects.error) return <ErrorLine error={projects.error} />;
  if (!projects.data) return null;
  if (projects.data.length === 0) {
    return (
      // Only an owner has the Add project button there.
      <p className="empty ov-lanes-empty">
        {owner ? 'No projects yet. Add one on the ' : 'No projects yet. An owner can add one on the '}
        <Link to="/projects">Projects page</Link>
        {owner ? ' and it appears here.' : '.'}
      </p>
    );
  }

  const board = boardLanes(projects.data, needs, runs);
  // "Start here" shows only when there is a choice to make.
  const firstKey = needs.length >= 2 ? needs[0].key : '';

  return (
    <section className="ov-lanes" aria-label="Projects">
      {board.lanes.map((lane) => {
        const detail = lampDetail(lane.project, lane.state);
        return (
          <div key={lane.project.name} className="ov-lane">
            <Link to={`/tasks?project=${encodeURIComponent(lane.project.name)}`} className={`board-window lamp-${lane.state.lamp}`}>
              <span className="board-lamp" aria-hidden="true" />
              <span className="board-name">{lane.project.name}</span>
              <span className="board-count">{lane.state.label}</span>
              {detail ? <span className="board-detail">{detail}</span> : null}
              <DeployLine project={lane.project} last={latest.get(lane.project.name)} allDeploys={allDeploys} />
            </Link>
            <div className="ov-needs">
              {lane.needs.length > 0 ? (
                lane.needs.map((need) => <NeedRow key={need.key} need={need} first={need.key === firstKey} now={now} />)
              ) : (
                <p className="ov-none">Nothing needs you here.</p>
              )}
            </div>
            <Flight runs={lane.runs} queued={lane.project.counts.queued} scale={scale} now={now} />
          </div>
        );
      })}
      {board.loose.length > 0 ? (
        <div className="ov-lane">
          <div className="board-window lamp-run">
            <span className="board-lamp" aria-hidden="true" />
            <span className="board-name">No project</span>
            <span className="board-count">{board.loose.length} running</span>
          </div>
          <div className="ov-needs">
            <p className="ov-none">Runs that belong to no project.</p>
          </div>
          <Flight runs={board.loose} queued={0} scale={scale} now={now} />
        </div>
      ) : null}
      {board.quiet.length > 0 ? (
        <div className="ov-lane-quiet">
          <span className="lamp lamp-off" aria-hidden="true" />
          <span>
            <b>Quiet:</b>{' '}
            {board.quiet.map((project, index) => (
              <Fragment key={project.name}>
                {index > 0 ? ', ' : null}
                <Link to={`/tasks?project=${encodeURIComponent(project.name)}`}>{project.name}</Link>
              </Fragment>
            ))}
            {' · no work in flight'}
          </span>
        </div>
      ) : null}
    </section>
  );
}

/** What is running in a lane, and how much waits behind it. */
function Flight({ runs, queued, scale, now }: { runs: Run[]; queued: number; scale: number; now: number }) {
  const note =
    runs.length === 0 ? (queued > 0 ? `Nothing running. ${queued} queued.` : 'Nothing running.') : queued > 0 ? `${queued} queued` : '';
  return (
    <div className="ov-flight">
      <p className="ov-label">In flight</p>
      {runs.length > 0 ? (
        <ul className="run-list">
          {runs.map((run) => (
            <RunItem key={run.id} run={run} scale={scale} now={now} inLane />
          ))}
        </ul>
      ) : null}
      {note ? <p className="ov-flight-note">{note}</p> : null}
    </div>
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
export function LineWindow() {
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
