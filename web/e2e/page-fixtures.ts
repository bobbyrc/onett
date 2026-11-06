// Realistic mock data for the page screenshots (npm run shots, e2e/page-shots.spec.ts). Unlike
// fixtures.ts, which is shaped to break layouts, these are the shapes a long-running daemon really
// holds: chat runs with no task but a conversation, goal check-in conversations the daemon started
// itself, 60+ character branch names, long role names, image attachments, timestamps from minutes
// to days old, done / blocked / active goals, deploys with and without notes, snake_case loop
// names and multi-paragraph agent prompts. Types come from the UI so a field rename fails here.
import type {
  AgentView,
  Approval,
  ChatMessage,
  Conversation,
  Deployment,
  Friction,
  Goal,
  LogLine,
  LoopView,
  OnettEvent,
  ProjectView,
  Run,
  Signal,
  Stats,
  Task,
  TaskConversation,
} from '../src/types';
import { config as baseConfig, configDocument as baseConfigDocument } from './fixtures';

/** The clock the shots freeze at; every timestamp below is relative to it. */
export const FROZEN = '2026-11-05T12:00:00Z';
const T0 = Date.parse(FROZEN);
const ago = (minutes: number) => new Date(T0 - minutes * 60_000).toISOString();
const DAY = 24 * 60;

const ulid = (n: number) => `01JC${String(n).padStart(3, '0')}7Q2M8V4XK6R9T3WZ5B`.slice(0, 26);
export const IDS = {
  task: ulid(1),
  chatTask: ulid(2),
  goal: ulid(3),
  doneGoal: ulid(4),
  blockedGoal: ulid(5),
  run: ulid(6),
  chatRun: ulid(7),
  chat: ulid(8),
  goalChat: ulid(9),
  taskChat: ulid(13),
  goalRun: ulid(303),
};

const LONG_BRANCH = 'onett/01a1af59-fde4-72ab-a758-5aa2396e1288-mocked-screenshots-of-every-page-with-re';
const SHA = '9f3c1a7e5b2d4086a1c7e93bd05f6a284c7d1e0b';
const PR = 'https://github.com/bobbyrc/onett/pull';

const task = (n: number, over: Partial<Task>): Task => ({
  id: ulid(100 + n),
  project: 'onett',
  kind: 'feature',
  title: 'Task',
  body: '',
  role: 'senior-engineer',
  status: 'queued',
  priority: 2,
  risk: 'low',
  source: 'chat',
  attempts: 1,
  max_attempts: 3,
  created_at: ago(300),
  updated_at: ago(12),
  ...over,
});

export const tasks: Task[] = [
  task(0, {
    id: IDS.task,
    title: 'Show elapsed time on running tool rows from tool_progress',
    body: 'The agent emits a `tool_progress` heartbeat while a tool call runs. Show the elapsed seconds on the running row in the chat transcript.\n\n- Only for calls still running\n- Update at most once a second',
    status: 'awaiting_approval',
    risk: 'medium',
    risk_reasons: ['Touches web/src/components/Transcript.tsx (user-visible behaviour)', 'Diff is 412 lines, over the 400-line default'],
    branch: LONG_BRANCH,
    pr_number: 297,
    pr_url: `${PR}/297`,
    summary: 'Added a ToolElapsed component fed by tool_progress lines and tests for the formatting.',
    goal_id: IDS.goal,
    created_at: ago(190),
    updated_at: ago(12),
  }),
  task(1, {
    id: IDS.chatTask,
    kind: 'bugfix',
    title: 'Fix chat threads: system-started turns, viewport scroll, internal lines, ring, branch',
    status: 'running',
    branch: 'onett/01a1aa15-3aa2-7372-bfa8-d97469dc6098-fix-chat-threads-system-started-turns',
    goal_id: IDS.goal,
    updated_at: ago(3),
  }),
  task(2, {
    kind: 'chore',
    title: 'Kill leftover shims and agents before reattach test TempDir removal',
    status: 'done',
    pr_number: 295,
    pr_url: `${PR}/295`,
    merge_sha: SHA,
    approved_sha: SHA,
    updated_at: ago(3 * DAY),
  }),
  task(3, {
    kind: 'improve',
    title: 'Title every chat from its first message and backfill existing "New chat" conversations',
    status: 'review',
    pr_number: 284,
    pr_url: `${PR}/284`,
    branch: 'onett/01a1af0c-1b77-7c0e-9d5e-3f3f2b1d8a10-title-every-chat-from-its-first-message',
    updated_at: ago(45),
  }),
  task(4, {
    kind: 'feature',
    title: 'Publish agent screenshots from the outbox into the PR body',
    status: 'blocked',
    blocked_reason: 'Run was denied: Bash(git push origin HEAD) is not allowed. Onett pushes branches itself; the agent must not.',
    role: 'principal-staff-engineer-for-cross-cutting-changes',
    updated_at: ago(8 * 60),
  }),
  task(5, {
    kind: 'incident',
    title: 'Deploy of 0e8ebaa rolled back: health check on /api/v1/health timed out',
    status: 'failed',
    risk: 'high',
    risk_reasons: ['Incident from a failed deploy'],
    role: 'sre',
    source: 'prod_watch',
    updated_at: ago(26 * 60),
  }),
  task(6, { kind: 'question', title: 'Why does the shepherd retry a failed merge every tick?', status: 'merged', updated_at: ago(2 * DAY) }),
  task(7, { title: 'Cap the shepherd backoff at 10 minutes', status: 'queued', queue_position: 1, project_queue_position: 1, updated_at: ago(2) }),
  task(8, { title: 'Add a retry button to a stalled run', status: 'cancelled', project: 'dotfiles-and-agent-configs', updated_at: ago(5 * DAY) }),
];

