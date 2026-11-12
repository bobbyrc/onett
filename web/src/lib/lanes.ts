// Which projects get a lane on the Overview board, and in what order. A project with nothing going
// on folds into one quiet line instead of a lane.
import type { ProjectView, Run } from '../types';
import { projectLamp, type LampState } from './lamp';
import type { Need } from './needs';

export interface Lane {
  project: ProjectView;
  state: LampState;
  needs: Need[];
  runs: Run[];
}

export interface Board {
  lanes: Lane[];
  /** Projects with nothing going on, A to Z: they fold into one line. */
  quiet: ProjectView[];
  /** Live runs whose project is not a configured project (a chat with no project). */
  loose: Run[];
}

const LAMP_RANK: Record<LampState['lamp'], number> = { stop: 0, call: 1, run: 2, off: 3 };

/** Pure: the arguments are not changed. */
export function boardLanes(projects: ProjectView[], needs: Need[], runs: Run[]): Board {
  // The position of each project's first need in the list; a project with none sorts last.
  const firstNeed = new Map<string, number>();
  needs.forEach((need, index) => {
    if (!firstNeed.has(need.project)) firstNeed.set(need.project, index);
  });
  const names = new Set(projects.map((project) => project.name));

  const lanes: Lane[] = [];
  const quiet: ProjectView[] = [];
  for (const project of projects) {
    const state = projectLamp(project);
    const own = needs.filter((need) => need.project === project.name);
    const ownRuns = runs.filter((run) => run.project === project.name);
    if (own.length === 0 && ownRuns.length === 0 && state.lamp === 'off' && project.counts.queued === 0 && !project.paused) {
      quiet.push(project);
    } else {
      lanes.push({ project, state, needs: own, runs: ownRuns });
    }
  }

  const rank = (lane: Lane) => firstNeed.get(lane.project.name) ?? Infinity;
  lanes.sort((a, b) => {
    const ra = rank(a);
    const rb = rank(b);
    if (ra !== rb) return ra < rb ? -1 : 1;
    const byLamp = LAMP_RANK[a.state.lamp] - LAMP_RANK[b.state.lamp];
    if (byLamp !== 0) return byLamp;
    return a.project.name.localeCompare(b.project.name);
  });
  quiet.sort((a, b) => a.name.localeCompare(b.name));

  return {
    lanes,
    quiet,
    loose: runs.filter((run) => !names.has(run.project)),
  };
}
