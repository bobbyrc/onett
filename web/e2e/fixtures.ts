// Mock backend data for the mobile layout check. Everything is shaped to stress layout: very long
// titles (one with no spaces), full-length IDs and SHAs, long branch names and paths, and long
// unbroken log lines. Types come from the UI so a field rename fails the typecheck here too.
import type {
  AgentConversation,
  AgentView,
  Approval,
  AuthStateResponse,
  ChatMessage,
  Config,
  ConfigDocument,
  Conversation,
  Deployment,
  Friction,
  Goal,
  LogLine,
  LoopView,
  ProjectView,
  Run,
  SecretView,
  Signal,
  Stats,
  Task,
  TaskConversation,
  User,
} from '../src/types';

const NOW = '2026-11-05T12:00:00Z';
const EARLIER = '2026-11-05T11:00:00Z';

// 26-char ULID-style IDs.
const id = (n: number) => `01HZX${String(n).padStart(2, '0')}K8M3Q7R2T5V9W4Y6ABCD`.slice(0, 26);
export const TASK_ID = id(1);
export const GOAL_ID = id(2);
export const RUN_ID = id(3);
export const CONV_ID = id(4);
export const CHAT_TASK_ID = id(5);

const LONG_TITLE =
  'Make the dashboard handle absurdly long task and goal titles without clipping, wrapping badly, or pushing the page wider than the phone screen so that nobody has to pan sideways to read the rest of the heading text on a small device';
const NO_SPACE_TITLE = 'Supercalifragilistic'.repeat(12) + '-unbroken-title-with-no-spaces-at-all';
const BRANCH = 'onett/01a1aa15-3aa2-7372-bfa8-d97469dc6098-mobile-web-ui-nothing-clipped';
const SHA = '0123456789abcdef0123456789abcdef01234567';
const PATH = 'internal/engine/prompts/some/very/deeply/nested/directory/structure/with_a_long_file_name_that_keeps_going.go';
const UNBROKEN = 'x'.repeat(260);

export const user: User = { username: 'owner-with-a-fairly-long-username', role: 'owner', created_at: EARLIER, updated_at: NOW };
export const authState: AuthStateResponse = { setup_required: false, user };
export const users: User[] = [
  user,
  { username: 'operator', role: 'operator', created_at: EARLIER, updated_at: NOW },
  { username: 'viewer-' + 'v'.repeat(60), role: 'viewer', created_at: EARLIER, updated_at: NOW },
];

function task(n: number, over: Partial<Task> = {}): Task {
  return {
    id: id(10 + n),
    project: n % 2 ? 'onett' : 'a-project-with-a-long-name-for-layout-testing',
    kind: 'feature',
    title: LONG_TITLE,
    body: `${LONG_TITLE}\n\n${UNBROKEN}\n\n- ${PATH}`,
    role: 'implementer',
    status: 'running',
    priority: 2,
    risk: 'medium',
    risk_reasons: [`Touches ${PATH}`, 'Changes user-visible behaviour in a way that needs review'],
    source: 'chat',
    source_ref: id(4),
    branch: BRANCH,
    pr_number: 1234,
    pr_url: `https://github.com/example/${'repo-'.repeat(8)}/pull/1234`,
    merge_sha: SHA,
    approved_sha: SHA,
    summary: LONG_TITLE,
    attempts: 1,
    max_attempts: 3,
    created_at: EARLIER,
    updated_at: NOW,
    goal_id: GOAL_ID,
    ...over,
  };
}

export const tasks: Task[] = [
  task(0, { id: TASK_ID, status: 'awaiting_approval' }),
  task(1, { title: NO_SPACE_TITLE, status: 'blocked', blocked_reason: UNBROKEN + ' ' + LONG_TITLE }),
  task(2, { status: 'queued', kind: 'bugfix' }),
  task(3, { status: 'review', waiting_on: [id(11), id(12)] }),
  task(4, { status: 'done', kind: 'chore' }),
  task(5, { status: 'failed', kind: 'incident' }),
];