export const runs: Run[] = [
  {
    id: IDS.run,
    task_id: IDS.task,
    project: 'onett',
    role: 'senior-engineer',
    harness: 'claude',
    model: 'claude-sonnet-5-5',
    effort: 'high',
    status: 'succeeded',
    session_id: ulid(300),
    workdir: '/Users/bcraig/.local/share/onett-dogfood/data/worktrees/onett/01a1af59-fde4-72ab-a758-5aa2396e1288',
    branch: LONG_BRANCH,
    started_at: ago(180),
    ended_at: ago(22),
    last_output_at: ago(22),
    exit_code: 0,
    cost_usd: 3.4127,
    billing: 'metered',
    input_tokens: 1_204_331,
    output_tokens: 48_902,
    cached_input_tokens: 1_020_000,
    summary: 'Added ToolElapsed and its tests; npm run lint, typecheck and test pass.',
    start_sha: SHA,
    remote_sha: SHA,
  },
  {
    id: IDS.chatRun,
    task_id: '',
    conversation_id: IDS.chat,
    project: 'onett',
    role: 'orchestrator',
    harness: 'claude',
    model: 'claude-opus-5-5',
    status: 'running',
    session_id: ulid(301),
    started_at: ago(7),
    last_output_at: ago(0.5),
    exit_code: 0,
    cost_usd: 0.8,
    billing: 'subscription',
    input_tokens: 220_100,
    output_tokens: 3_100,
  },
  {
    id: ulid(302),
    task_id: IDS.chatTask,
    project: 'onett',
    role: 'senior-engineer',
    harness: 'claude',
    model: 'claude-sonnet-5-5',
    status: 'running',
    branch: tasks[1].branch,
    started_at: ago(41),
    last_output_at: ago(3),
    exit_code: 0,
    cost_usd: 1.07,
    billing: 'metered',
    input_tokens: 610_000,
    output_tokens: 21_400,
  },
  {
    id: IDS.goalRun,
    task_id: '',
    conversation_id: IDS.goalChat,
    goal_turn: true,
    project: 'onett',
    role: 'orchestrator',
    harness: 'claude',
    model: 'claude-opus-5-5',
    status: 'succeeded',
    started_at: ago(62),
    ended_at: ago(58),
    last_output_at: ago(58),
    exit_code: 0,
    cost_usd: 0.31,
    billing: 'metered',
    input_tokens: 90_000,
    output_tokens: 2_400,
    summary: 'Checked in on the goal: two tasks queued, waiting on review.',
  },
  {
    id: ulid(304),
    task_id: tasks[5].id,
    project: 'onett',
    role: 'sre',
    harness: 'codex',
    model: 'gpt-5-codex',
    status: 'failed',
    started_at: ago(26 * 60 + 20),
    ended_at: ago(26 * 60),
    last_output_at: ago(26 * 60),
    exit_code: 1,
    cost_usd: 0.12,
    billing: 'metered',
    input_tokens: 31_000,
    output_tokens: 900,
    error: 'exit status 1: the sandbox could not bind 127.0.0.1:0 (operation not permitted)',
  },
  {
    id: ulid(305),
    task_id: tasks[2].id,
    project: 'onett',
    role: 'senior-engineer',
    harness: 'claude',
    model: 'claude-sonnet-5-5',
    status: 'timed_out',
    started_at: ago(4 * DAY),
    ended_at: ago(4 * DAY - 60),
    last_output_at: ago(4 * DAY - 55),
    exit_code: -1,
    cost_usd: 2.2,
    billing: 'metered',
    input_tokens: 800_000,
    output_tokens: 30_000,
    error: 'run timeout (1h) exceeded',
  },
];

