import { describe, expect, it } from 'vitest';
import type { Approval, Problem, ProjectView, Run } from '../types';
import { buildNeeds } from './needs';
import { boardLanes } from './lanes';

const proj = (name: string, over: Partial<ProjectView> = {}, counts: Partial<ProjectView['counts']> = {}): ProjectView => ({
  name,
  repo: '',
  path: '',
  default_branch: 'main',
  policy: 'default',
  deploy: null,
  observe: [],
  paused: false,
  paused_by: '',
  counts: { queued: 0, running: 0, review: 0, awaiting_approval: 0, blocked: 0, ...counts },
  ...over,
});

const run = (id: string, project: string): Run =>
  ({ id, project, task_id: '', role: 'engineer', status: 'running' }) as unknown as Run;

const approval = (id: string, project: string, created: string): Approval => ({
  id,
  task_id: 't-' + id,
  project,
  kind: 'merge',
  title: 'Merge it',
  reason: '',
  status: 'pending',
  created_at: created,
});

describe('boardLanes', () => {
  it('puts the project with the most urgent need first, even when another is red', () => {
    const red = proj('alpha', {}, { blocked: 1, awaiting_approval: 1 });
    const running = proj('zeta', {}, { running: 1 });
    const problem: Problem = { kind: 'blocked_tasks', project: 'zeta', key: 'p1', title: 'Stuck', detail: '', count: 2, since: '2026-11-05T09:00:00Z' };
    const needs = buildNeeds([problem], [approval('a', 'alpha', '2026-11-05T10:00:00Z')]);
    const board = boardLanes([red, running], needs, []);
    expect(board.lanes.map((l) => l.project.name)).toEqual(['zeta', 'alpha']);
  });

  it('puts a green project with no need after projects with needs and before a paused off project', () => {
    const green = proj('green', {}, { running: 1 });
    const paused = proj('paused', { paused: true });
    const needy = proj('needy', {}, { awaiting_approval: 1 });
    const needs = buildNeeds([], [approval('a', 'needy', '2026-11-05T10:00:00Z')]);
    const board = boardLanes([paused, green, needy], needs, [run('r1', 'green')]);
    expect(board.lanes.map((l) => l.project.name)).toEqual(['needy', 'green', 'paused']);
  });

  it('folds an idle project into quiet', () => {
    const idle = proj('idle');
    const board = boardLanes([idle], [], []);
    expect(board.lanes).toEqual([]);
    expect(board.quiet.map((p) => p.name)).toEqual(['idle']);
  });

  it('gives a project with only queued tasks a lane, not a quiet line', () => {
    const queued = proj('queued', {}, { queued: 2 });
    const board = boardLanes([queued], [], []);
    expect(board.quiet).toEqual([]);
    expect(board.lanes.map((l) => l.project.name)).toEqual(['queued']);
  });

  it('gives a paused idle project a lane', () => {
    const paused = proj('paused', { paused: true });
    const board = boardLanes([paused], [], []);
    expect(board.quiet).toEqual([]);
    expect(board.lanes.map((l) => l.project.name)).toEqual(['paused']);
  });

  it('keeps a run with no project as a loose run, and files the others under their lane', () => {
    const onett = proj('onett', {}, { running: 1 });
    const loose = run('chat', '');
    const mine = run('mine', 'onett');
    const board = boardLanes([onett], [], [loose, mine]);
    expect(board.loose.map((r) => r.id)).toEqual(['chat']);
    expect(board.lanes[0].runs.map((r) => r.id)).toEqual(['mine']);
  });

  it('does not change its arguments', () => {
    const projects = [proj('b', {}, { running: 1 }), proj('a', {}, { running: 1 })];
    const runs = [run('r', 'a')];
    const snapshot = JSON.stringify([projects, runs]);
    boardLanes(projects, [], runs);
    expect(JSON.stringify([projects, runs])).toBe(snapshot);
    expect(projects.map((p) => p.name)).toEqual(['b', 'a']);
  });
});
