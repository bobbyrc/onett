import './Loops.css';
import { useCan } from '../auth';
import { useRunLoop, useLoops } from '../queries';
import { Button } from '../components/Button';
import { Empty } from '../components/Empty';
import { ErrorLine } from '../components/ErrorLine';
import { PageHeader } from '../components/PageHeader';
import { RelTime } from '../components/RelTime';
import type { LoopView } from '../types';
import { GLOBAL_PROJECT, loopGrid } from '../lib/loopGrid';
import { loopLabel, loopStatusWord } from '../lib/loopLabel';
import { formatGoDuration } from '../lib/format';
import { loopHealth } from '../lib/projects';
import { parseTime } from '../lib/time';

/** The label for a project name. A global loop has no project, so it says so. */
const projectLabel = (project: string) => (project === GLOBAL_PROJECT ? 'all projects' : project);

/** A loop's lamp, lit by loopHealth, with its state in words beside it, so colour is not the only signal. */
function Status({ loop }: { loop: LoopView }) {
  const health = loopHealth(loop);
  return (
    <span className="loop-status">
      <span className={`loop-lamp loop-lamp-${health}`} aria-hidden="true" />
      <span className={health === 'danger' ? 'danger-text' : health === 'muted' ? 'muted' : undefined}>{loopStatusWord(loop)}</span>
    </span>
  );
}

/** How often a loop runs, and its next run when the loop reports one (heartbeat). */
function Schedule({ loop }: { loop: LoopView }) {
  const next = loop.enabled && parseTime(loop.next_run_at);
  return (
    <>
      <span className={loop.enabled ? undefined : 'muted'}>every {formatGoDuration(loop.every)}</span>
      {next ? (
        <span className="loop-sub muted">
          next <RelTime value={loop.next_run_at} />
        </span>
      ) : null}
    </>
  );
}

/** When a loop last ran and how many items it found, then its last error, cut to one line. */
function LastRun({ loop }: { loop: LoopView }) {
  return (
    <>
      {parseTime(loop.last_run_at) ? (
        <span>
          <RelTime value={loop.last_run_at} />
          {loop.item_count ? (
            <span className="muted">
              {' · '}
              {loop.item_count} {loop.item_count === 1 ? 'item' : 'items'}
            </span>
          ) : null}
        </span>
      ) : (
        <span className="muted">never</span>
      )}
      {loop.last_error ? (
        <span className="loop-sub danger-text loop-error" title={loop.last_error}>
          {loop.last_error}
        </span>
      ) : null}
    </>
  );
}

/** The loop's readable name over its id, which config and the API use. */
function LoopName({ name }: { name: string }) {
  return (
    <>
      <span className="loop-label">{loopLabel(name)}</span>
      <span className="loop-id mono">{name}</span>
    </>
  );
}

export function Loops() {
  const loops = useLoops();
  const runLoop = useRunLoop();
  const canOperate = useCan('operate');

  // "Run" is an operator action: viewers see the loops without the button or its column.
  const runButton = (loop: LoopView) => {
    const pending = runLoop.isPending && runLoop.variables?.name === loop.name && runLoop.variables.project === loop.project;
    return (
      <Button
        variant="secondary"
        className="loop-run"
        disabled={pending}
        aria-busy={pending}
        aria-label={pending ? undefined : `Run ${loopLabel(loop.name)} for ${projectLabel(loop.project)}`}
        onClick={() => runLoop.mutate({ name: loop.name, project: loop.project })}
      >
        {pending ? 'Running…' : 'Run'}
      </Button>
    );
  };

  const list = loops.isSuccess ? loops.data : [];
  const grid = loopGrid(list);
  // One group per loop name, holding the loop in each project it runs in.
  const groups = grid.names.map((name) => ({
    name,
    loops: grid.projects.map((project) => grid.cell(name, project)).filter((loop): loop is LoopView => loop !== undefined),
  }));
  const enabled = list.filter((l) => l.enabled).length;
  const failing = list.filter((l) => loopHealth(l) === 'danger').length;

  return (
    <>
      <PageHeader title="Loops" />
      <ErrorLine error={loops.error} />
      {runLoop.isSuccess && runLoop.variables ? (
        <p className="muted small" role="status">
          Ran {loopLabel(runLoop.variables.name)} for {projectLabel(runLoop.variables.project)}.
        </p>
      ) : null}
      {loops.isSuccess && list.length === 0 ? (
        <Empty>No loops are configured. Loops are set under loops in onett.yaml.</Empty>
      ) : null}
      {loops.isSuccess && list.length > 0 ? (
        <>
          <p className="muted small loops-summary">
            {enabled} {enabled === 1 ? 'loop' : 'loops'} on · {list.length - enabled} off ·{' '}
            <span className={failing > 0 ? 'danger-text' : undefined}>{failing} failing</span>
          </p>

          <div className="loops-body">
            {/* Wide: one row per loop and project, grouped under the loop's name. */}
            <div className="loops-table-wrap">
              <table className="loops-table">
                <colgroup>
                  <col className="loops-col-name" />
                  <col className="loops-col-project" />
                  <col className="loops-col-status" />
                  <col className="loops-col-schedule" />
                  <col />
                  {canOperate ? <col className="loops-col-action" /> : null}
                </colgroup>
                <thead>
                  <tr>
                    <th scope="col">Loop</th>
                    <th scope="col">Project</th>
                    <th scope="col">Status</th>
                    <th scope="col">Schedule</th>
                    <th scope="col">Last run</th>
                    {canOperate ? (
                      <th scope="col">
                        <span className="sr-only">Action</span>
                      </th>
                    ) : null}
                  </tr>
                </thead>
                {groups.map((group) => (
                  <tbody key={group.name}>
                    {group.loops.map((loop, i) => (
                      <tr key={loop.project}>
                        {i === 0 ? (
                          <th scope="rowgroup" rowSpan={group.loops.length} className="loop-name">
                            <LoopName name={group.name} />
                          </th>
                        ) : null}
                        <td>{projectLabel(loop.project)}</td>
                        <td>
                          <Status loop={loop} />
                        </td>
                        <td>
                          <Schedule loop={loop} />
                        </td>
                        <td>
                          <LastRun loop={loop} />
                        </td>
                        {canOperate ? <td className="loop-action">{runButton(loop)}</td> : null}
                      </tr>
                    ))}
                  </tbody>
                ))}
              </table>
            </div>

            {/* Narrow: one sheet per loop name, with a row for each project it runs in. */}
            <div className="loops-sheets">
              {groups.map((group) => (
                <section key={group.name} className="loop-sheet">
                  <h3 className="loop-sheet-name">
                    <LoopName name={group.name} />
                  </h3>
                  <ul className="loop-sheet-rows">
                    {group.loops.map((loop) => (
                      <li key={loop.project} className="loop-sheet-row">
                        <div className="loop-sheet-main">
                          <div className="loop-sheet-top">
                            <span className="loop-project">{projectLabel(loop.project)}</span>
                            <Status loop={loop} />
                          </div>
                          <div className="loop-sheet-line">
                            <Schedule loop={loop} />
                          </div>
                          <div className="loop-sheet-line">
                            <LastRun loop={loop} />
                          </div>
                        </div>
                        {canOperate ? runButton(loop) : null}
                      </li>
                    ))}
                  </ul>
                </section>
              ))}
            </div>
          </div>

          <ErrorLine error={runLoop.error} />
        </>
      ) : null}
    </>
  );
}