const L = (run: string, seq: number, kind: LogLine['kind'], text: string, minutes: number, tool_id?: string): LogLine => ({
  run_id: run,
  seq,
  kind,
  text,
  tool_id,
  time: ago(minutes),
});

export const runLogs: LogLine[] = [
  L(IDS.run, 1, 'prompt', 'Show elapsed time on running tool rows from tool_progress. The agent emits a tool_progress heartbeat while a tool call runs.', 180),
  L(IDS.run, 2, 'text', 'I will start by reading the transcript component and the existing log parser.', 179),
  L(IDS.run, 3, 'tool_use', 'Grep: {"pattern":"tool_progress","path":"web/src"}', 178, 'g1'),
  L(IDS.run, 4, 'tool_result', 'web/src/lib/transcript.ts:41:  // tool_progress heartbeats\nweb/src/components/Transcript.tsx:112', 178, 'g1'),
  L(IDS.run, 5, 'tool_use', 'Read: {"file_path":"web/src/components/Transcript.tsx"}', 177, 'g2'),
  L(IDS.run, 6, 'tool_result', 'import { useMemo } from "react";\n// ... 214 lines', 177, 'g2'),
  L(IDS.run, 7, 'tool_use', 'Edit: {"file_path":"web/src/components/Transcript.tsx","old_string":"<ToolRow","new_string":"<ToolRow elapsed={elapsed}"}', 150, 'g3'),
  L(IDS.run, 8, 'tool_result', 'edited', 150, 'g3'),
  L(IDS.run, 9, 'tool_use', 'Bash: cd web && npm test', 120, 'g4'),
  L(IDS.run, 10, 'tool_result', ' Test Files  41 passed (41)\n      Tests  318 passed (318)', 118, 'g4'),
  L(IDS.run, 11, 'stderr', 'npm warn deprecated inflight@1.0.6: This module is not supported', 117),
  L(IDS.run, 12, 'tool_use', 'Bash: {"command":"cd web && npm run lint && npm run typecheck && npm test -- --run src/lib/transcript.test.ts","timeout":600000,"description":"Run web checks"}', 100, 'g5'),
  L(IDS.run, 13, 'system', '{"type":"tool_progress","tool_use_id":"g5-heartbeat-1","tool_name":"Bash","elapsed_time_seconds":30,"heartbeat":true}', 99),
  L(IDS.run, 14, 'tool_result', 'ok', 98, 'g5'),
  L(IDS.run, 15, 'text', 'Added `ToolElapsed`, fed from the latest `tool_progress` line for the call.\n\n- Shows seconds under a minute, then `1m 04s`\n- Hidden once the call has a result\n\nAll web checks pass.', 25),
];

export const chatRunLogs: LogLine[] = [
  L(IDS.chatRun, 1, 'text', 'Looking at the open pull requests for the screenshot work.', 6),
  L(IDS.chatRun, 2, 'tool_use', 'Bash: gh pr list --state open --json number,title', 5, 'c1'),
  L(IDS.chatRun, 3, 'tool_result', '[{"number":297,"title":"Show elapsed time on running tool rows"}]', 5, 'c1'),
  L(IDS.chatRun, 4, 'text', 'One open PR touches the transcript: #297.', 1),
];

const GOAL_RUN = IDS.goalRun;
export const goalRunLogs: LogLine[] = [
  L(GOAL_RUN, 1, 'tool_use', 'Bash: onett task list --goal 01JC0037Q2M8V4XK6R9T3WZ5B', 61, 'q1'),
  L(GOAL_RUN, 2, 'tool_result', '3 done, 1 review, 2 queued', 61, 'q1'),
  L(GOAL_RUN, 3, 'text', 'Two tasks are still queued and one is waiting on review, so I am leaving the goal as it is until #297 merges.', 58),
];