export const runs: Run[] = [0, 1, 2, 3].map((n) => ({
  id: n === 0 ? RUN_ID : id(30 + n),
  task_id: tasks[n].id,
  project: tasks[n].project,
  role: 'implementer',
  harness: 'claude-code-with-a-long-name',
  model: 'claude-sonnet-5-5',
  status: (['running', 'succeeded', 'failed', 'stalled'] as const)[n],
  session_id: id(40 + n),
  workdir: `/Users/someone/.local/share/onett/data/worktrees/${'project/'.repeat(4)}${tasks[n].id}`,
  branch: BRANCH,
  started_at: EARLIER,
  ended_at: n === 0 ? undefined : NOW,
  last_output_at: NOW,
  exit_code: n === 2 ? 1 : 0,
  cost_usd: 1.2345,
  billing: 'metered',
  input_tokens: 123456,
  output_tokens: 7890,
  summary: LONG_TITLE,
  error: n === 2 ? UNBROKEN + ' failed ' + PATH : undefined,
}));

export const logs: LogLine[] = [
  { run_id: RUN_ID, seq: 1, time: EARLIER, kind: 'prompt', text: LONG_TITLE },
  { run_id: RUN_ID, seq: 2, time: EARLIER, kind: 'text', text: `Reading ${PATH} then ${UNBROKEN}` },
  { run_id: RUN_ID, seq: 3, time: EARLIER, kind: 'tool_use', text: `Bash: cd ${PATH} && ${UNBROKEN}`, tool_id: 't1' },
  { run_id: RUN_ID, seq: 4, time: EARLIER, kind: 'tool_result', text: UNBROKEN + '\n' + UNBROKEN, tool_id: 't1' },
  { run_id: RUN_ID, seq: 5, time: NOW, kind: 'stderr', text: UNBROKEN },
  { run_id: RUN_ID, seq: 6, time: NOW, kind: 'denial', text: `denied: ${UNBROKEN}` },
  { run_id: RUN_ID, seq: 7, time: NOW, kind: 'system', text: SHA },
  { run_id: RUN_ID, seq: 8, time: NOW, kind: 'error', text: `error: ${PATH}: ${UNBROKEN}` },
];

export const goals: Goal[] = [
  {
    id: GOAL_ID,
    project: 'onett',
    title: LONG_TITLE,
    body: `${LONG_TITLE}\n\n${UNBROKEN}`,
    status: 'active',
    conversation_id: CONV_ID,
    turns: 4,
    stalled_turns: 1,
    last_turn_at: NOW,
    summary: LONG_TITLE,
    created_at: EARLIER,
    updated_at: NOW,
  },
  {
    id: id(21),
    project: 'a-project-with-a-long-name-for-layout-testing',
    title: NO_SPACE_TITLE,
    body: UNBROKEN,
    status: 'blocked',
    conversation_id: id(22),
    turns: 9,
    created_at: EARLIER,
    updated_at: NOW,
  },
  { id: id(23), project: 'onett', title: 'Short goal', body: 'x', status: 'done', conversation_id: id(24), turns: 1, created_at: EARLIER, updated_at: NOW },
];

export const approvals: Approval[] = [0, 1, 2].map((n) => ({
  id: id(50 + n),
  task_id: tasks[n].id,
  project: tasks[n].project,
  kind: 'merge',
  title: n === 1 ? NO_SPACE_TITLE : LONG_TITLE,
  reason: `High risk: ${PATH} ${UNBROKEN}`,
  status: n === 2 ? 'approved' : 'pending',
  head_sha: SHA,
  note: n === 2 ? LONG_TITLE : undefined,
  created_at: EARLIER,
  decided_at: n === 2 ? NOW : undefined,
}));

export const deployments: Deployment[] = (['healthy', 'failed', 'verifying', 'rolled_back'] as const).map((state, n) => ({
  id: id(60 + n),
  project: 'onett',
  task_id: tasks[n].id,
  ref: SHA,
  provider_id: `provider-${'long-'.repeat(10)}${n}`,
  url: `https://staging.example.com/${'very-long-path/'.repeat(6)}`,
  state,
  notes: LONG_TITLE,
  log: `${UNBROKEN}\n${UNBROKEN}`,
  started_at: EARLIER,
  updated_at: NOW,
}));