const conv = (over: Partial<Conversation>): Conversation => ({
  id: IDS.chat,
  title: 'Mocked screenshots of every page',
  project: 'onett',
  busy: false,
  created_at: ago(120),
  updated_at: ago(7),
  ...over,
});

export const conversations: Conversation[] = [
  conv({ busy: true, active_run_id: IDS.chatRun }),
  conv({
    id: IDS.goalChat,
    title: 'Goal: Make the web UI feel finished on phones, tablets and wide desktop screens',
    updated_at: ago(58),
    created_at: ago(2 * DAY),
  }),
  conv({ id: ulid(10), title: 'Operations', kind: 'operations', updated_at: ago(15), created_at: ago(20 * DAY) }),
  conv({ id: ulid(11), title: 'Why did the last deploy roll back?', project: 'dotfiles-and-agent-configs', updated_at: ago(3 * DAY) }),
  conv({ id: ulid(12), title: 'New chat', project: undefined, updated_at: ago(5 * DAY) }),
  conv({ id: IDS.taskChat, title: tasks[1].title, task_id: IDS.chatTask, updated_at: ago(3) }),
];

const m = (n: number, role: ChatMessage['role'], text: string, minutes: number, over: Partial<ChatMessage> = {}): ChatMessage => ({
  id: ulid(400 + n),
  conversation_id: IDS.chat,
  role,
  text,
  created_at: ago(minutes),
  ...over,
});

export const chatMessages: ChatMessage[] = [
  m(1, 'user', 'Worker agents can only screenshot the Chat page. Can you add mocked screenshots of every page? Here is how the Tasks page looks right now:', 30, {
    attachments: [{ id: ulid(500), conversation_id: IDS.chat, name: 'tasks-page-today.png', mime: 'image/png', size: 183_204, created_at: ago(30) }],
  }),
  m(2, 'assistant', 'I will open a task for it.\n\nOne question first: should the shots use the signed-in owner, so Agents and Config show data?', 28),
  m(3, 'user', 'Yes, owner. Phone and desktop, light and dark.', 26),
  m(4, 'assistant', 'Created the task. It will show up under **Tasks** as `Mocked screenshots of every page with realistic fixtures`.', 24, { run_id: IDS.run }),
  m(6, 'user', 'Also attach the phone shots.', 9, { queued: true }),
];

/** A goal check-in conversation: the daemon starts every turn, so the first user line is internal. */
export const goalChatMessages: ChatMessage[] = [
  {
    id: ulid(450),
    conversation_id: IDS.goalChat,
    role: 'user',
    kind: 'internal',
    text: 'Goal check-in: 3 of 5 tasks are done. Decide the next step.',
    created_at: ago(62),
  },
  {
    id: ulid(451),
    conversation_id: IDS.goalChat,
    role: 'assistant',
    run_id: ulid(303),
    text: 'Two tasks are still queued and one is waiting on review, so I am leaving the goal as it is until #297 merges.',
    created_at: ago(58),
  },
];

export const taskConversation: TaskConversation = {
  task: tasks[1],
  conversation: conv({ id: IDS.taskChat, title: tasks[1].title, task_id: IDS.chatTask, updated_at: ago(3) }),
  tasks: [tasks[1]],
  runs: [runs[2]],
  messages: [{ ...m(20, 'user', 'Fix the five chat-thread defects listed in the goal.', 41), conversation_id: IDS.taskChat }],
  can_send: true,
};

export const goals: Goal[] = [
  {
    id: IDS.goal,
    project: 'onett',
    title: 'Make the web UI feel finished on phones, tablets and wide desktop screens',
    body: 'Every page must read well at 390px and 1440px, in light and dark.\n\n- No clipped text\n- No horizontal scrolling\n- Empty states say what to do next',
    status: 'active',
    conversation_id: IDS.goalChat,
    turns: 6,
    stalled_turns: 0,
    last_turn_at: ago(58),
    summary: 'Chat and task pages are done; Agents, Config and Deploys are still being checked.',
    created_at: ago(2 * DAY),
    updated_at: ago(58),
  },
  {
    id: IDS.doneGoal,
    project: 'onett',
    title: 'Let agents hand screenshots to the PR through the outbox',
    body: 'Agents hold no GitHub access, so onett should publish their PNGs.',
    status: 'done',
    conversation_id: ulid(14),
    turns: 9,
    summary: 'Onett now pushes $ONETT_OUTBOX/shots/<label> to a pr-assets branch and links the images in the PR body. Shipped in #293.',
    created_at: ago(9 * DAY),
    updated_at: ago(4 * DAY),
  },
  {
    id: IDS.blockedGoal,
    project: 'dotfiles-and-agent-configs',
    title: 'Move the shell setup to chezmoi templates',
    body: 'Replace the hand-copied dotfiles.',
    status: 'blocked',
    conversation_id: ulid(15),
    turns: 12,
    stalled_turns: 3,
    last_turn_at: ago(18 * 60),
    summary: 'Waiting on the owner: the 1Password CLI is not installed on this machine, so secrets cannot be templated.',
    created_at: ago(14 * DAY),
    updated_at: ago(18 * 60),
  },
];

export const approvals: Approval[] = [
  {
    id: ulid(600),
    task_id: IDS.task,
    project: 'onett',
    kind: 'merge',
    title: tasks[0].title,
    reason: 'Medium risk: touches user-visible behaviour in web/src/components/Transcript.tsx. Reviewer approved at 9f3c1a7.',
    status: 'pending',
    head_sha: SHA,
    created_at: ago(12),
  },
  {
    id: ulid(601),
    task_id: tasks[3].id,
    project: 'onett',
    kind: 'merge',
    title: tasks[3].title,
    reason: 'High risk: matches the built-in rule for internal/engine/**.',
    status: 'approved',
    head_sha: SHA,
    note: 'Looks right, go ahead.',
    created_at: ago(2 * DAY),
    decided_at: ago(2 * DAY - 20),
  },
  {
    id: ulid(602),
    task_id: tasks[4].id,
    project: 'onett',
    kind: 'merge',
    title: tasks[4].title,
    reason: 'Changes AGENTS.md (medium risk by built-in rule).',
    status: 'rejected',
    head_sha: SHA,
    note: 'The agent should not push; onett does. Rework.',
    created_at: ago(5 * DAY),
    decided_at: ago(5 * DAY - 90),
  },
];

const dep = (n: number, over: Partial<Deployment>): Deployment => ({
  id: ulid(700 + n),
  project: 'onett',
  ref: SHA,
  provider_id: 'local',
  state: 'healthy',
  started_at: ago(60),
  updated_at: ago(55),
  ...over,
});

export const deployments: Deployment[] = [
  dep(0, { task_id: IDS.task, url: 'http://127.0.0.1:7788', state: 'verifying', verifying_since: ago(4), started_at: ago(6), updated_at: ago(4), notes: 'Staging release on port 7788; health check passes, watching for 10 minutes.' }),
  dep(1, { task_id: tasks[2].id, started_at: ago(3 * DAY), updated_at: ago(3 * DAY - 12), url: 'http://127.0.0.1:7777' }),
  dep(2, { task_id: tasks[5].id, state: 'rolled_back', started_at: ago(26 * 60 + 10), updated_at: ago(26 * 60), notes: 'Health check on /api/v1/health timed out after 60s; the previous binary was restored.', log: 'building 0e8ebaa...\nswapping binary\nhealth: GET /api/v1/health -> timeout\nrolling back to 5c1d02e\nhealth: ok' }),
  dep(3, { state: 'failed', provider_id: 'launchd-user-agent-for-the-staging-copy', started_at: ago(6 * DAY), updated_at: ago(6 * DAY - 3) }),
  dep(4, { state: 'deploying', started_at: ago(1), updated_at: ago(0.5), provider_id: 'render' }),
  // A second project's deploy, so the Overview board shows a last deploy beyond onett's.
  dep(5, { project: 'retro-diffusion-sprite-pipeline', ref: '4be81d0c2a9f6e3b7d15c8a0f42e9b6d3c71a5e8', provider_id: 'render', started_at: ago(2 * DAY), updated_at: ago(2 * DAY - 6) }),
];