export const signals: Signal[] = [0, 1, 2].map((n) => ({
  id: id(70 + n),
  project: 'onett',
  source: 'sentry-' + 'x'.repeat(20),
  fingerprint: SHA + SHA,
  title: n === 1 ? NO_SPACE_TITLE : LONG_TITLE,
  level: ['error', 'warning', 'fatal'][n],
  count: 12345,
  url: `https://sentry.example.com/${'issues/'.repeat(10)}${n}`,
  culprit: `${PATH} in handler`,
  release: SHA,
  first_seen: EARLIER,
  last_seen: NOW,
  task_id: n === 0 ? tasks[0].id : undefined,
  created_at: EARLIER,
  updated_at: NOW,
}));

export const friction: Friction[] = [0, 1, 2].map((n) => ({
  id: id(80 + n),
  project: 'onett',
  run_id: runs[n].id,
  title: n === 1 ? NO_SPACE_TITLE : LONG_TITLE,
  body: `${LONG_TITLE}\n\n${UNBROKEN}`,
  fingerprint: SHA + SHA,
  count: 7,
  issue_url: `https://github.com/example/repo/issues/${n}`,
  issue_state: n === 2 ? 'closed' : 'open',
  created_at: EARLIER,
  updated_at: NOW,
}));

export const loops: LoopView[] = ['issue_intake', 'goals', 'pr_shepherd', 'deploy_watch', 'prod_watch', 'friction', 'retro', 'self_update'].map(
  (name, n) => ({
    name,
    project: n % 2 ? 'a-project-with-a-long-name-for-layout-testing' : 'onett',
    enabled: n % 3 !== 0,
    every: '5m',
    last_run_at: NOW,
    last_error: n === 1 ? UNBROKEN + ' ' + LONG_TITLE : undefined,
  }),
);

export const agents: AgentView[] = ['implementer', 'reviewer', 'orchestrator-with-a-long-custom-agent-name'].map((name, n) => ({
  name,
  origin: (['builtin', 'edited', 'custom'] as const)[n],
  disabled: false,
  harness: 'claude',
  harness_type: 'claude',
  harness_cli_found: true,
  model: 'claude-sonnet-5-5',
  prompt: `prompts/${name}.md`,
  prompt_text: `# ${name}\n\n${LONG_TITLE}\n\n${UNBROKEN}\n\n\`\`\`\n${UNBROKEN}\n\`\`\``,
  prompt_updates: false,
  description: LONG_TITLE,
  args: ['--flag', UNBROKEN.slice(0, 80)],
  kinds: ['feature', 'bugfix'],
  uses: [{ kind: 'feature', project: 'onett', description: LONG_TITLE }],
}));

export const projects: ProjectView[] = [
  {
    name: 'onett',
    repo: 'bobbyrc/onett',
    path: `/Users/someone/${'long-directory/'.repeat(6)}onett`,
    default_branch: 'main',
    policy: 'default',
    instructions: LONG_TITLE,
    deploy: { type: 'local', health_url: `http://127.0.0.1:7777/${'health/'.repeat(10)}`, verify_for: '2m' },
    observe: [{ name: 'sentry', type: 'sentry' }],
    paused: false,
    paused_by: '',
    counts: { queued: 2, running: 1, review: 1, awaiting_approval: 1, blocked: 1 },
  },
  {
    name: 'a-project-with-a-long-name-for-layout-testing',
    repo: 'some-organisation-with-a-long-name/' + 'repository-'.repeat(6),
    path: '/tmp/x',
    default_branch: 'a-long-default-branch-name-' + 'x'.repeat(30),
    policy: 'strict',
    deploy: null,
    observe: [],
    paused: true,
    paused_by: 'owner',
    counts: { queued: 0, running: 0, review: 0, awaiting_approval: 0, blocked: 0 },
  },
];