export const signals: Signal[] = [
  {
    id: ulid(800),
    project: 'onett',
    source: 'sentry',
    fingerprint: 'a1b2c3d4e5f6a7b8',
    title: 'panic: send on closed channel in (*Bus).publish',
    level: 'fatal',
    count: 3,
    url: 'https://sentry.io/organizations/bobbyrc/issues/4821337/',
    culprit: 'internal/engine/bus.go in (*Bus).publish',
    release: '0e8ebaa',
    first_seen: ago(26 * 60),
    last_seen: ago(25 * 60),
    task_id: tasks[5].id,
    created_at: ago(26 * 60),
    updated_at: ago(25 * 60),
  },
  {
    id: ulid(801),
    project: 'onett',
    source: 'sentry',
    fingerprint: 'b8a7f6e5d4c3b2a1',
    title: 'TypeError: Cannot read properties of undefined (reading "attachments")',
    level: 'error',
    count: 128,
    url: 'https://sentry.io/organizations/bobbyrc/issues/4821400/',
    culprit: 'web/src/pages/Chat.tsx in renderMessage',
    release: 'a408b31',
    first_seen: ago(3 * DAY),
    last_seen: ago(40),
    created_at: ago(3 * DAY),
    updated_at: ago(40),
  },
  {
    id: ulid(802),
    project: 'dotfiles-and-agent-configs',
    source: 'logs',
    fingerprint: 'c0ffee00c0ffee00',
    title: 'slow query: SELECT * FROM events ORDER BY id DESC (2.4s)',
    level: 'warning',
    count: 9,
    first_seen: ago(7 * DAY),
    last_seen: ago(2 * 60),
    created_at: ago(7 * DAY),
    updated_at: ago(2 * 60),
  },
];

export const friction: Friction[] = [
  {
    id: ulid(900),
    project: 'onett',
    run_id: IDS.run,
    title: 'docs/ui.md does not say shoot-live needs a token the worker agents never get',
    body: 'I tried `node scripts/shoot-live.mjs` as instructed and it exits with "set ONETT_API and ONETT_TOKEN". Agents hold neither.',
    fingerprint: 'f1',
    count: 4,
    issue_url: 'https://github.com/bobbyrc/onett/issues/301',
    issue_state: 'open',
    created_at: ago(2 * DAY),
    updated_at: ago(60),
  },
  {
    id: ulid(901),
    project: 'onett',
    run_id: runs[4].id,
    title: 'Codex sandbox cannot bind localhost, so httptest tests fail',
    body: 'The api and engine tests need loopback.',
    fingerprint: 'f2',
    count: 11,
    issue_url: 'https://github.com/bobbyrc/onett/issues/288',
    issue_state: 'closed',
    created_at: ago(8 * DAY),
    updated_at: ago(5 * DAY),
  },
  {
    id: ulid(902),
    project: 'onett',
    run_id: runs[0].id,
    title: 'npm ci prints a deprecation warning for inflight on every run',
    body: '',
    fingerprint: 'f3',
    count: 1,
    created_at: ago(30),
    updated_at: ago(30),
  },
];

const loop = (name: string, over: Partial<LoopView> = {}): LoopView => ({
  name,
  project: 'onett',
  enabled: true,
  every: '1m',
  last_run_at: ago(0.7),
  ...over,
});

export const loops: LoopView[] = [
  loop('issue_intake', { every: '5m', last_run_at: ago(2) }),
  loop('goal_check_ins', { last_run_at: ago(1) }),
  loop('pr_shepherd'),
  loop('deploy_watch', { last_run_at: ago(0.2) }),
  loop('prod_watch', { enabled: false, every: '5m', last_run_at: ago(3 * DAY) }),
  loop('friction_triage', { every: '1h', last_run_at: ago(41) }),
  loop('retro', { every: '24h', last_run_at: '0001-01-01T00:00:00Z' }),
  loop('self_update', { enabled: false, every: '1h', last_run_at: ago(9 * DAY), last_error: 'new release held back: 2 tasks still running' }),
  loop('heartbeat', { every: '15m', last_run_at: ago(9), next_run_at: new Date(T0 + 6 * 60_000).toISOString(), item_count: 3, actions_24h: 7, escalations_24h: 1, operations_conversation_id: ulid(10) }),
  loop('issue_intake', { project: 'dotfiles-and-agent-configs', every: '5m', last_run_at: ago(2), last_error: 'gh: HTTP 403: API rate limit exceeded for user ID 1234567 (resets in 41m)' }),
];

// Hard-wrapped at ~100 columns like the markdown sources, with a list, a code block and a
// multi-line list item, so the Agents page shows how it reflows them.
const PROMPT = (title: string) =>
  `# ${title}

You turn a task into a working, tested change on your branch. Read the task, then the code it
touches, before changing anything. If the task links an issue, the issue text is in the task
body.

1. Read the task, then the code it touches, before changing anything. If the task links an
   issue, the issue text is in the task body.
2. If this is a re-run, the task body ends with what went wrong last time (CI failure log,
   reviewer findings, merge conflict, or a human's note). Address that first.
3. Make the smallest change that fully solves the task. Fix root causes, not symptoms. No
   drive-by refactors; note them as followups instead.
4. Add or update tests that would have caught the bug or that pin the new behaviour.

## Your report (required)

Before you finish, write JSON to \`$ONETT_OUTBOX/result.json\`:

\`\`\`json
{ "status": "completed", "summary": "...", "risk": "low" }
\`\`\`

Do not commit, push, open pull requests or call the GitHub API. Onett does those.`;

const agent = (name: string, over: Partial<AgentView>): AgentView => ({
  name,
  origin: 'builtin',
  disabled: false,
  harness: 'claude',
  harness_type: 'claude',
  harness_cli_found: true,
  model: 'claude-sonnet-5-5',
  effort: 'high',
  prompt: `prompts/${name}.md`,
  prompt_text: PROMPT(name),
  prompt_updates: false,
  description: 'Turns a task into a tested change.',
  args: [],
  kinds: [],
  uses: [],
  ...over,
});

export const agents: AgentView[] = [
  agent('senior-engineer', {
    title: 'Senior engineer',
    origin: 'edited',
    prompt_updates: true,
    context: ['AGENTS.md', 'docs/design.md', 'tasks/lessons.md'],
    tools: ['Bash(go test:*)', 'Bash(npm run:*)', 'Edit', 'Read'],
    kinds: ['feature', 'bugfix', 'chore', 'improve'],
    uses: [
      { kind: 'feature', description: 'Default for features and bug fixes' },
      { project: 'onett', description: 'Used for every onett task' },
    ],
  }),
  agent('reviewer', { title: 'Reviewer', model: 'claude-opus-5-5', description: 'Reads the diff and the task, then approves or asks for changes.', kinds: ['review'] }),
  agent('principal-staff-engineer-for-cross-cutting-changes', {
    title: 'Principal staff engineer for cross-cutting changes',
    origin: 'custom',
    model: 'claude-opus-5-5',
    description: 'Custom agent for changes that span the daemon, the API and the web UI at once.',
    prompt_text: PROMPT('Principal staff engineer') + '\n\n' + PROMPT('Cross-cutting checklist'),
    args: ['--max-turns', '80'],
  }),
  agent('sre', { title: 'SRE', harness: 'codex', harness_type: 'codex', harness_cli_found: false, model: 'gpt-5-codex', description: 'Investigates incidents. The codex CLI is not on onett’s PATH.', kinds: ['incident'] }),
  agent('retired-doc-writer', { origin: 'custom', disabled: true, description: 'Disabled.' }),
];

const proj = (name: string, over: Partial<ProjectView> = {}): ProjectView => ({
  name,
  repo: `bobbyrc/${name}`,
  path: `/Users/bcraig/code/${name}`,
  default_branch: 'main',
  policy: 'default',
  deploy: null,
  observe: [],
  paused: false,
  paused_by: '',
  counts: { queued: 0, running: 0, review: 0, awaiting_approval: 0, blocked: 0 },
  ...over,
});

export const oneProject: ProjectView[] = [
  proj('onett', {
    instructions: 'Read AGENTS.md first. Run go vet and go test -race before reporting success.',
    deploy: { type: 'local', health_url: 'http://127.0.0.1:7788/api/v1/health', verify_for: '10m' },
    observe: [{ name: 'sentry', type: 'sentry' }],
    counts: { queued: 1, running: 2, review: 1, awaiting_approval: 1, blocked: 1 },
  }),
];

export const projects: ProjectView[] = [
  ...oneProject,
  proj('dotfiles-and-agent-configs', { policy: 'strict', counts: { queued: 0, running: 0, review: 0, awaiting_approval: 0, blocked: 1 } }),
  proj('retro-diffusion-sprite-pipeline', {
    paused: true,
    paused_by: 'owner',
    deploy: { type: 'render', health_url: 'https://sprites.example.com/healthz', verify_for: '5m' },
    counts: { queued: 3, running: 0, review: 0, awaiting_approval: 0, blocked: 0 },
  }),
  proj('home-lab', { counts: { queued: 0, running: 1, review: 0, awaiting_approval: 0, blocked: 0 } }),
];