export const stats: Stats = {
  tasks_by_status: { queued: 2, running: 1, review: 1, awaiting_approval: 1, blocked: 1, done: 5 },
  active_runs: 1,
  approvals_pending: 2,
  cost_today_usd: 12.3456,
  subscription_cost_today_usd: 4.2,
  paused: false,
  pause: null,
  preflight: [{ project: 'onett', ok: false, path: PATH, error: LONG_TITLE, fix: UNBROKEN, checked_at: NOW }],
  holds: [{ harness: 'claude', until: '2026-11-05T13:00:00Z', message: LONG_TITLE, hits: 3 }],
  owner_unreachable: true,
  blocked_goals: [{ id: GOAL_ID, project: 'onett', title: LONG_TITLE, reason: UNBROKEN }],
  awaiting_owner: 1,
};

export const control = { paused: false, pause: null };

export const conversations: Conversation[] = [
  { id: CONV_ID, title: LONG_TITLE, project: 'onett', busy: false, created_at: EARLIER, updated_at: NOW },
  { id: id(90), title: NO_SPACE_TITLE, busy: true, created_at: EARLIER, updated_at: NOW, active_run_id: RUN_ID },
];

export const chatMessages: ChatMessage[] = [
  {
    id: id(91),
    conversation_id: CONV_ID,
    role: 'user',
    text: `${LONG_TITLE}\n\n${UNBROKEN}`,
    created_at: EARLIER,
    attachments: [
      { id: id(95), conversation_id: CONV_ID, name: 'screenshot.png', mime: 'image/png', size: 84_000, created_at: EARLIER },
      { id: id(96), conversation_id: CONV_ID, name: `${UNBROKEN}-quarterly-report.pdf`, mime: 'application/pdf', size: 2_400_000, created_at: EARLIER },
    ],
  },
  {
    id: id(92),
    conversation_id: CONV_ID,
    role: 'assistant',
    text: `Here is a long answer.\n\n\`\`\`\n${UNBROKEN}\n\`\`\`\n\n| a | b | c |\n|---|---|---|\n| ${UNBROKEN} | ${PATH} | ${SHA} |\n\n${NO_SPACE_TITLE}`,
    run_id: RUN_ID,
    created_at: NOW,
  },
  { id: id(93), conversation_id: CONV_ID, role: 'user', text: 'queued message', created_at: NOW, queued: true },
];

export const agentConversations: AgentConversation[] = [
  { task_id: CHAT_TASK_ID, title: LONG_TITLE, project: 'onett', role: 'implementer', task_status: 'running', live: true, closed: false, updated_at: NOW },
];

export const taskConversation: TaskConversation = {
  task: tasks[0],
  conversation: { id: id(94), title: LONG_TITLE, busy: false, created_at: EARLIER, updated_at: NOW, task_id: tasks[0].id },
  tasks: [tasks[0], tasks[1]],
  runs: runs.slice(0, 2),
  messages: chatMessages.map((m) => ({ ...m, conversation_id: id(94) })),
  can_send: true,
};

export const secrets: SecretView[] = [
  { name: 'GITHUB_TOKEN_WITH_A_VERY_LONG_NAME_FOR_LAYOUT', set: true, source: 'file', referenced_by: [`channels[0].options.${'bot_token_env'.repeat(3)}`] },
  { name: 'SENTRY_AUTH_TOKEN', set: false, source: '', referenced_by: [] },
];

const harnessCfg = { type: 'claude', command: ['claude'], args: ['-p', UNBROKEN.slice(0, 80)] };
const policyCfg = { max_lines: 500, max_files: 20, rules: [{ paths: [PATH], risk: 'high' as const, reason: LONG_TITLE }], gates: null };
const loopsCfg = {
  issue_intake: { enabled: true, every: '5m', label: 'onett' },
  exec_intake: { enabled: false, every: '5m' },
  goals: { enabled: true, every: '1m' },
  pr_shepherd: { enabled: true, every: '1m' },
  deploy_watch: { enabled: true, every: '1m' },
  prod_watch: { enabled: false, every: '5m', min_level: 'error', min_count: 3 },
  friction: { enabled: true, every: '1h', auto_queue: false, label: 'friction', min_count: 2 },
  retro: { enabled: false, every: '24h', lookback: '24h' },
  self_update: { enabled: false, every: '1h', min_healthy: '10m', command: UNBROKEN.slice(0, 80) },
};

export const config: Config = {
  server: { addr: '127.0.0.1:7777', data_dir: `/Users/someone/${'long-directory/'.repeat(6)}data`, public_url: '' },
  limits: { max_concurrent_runs: 2, max_runs_per_project: 1, stall_timeout: '15m', run_timeout: '1h', max_attempts: 3, max_goal_turns: 20 },
  harnesses: { claude: harnessCfg },
  agents: { implementer: { harness: 'claude', prompt: 'prompts/implementer.md', description: LONG_TITLE } },
  routing: { feature: 'implementer' },
  policies: { default: policyCfg },
  projects: { onett: { name: 'onett', repo: 'bobbyrc/onett', path: '/tmp/onett', default_branch: 'main', policy: 'default', paused: false } },
  loops: loopsCfg,
  notify: [{ type: 'log' }],
  channels: null,
  self_project: 'onett',
  path: `/Users/someone/${'long-directory/'.repeat(6)}onett.yaml`,
};

export const configDocument: ConfigDocument = {
  path: config.path,
  version: 'v1',
  document: {
    server: { addr: '127.0.0.1:7777', data_dir: config.server.data_dir },
    limits: { max_concurrent_runs: 2, stall_timeout: '15m' },
    projects: { onett: { repo: 'bobbyrc/onett', path: '/tmp/onett', default_branch: 'main' } },
  },
  schema: {
    key: '',
    type: 'section',
    fields: [
      {
        key: 'server',
        type: 'section',
        help: LONG_TITLE,
        fields: [
          { key: 'addr', type: 'string', help: 'Address to listen on.' },
          { key: 'data_dir', type: 'string', help: LONG_TITLE },
        ],
      },
      {
        key: 'limits',
        type: 'section',
        fields: [
          { key: 'max_concurrent_runs', type: 'int' },
          { key: 'stall_timeout', type: 'duration' },
        ],
      },
      {
        key: 'projects',
        type: 'map',
        item: {
          key: '',
          type: 'section',
          fields: [
            { key: 'repo', type: 'string' },
            { key: 'path', type: 'string' },
            { key: 'default_branch', type: 'string' },
          ],
        },
      },
    ],
  },
};

export const configText = {
  path: config.path,
  version: 'v1',
  text: `server:\n  addr: 127.0.0.1:7777\n  data_dir: ${config.server.data_dir}\n# ${UNBROKEN}\n`,
};

// Code blocks in agent Markdown: a long Go line and a shell block. Agent text reaches the chat
// and the run page through these log lines (see mobile.spec.ts).
const LONG_GO = `func main() { fmt.Println("${'long-line-'.repeat(30)}") }`;
logs.push({
  run_id: RUN_ID,
  seq: 9,
  time: NOW,
  kind: 'text',
  text: `Code:\n\n\`\`\`go\npackage main\n\n${LONG_GO}\n\`\`\`\n\n\`\`\`bash\ngo test -race -count=1 ./... && echo ${'done-'.repeat(40)}\n\`\`\``,
});

// The same code as a chat message with no run, so the orchestrator chat renders its text itself.
chatMessages.push({
  id: id(97),
  conversation_id: CONV_ID,
  role: 'assistant',
  text: logs[logs.length - 1].text,
  created_at: NOW,
});

/** A long conversation (40 turns), for scroll behaviour in the chat. */
export const longChatMessages: ChatMessage[] = Array.from({ length: 40 }, (_, n) => ({
  id: id(100 + n),
  conversation_id: CONV_ID,
  role: n % 2 ? ('assistant' as const) : ('user' as const),
  text: `Message ${n + 1}. ${'Some words to fill a few lines of a phone screen. '.repeat(3)}`,
  created_at: NOW,
}));