export const stats: Stats = {
  tasks_by_status: { queued: 4, running: 2, review: 1, awaiting_approval: 1, blocked: 1, failed: 1, done: 83, merged: 1 },
  active_runs: 2,
  approvals_pending: 1,
  cost_today_usd: 18.42,
  subscription_cost_today_usd: 6.1,
  paused: false,
  pause: null,
  preflight: [],
  holds: [],
  owner_unreachable: false,
  blocked_goals: [{ id: IDS.blockedGoal, project: 'dotfiles-and-agent-configs', title: goals[2].title, reason: 'The 1Password CLI is not installed' }],
  awaiting_owner: 2,
};

const ev = (id: number, minutes: number, type: string, message: string, over: Partial<OnettEvent> = {}): OnettEvent => ({
  id,
  time: ago(minutes),
  type,
  message,
  project: 'onett',
  ...over,
});

export const events: OnettEvent[] = [
  ev(12, 0.5, 'run.started', 'Started a senior-engineer run', { run_id: runs[2].id }),
  ev(11, 4, 'deploy.verifying', 'Deploy of 9f3c1a7 is healthy, verifying for 10m', { task_id: IDS.task }),
  ev(10, 12, 'approval.created', 'Merge approval needed: Show elapsed time on running tool rows', { task_id: IDS.task }),
  ev(9, 41, 'task.review', 'PR #284 opened', { task_id: tasks[3].id }),
  ev(8, 58, 'goal.turn', 'Goal check-in finished: Make the web UI feel finished'),
  ev(7, 3 * 60, 'task.blocked', 'Run denied: Bash(git push origin HEAD)', { task_id: tasks[4].id }),
  ev(6, 26 * 60, 'deploy.rolled_back', 'Rolled back 0e8ebaa: health check timed out', { task_id: tasks[5].id }),
];

export const users = [
  { username: 'bcraig', role: 'owner', created_at: ago(90 * DAY), updated_at: ago(DAY) },
  { username: 'orchestrator-bot', role: 'operator', created_at: ago(30 * DAY), updated_at: ago(30 * DAY) },
  { username: 'viewer', role: 'viewer', created_at: ago(2 * DAY), updated_at: ago(2 * DAY) },
] as const;

export const secrets = [
  { name: 'GH_TOKEN', set: true, source: 'file', referenced_by: ['projects.onett.repo'] },
  { name: 'SENTRY_AUTH_TOKEN', set: true, source: 'env', referenced_by: ['projects.onett.observe[0]'] },
  { name: 'SLACK_BOT_TOKEN', set: false, source: '', referenced_by: ['channels[0].options.bot_token_env'] },
];

export const owner = { username: 'bcraig', role: 'owner', created_at: ago(90 * DAY), updated_at: ago(DAY) } as const;

// The config page reads these. They start from the shared fixtures and swap the layout-stress
// paths and help text for ones a real install has.
const CONFIG_PATH = '/Users/bcraig/.local/share/onett-dogfood/onett.yaml';
const DATA_DIR = '/Users/bcraig/.local/share/onett-dogfood/data';
export const config = {
  ...baseConfig,
  path: CONFIG_PATH,
  server: { addr: '127.0.0.1:7777', data_dir: DATA_DIR, public_url: '' },
  agents: { 'senior-engineer': { harness: 'claude', prompt: 'prompts/senior-engineer.md', description: 'Turns a task into a tested change.' } },
  routing: { feature: 'senior-engineer', incident: 'sre' },
  projects: { onett: { name: 'onett', repo: 'bobbyrc/onett', path: '/Users/bcraig/code/onett', default_branch: 'main', policy: 'default', paused: false } },
} as typeof baseConfig;
export const configDocument = {
  ...baseConfigDocument,
  path: CONFIG_PATH,
  document: {
    server: { addr: '127.0.0.1:7777', data_dir: DATA_DIR },
    limits: { max_concurrent_runs: 2, stall_timeout: '15m' },
    projects: { onett: { repo: 'bobbyrc/onett', path: '/Users/bcraig/code/onett', default_branch: 'main' } },
  },
};
export const configText = {
  path: CONFIG_PATH,
  version: 'v1',
  text: `server:\n  addr: 127.0.0.1:7777\n  data_dir: ${DATA_DIR}\nprojects:\n  onett:\n    repo: bobbyrc/onett\n    path: /Users/bcraig/code/onett\n`,
};
