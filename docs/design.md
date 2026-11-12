# Onett design (v0)

Onett is a personal software factory: one daemon that takes work (from chat, GitHub issues,
production errors, its own friction reports), hands it to coding agents, and carries the result
through PR, merge, deploy and verification without further human involvement.

Status: v0, written 2026-10-27 without a live design review. Every decision below is meant to be
cheap to revisit; the ones most worth a second look are marked **(review)**.

## Product direction

Onett is a **self-hosted** app: anyone runs it on their own machine or server, with their own
agent logins and repositories. Later the owner plans a **paid managed version** (onett hosted
for customers). Build for self-hosting now, and keep the seams a managed version needs, so it is
an extension rather than a rewrite.

Self-hosted, now:

- One binary and SQLite, no required cloud service; configuration is a file the user owns.
- Nothing about the owner's own setup in defaults, code or docs (user names, host names, paths).
- Runs headless on a Linux server as well as on a Mac: services beyond launchd (systemd), and a
  documented way to reach the UI remotely (reverse proxy with TLS, `allowed_hosts`).
- Users bring their own agent CLIs and logins; onett never needs the maintainer's accounts.

Managed, later (do not build these yet, but do not close them off):

- **Tenancy:** a daemon serves one owner today. The likely first managed shape is one isolated
  onett instance per customer, running the same code as self-hosted; true multi-tenancy is a
  later choice. Keep all state behind `store.Store` so another database can be added.
- **Auth:** people already sign in with accounts and sessions (roles viewer, operator, owner;
  `docs/proposals/accounts-and-config-editor.md`), and the API token is for the CLI and the
  orchestrator only. Still to come: GitHub access moves from the user's `gh` login to a GitHub
  App, which also gives agents a bot identity.
- **Agent isolation is mandatory** when customers' code and agents run on shared hardware:
  containers or VMs per run (option B in `docs/proposals/agent-isolation.md`), never option C.
- **Secrets:** per-project secrets held by onett and given to runs explicitly, not the daemon's
  own environment.
- **Metering:** runs already record cost and tokens; billing and quotas build on that.
- **Open questions for the owner:** licence and open-core split; whether managed customers bring
  their own model keys or pay through onett (check each provider's terms before reselling
  access to a subscription).

Rule for changes now: prefer the choice that keeps these seams cheap (interfaces, no hard-coded
paths or accounts, nothing that assumes one machine and one person) when it costs little extra.

## Principles

0. **The CEO model: full autonomy by default.** The owner acts like a CEO and the agents are
   employees. The owner states an idea; onett turns it into shipped, deployed software (goal,
   tasks, implementation, review, merge, deploy, health check) without further intervention. The
   built-in `default` policy therefore merges every risk level once the reviewer approves and CI
   is green, and the orchestrator turns multi-task requests into goals and acts without asking.
   Supervision is opt-in and never the default: the `supervised` policy, `limits`, project
   pauses, `loops.goals.enabled: false`, and `loops.self_update.enabled: false`. What stays on regardless are
   the hard limits below (principles 1 and 2, secrets never leaving the machine, the daily budget
   for pay-per-token runs, and preflight holds for runs that would hang) and a real need for the
   owner, who is reached through a configured notifier or channel. When a decision trades
   autonomy against caution, default to autonomy and make the caution a setting.
1. **Agents do judgment, code does gates.** Agents write code, review diffs, triage issues and
   diagnose incidents. Deterministic Go code commits, pushes branches, opens PRs, decides risk,
   merges, deploys and rolls back. An agent can never lower a risk rating or merge its own work;
   it can only raise risk. Worker agents get no API token, so they cannot act on onett at all;
   only the orchestrator (the agent you chat with) can, and only through the CLI.
2. **A permission denial or a stall is terminal for that run.** A run that is denied a tool, or
   produces no output for `limits.stall_timeout`, is stopped, its task becomes `blocked`, and you
   are notified with the exact reason. Onett never retries a denial or rephrases around it.
3. **Everything is a plugin.** Harnesses, SCM, deployers, observers and notifiers are Go
   interfaces with built-in implementations. Harnesses, observers and notifiers also have an
   `exec` type that runs any external program speaking a small JSON protocol, and the `command`
   deployer runs any shell commands, so you can add a harness or provider in any language
   without recompiling. (The code host is GitHub only in v0.)
4. **Configuration is a file you own.** `onett.yaml` (plus per-repo `.onett/` overrides) is
   the source of truth for projects, agents, policies and loops. The database holds only runtime
   state (tasks, runs, events). Put the config in your dotfiles repo if you like.
5. **One API for every client.** The web UI is just a client of the HTTP API (SSE for the CLI, a WebSocket for the UI)
   (`docs/api.md`); native and mobile apps later use the same API.
6. **Onett manages itself.** The onett repo is registered as a project, so friction that
   agents report becomes issues that agents fix — that is the self-improvement loop.

## Shape

```text
            chat / web UI / CLI / future apps
                         │  HTTP: SSE (CLI), WebSocket (UI) (/api/v1)
                ┌────────▼─────────┐
                │      onettd    │  single Go binary, SQLite state
                │                  │
   loops ──────►│  task queue ──► scheduler ──► supervisor ──► harness (claude, codex, exec…)
 (issues, PRs,  │                  │                 │            runs in a git worktree
  deploys, prod,│                  │                 ▼
  friction)     │   policy ◄── shepherd: push, PR, CI, review, merge
                │                  │
                │   deploy watch ──► deployer + health + observers ──► rollback
                └──────────────────┘
```

### Concepts

- **Project** — a repository onett manages: GitHub repo, local clone path, default branch,
  policy, deployer, observers. Defined in config.
- **Agent profile (role)** — a named recipe: which harness, which model, which prompt file,
  extra args. Built-in roles are job personas: `chief-of-staff` (the agent you chat with), `senior-engineer`,
  `code-reviewer`, `tech-lead`, `sre`, `platform-engineer`. The old names (`orchestrator`,
  `implementer`, `reviewer`, `triager`, `improver`) are aliases, renamed on load. A role may also
  carry a `title`, `context` files and `tools` rules. Any role can be redefined or added in config. Each role has an
  **origin**: `builtin` (unchanged default), `edited` (a built-in the file replaces) or `custom`
  (added by the file). Its instructions are a Markdown file (`prompt`) or inline text
  (`prompt_text`), never both. `disabled: true` turns a role off without deleting it, so a
  built-in can be removed from use and restored later. A project override can disable a role for
  that project, but cannot re-enable one disabled globally. Turning a role off is refused while a route
  (global, or a project's own) still uses it, so the owner moves those routes first. The
  chief of staff cannot be turned off, because chat needs it.
- **Harness** — an adapter that runs one agent CLI non-interactively and streams its output:
  `claude-code`, `codex`, `exec` (anything else), `fake` (tests).
- **Task** — a unit of work for one role in one project, with a kind (`feature`, `bugfix`,
  `issue`, `incident`, `review`, `chore`, `improve`), a status, and a source used for dedupe
  (`github_issue:42`, `signal:<fingerprint>`, `chat:<conversation>`).
- **Run** — one attempt at a task by one harness session, in its own git worktree, with a
  streamed log, cost, and an outcome.
- **Outbox** — files an agent writes in `<worktree>/.onett-run/` to report back:
  `result.json` (status, summary, self-assessed risk, PR title/body, follow-up tasks, friction).
  Files, not network calls, so it works inside sandboxed harnesses such as Codex's.
- **Waiting** — a queued task can wait on other tasks (`waiting_on`); the scheduler does not
  claim it until each has merged or finished without a pull request, and it is cancelled, with
  the reason, if one ends blocked, failed or cancelled. A follow-up waits for the task that filed
  it (its worktree is cut from the default branch, which lacks that task's code until the merge)
  and inherits that task's goal. An agent blocked by another task's unmerged pull request reports
  `depends_on` in `result.json`; onett then re-queues it as waiting, without using an attempt,
  instead of blocking it. Review tasks never wait for the task they review.
- **Policy** — deterministic rules mapping a diff (paths, size, labels, task kind) to a risk
  level, and the gates per risk level (auto-merge, review required, human approval required).
  The built-in `default` policy requires no human approval at any level.
- **Loop** — a recurring job: issue intake, PR shepherd, deploy watch, prod watch, friction
  filing. Each can be enabled, disabled and tuned per project.
- **Signal** — a deduplicated production problem (e.g. a Sentry issue) from an observer.
- **Approval** — a request for your decision (merge a high-risk PR, act on a blocked task).

### Task lifecycle

```text
queued ─► running ─► review ─► merged ─► deploying ─► done
             │          │                     │
             │          └► awaiting_approval ─┘ (you approve in the UI → merge)
             ├► done        (no code change needed: question answered, triage finished)
             ├► blocked     (denied, stalled, agent says blocked, retries exhausted)
             └► failed / cancelled
```

- `running` → the scheduler picked it and a run is live.
- Agents never commit (Codex's sandbox keeps `.git` read-only, so they could not anyway). When
  the run ends with changes, onett commits them with the agent's PR title, pushes branch
  `onett/<task-id-suffix>-<slug>` and opens a PR; the task moves to `review`.
- Before a re-run on a branch that already has a PR, onett merges the default branch in. If
  that conflicts, the agent is given the conflicted files to resolve; leftover conflict
  markers fail the attempt instead of being committed.
- In `review` the shepherd waits for CI, dispatches a `reviewer` run when the policy requires
  review, then asks the policy: auto-merge → `merged`; otherwise → `awaiting_approval`. Every
  merge is pinned to the PR head commit that CI, review and the policy judged
  (`gh pr merge --match-head-commit`), and an approval is for one head: if the PR changes after
  you were asked, onett withdraws the approval request and re-checks.
- Every automatic status change is guarded by the status it expects, atomically. A run or loop
  that finishes after you cancelled or blocked a task cannot revive it.
- An agent-reported failure, failing CI, a merge conflict or "request changes" from the
  reviewer re-queues the task for another implementer run on the same branch, up to
  `max_attempts`, then `blocked` (`running → queued → running …`; not drawn above).
- After merge, if the project has a deployer, the deploy watch takes it through `deploying`;
  a failed health window rolls back, opens an `incident` task, and notifies you.

### Run supervision

The supervisor owns every harness session:

- streams output into the run log (WebSocket to the UI) and updates `last_output_at`;
- kills the session after `stall_timeout` without output (status `stalled`) or `run_timeout`
  overall (`timed_out`);
- marks `denied` when the harness reports a permission denial;
- in every one of those cases sets the task `blocked` with the exact reason and notifies.

A subscription or provider rate limit is not one of those cases. When a harness reports that a
run stopped on its usage limit (Claude Code's 5-hour and weekly limits, Codex usage limits, a
provider 429), the run ends as `cancelled` with the limit's message, the task goes back to
`queued` without using an attempt, and the harness is held: the scheduler starts no run for any
role that uses it until the reset time plus a minute. With no reset time the hold backs off from
5 minutes, doubling per consecutive hit up to 1 hour. Other harnesses keep running, and dispatch
resumes by itself when the hold ends. Holds are saved as loop state, so a restart keeps them. `onett status` lists each hold. An orchestrator chat turn on a held harness is
not queued: the chat is told the limit and when it lifts.

#### Pre-approved `onett` calls for the orchestrator

Claude Code's auto permission mode runs a classifier over risky tool calls, and it once refused
the orchestrator's `onett task cancel`. Orchestrator runs (chat and goal turns) therefore start
with `--allowedTools "Bash(onett:*)"`, an allow rule for the `onett` command and nothing else.
Other roles get no such rule. Repeated `--allowedTools` flags add up, so an owner's own
`--allowedTools` in a harness's `args` is kept next to it rather than replacing it.

#### The owner's Claude setup and the prompt cache (`harnesses.<name>.isolate`, off by default)

Claude Code's prompt cache works on a prefix: the tool list, the system prompt, then the
conversation so far. If anything at the front changes, the whole history after it is read again
at full price. The owner wants their own Claude Code setup (claude.ai MCP connectors, plugins,
hooks, context) available to onett's agents, so by default a claude agent inherits it. The cost
is that it loads hundreds of tool definitions, and connectors that connect or drop between turns
change that list. Measured on one machine, a two-message `--resume` conversation, with a new
process per message, read about 65k input tokens per turn at a 18-34% cache hit rate; isolated,
about 18k at 78-80%. With `isolate: true` the claude harness adds `--strict-mcp-config` (no MCP
servers) and `--setting-sources project,local` (no user-level hooks, plugins or permissions; the
repository's own settings and CLAUDE.md still load, and the login is untouched). The owner's
`args` come after these and can override them. The orchestrator's system prompt is identical on
every turn of a conversation, and the changing list of projects and agents rides in the user
message instead. Each run records the cached and cache-written parts of its input
(`cached_input_tokens`, `cache_write_tokens`); `onett run show` and the run page show the hit
rate.

#### Full access per harness (`harnesses.<name>.claude.full_access`, `.codex.full_access`, off by default)

A harness block has a section per provider for options only that agent CLI has, and a section on
a harness of another type is a config error. The first option is `full_access`: it replaces the
adapter's built-in default (Claude's `--permission-mode auto`, Codex's `--approve-for-me`) with
`bypassPermissions` or `--dangerously-bypass-approvals-and-sandbox`, so tests that bind localhost
or need the network can run. It only replaces that default. The order, highest first, is the
conversation's permission choice, the agent's `permission_mode`, a permission or sandbox flag
already in the harness `args`, `full_access`, then the built-in default. `onett doctor` warns for
each harness that has it on. Future provider-specific options go in the same sections.

#### One agent process per conversation (`chat.session_idle_timeout`, 30m by default)

The better fix for the same problem is to stop starting a new process for each message, as an
interactive Claude Code session does not. For the owner's orchestrator chats and goal
conversations the claude harness keeps one `claude -p --input-format stream-json` process alive
per conversation. Each owner message, chat follow-up and goal check-in is written to its stdin;
MCP servers connect once, the tool list stays fixed inside the process, and the prompt cache keeps
hitting. The harness interface stays the same: the engine sets `Spec.ConversationID` and
`Spec.KeepAlive`, and `Start` returns a session for one turn. Its events end, and `Wait` returns,
when the turn's `result` line arrives, so each turn is still its own run with its own stall
watchdog, denial handling and token counts (`internal/harness/conv.go`).

The process stops when the conversation has been idle for `chat.session_idle_timeout`; the next
message then starts a new process with `--resume <session>`, which is what every message did
before. A denial, a stall or a timeout kills the process (a denial is terminal), and so does a
process that dies mid-turn, which fails that turn as a failed run does; the next message resumes
in a new one. A negative `session_idle_timeout` turns the feature off. Two things to know:
with the run shim on (`limits.run_shim`) each of these processes runs behind its own shim, named
`conv-<conversation id>`, so a restart of onett does not end it. The first turn after the restart
finds the shim, replays its log through a fresh parser (which rebuilds the session id and the
cost total) and writes to the same process. It does so only when the process was idle between
turns and was started with the same settings (the harness records the fingerprint in the shim's
directory); a process that was answering when onett went away, or whose settings changed, is
stopped and the conversation resumes its session in a new one. The exception is Claude's model
and permission mode: a change to either is written to the running process as a `set_model` or
`set_permission_mode` control request ahead of the next message, so the process and its prompt
cache stay. (Checked against claude 2.1.318. A refused request, such as an unknown model, is
shown in the log and the old setting stays. Effort has no such request and still restarts the
process.) Without the shim they are direct
children of onett and a restart ends them. And the environment is fixed when a process
starts, so `ONETT_RUN_ID` names the run of the turn that started it, and a task created in a
later turn is attributed to that earlier run. `ONETT_CONVERSATION_ID` does not change.

#### Run shim (`limits.run_shim`, on by default)

Lets the daemon restart without killing running agents. Unless `run_shim: false`,
each agent CLI is started by `onett run-shim` (the same binary, hidden from `onett help`), a
small process in its own session that owns the agent and its pipes. The shim keeps draining the
agent's stdout into `<data_dir>/runs/<run_id>/stream.log` whether or not the daemon is connected,
so a full pipe can never stall the agent, and writes `status.json` beside it (shim and agent
pids, timestamps, exit code, stderr tail). The daemon talks to it over a Unix socket at
`<data_dir>/s/<8 chars>.sock` (short, because macOS limits socket paths to about 104 bytes) with
a versioned JSON-lines protocol: `hello`, `replay` (from a byte offset, then live), `write_stdin`,
`close_stdin`, `signal` (SIGTERM or SIGKILL to the agent's process group), `status` and `ack`. The
shim exits when the agent has exited and the daemon acked the last offset, or 24 hours after the
agent exits. Through the harness the session looks the same as a direct child. See
`internal/runshim`.

**Reattaching after a restart.** At shutdown the daemon lets go of a task run's agent instead of
killing it (`Detacher`), and leaves the run and its task `running`. The run stores what a new
daemon needs: `stream_offset` (where in the shim's log to read next, saved after every flush of
the run log, so a line is never skipped and a hard stop repeats at most one batch), `last_output_at`,
`deadline_at` (started + `run_timeout`, absolute) and the workspace's `start_sha` and `remote_sha`.
On start, `recover` looks at each run still `running`: if its shim is alive (`status.json`, then a
dial) it replays the agent's output through the harness's parser, drops the events before the
stored offset, and supervises the run again with the stall clock seeded from the stored
`last_output_at` and the timeout from `deadline_at`. Cancel, stall and timeout reach the agent
through the shim's `signal`. The run then finishes through the same code as any other
(`settleTask`); setup owed after the agent (`setup_owed` on the run) and the owner messages the run
claimed (found by their run id) are restored first. If the shim is gone (reboot, shim crash), the
run ends `interrupted`, not `failed`, and its task is re-queued with the attempt given back, so
the interruption never uses one up, even on a task at its last attempt. The next run resumes the
interrupted run's harness session with a note to check the worktree and carry on
(`interruptedNote`); a run that had no session yet starts fresh with the same note. Reviews
are treated the same. Before that, `recover` replays the agent's output past the stored offset
from the shim's `stream.log` through the harness parser (`harness.LogReplayer`), so it is stored on
the interrupted run, and any session ID in it is kept for the resume. Covered: task and review runs,
and orchestrator chat and goal turns. A
turn is reattached the same way (the conversation's `active_run_id` finds it) and finishes through
the code a normal turn uses: the reply is stored once, the session id kept, the conversation
released and, for a goal turn (`goal_turn` on the run), the goal's progress check runs on the real
result, judged from the goal's `last_turn_at` because the in-memory snapshot is gone. If a turn's
shim is gone, `recover` does not fail the turn: it re-runs it by resuming the conversation's session
with a note that onett restarted. An interrupted goal turn is taken back out of `turns` and its
re-run is not judged, so a restart can neither use up a goal's turns nor block it.

### Risk and auto-merge **(review)**

Risk is `low < medium < high`. Final risk = max(rules matched by the diff, agent's own rating).
Default policy: every level auto-merges after green CI and an approving reviewer run; risk is
still computed and recorded on every PR. The built-in `supervised` policy (a project opts in
with `policy: supervised`) auto-merges only `low`; `medium` and `high` need your approval. Default high-risk paths: migrations, auth, CI/deploy config, secrets,
infra, dependency manifests with major bumps, `.onett/`, and onett's own gates and agent
prompts (in onett's repo only); default size limit 400 changed lines. A policy's `extra_rules`
add to these defaults, while `rules` replaces them, so use `extra_rules` unless you mean to drop
the built-in protections.
Work onett assigned itself (follow-ups and retrospectives, matched by task source) is rated
`medium` by default, which under `supervised` means a human approves it.

### Deploy watch

`deployer.deploy(ref)` (or, for platforms that deploy on push such as Vercel, locate the
deployment for the merge commit) → poll until ready → health window (`verify_for`, default
10m): health URL must stay 2xx and observers must not report new signals above threshold for
this release. If verification fails: `deployer.rollback(previous healthy)`, record it, open an
`incident` task with the evidence, notify. A release that fails before it is ready (build
error, or still not ready after `deploy.timeout`) is failed without a rollback, since nothing
new is serving; it still opens an incident and blocks the task.

A release is stored as `pending` and marked `deploying` just before its deploy command starts, so
the record says whether the command may have run. If the daemon stops while the command runs (a
restart, a crash, a reboot), onett never sees the command's result. It does not run the command a
second time; at the next start it judges the release by what is serving. The release goes
straight to the health window: it is kept if it passes, and rolled back like any other release
that fails verification. Without a `health_url` there is nothing to check, so such a release is
failed and rolled back to the last healthy one. A graceful stop kills the command along with the
daemon, but after a crash the command can keep running on its own. So onett writes the command's
process ID to `deploy-<release id>.pid` in the data directory while it runs, and after a restart
waits for that process to exit (for at most `deploy.timeout`) before it judges the release. The
provider ID of an interrupted release stays empty, because the command's output was lost; a
later rollback to that release passes an empty `ONETT_ROLLBACK_ID` with the ref. A health check
shows that something healthy is serving, not which version: a command killed before it switched
versions leaves the old version serving, and the release is still kept **(review)**. A release
still `pending` at start-up never ran its command (the daemon died between the two writes); it is
failed without a rollback, since nothing changed.

The evidence on a release and its incident is the deploy command's output tail and, on failure,
the output of the project's `deploy.logs` command. Secret values (environment variables named
like `*_TOKEN`, `*_SECRET`, `*_KEY`, ..., and URLs with credentials) are replaced with `***` before
anything is stored or handed to an agent. A task blocked because its own release failed becomes
done when a later healthy release contains its merge commit and no commit in that release
reverts it: the code shipped, so nothing is left to do for it.

A deploy command that exits 0 may leave a service running in the background, and that service
keeps running. A failed or timed-out deploy has its whole process group killed. The command's
output goes to temporary files rather than pipes, so a service that keeps printing is not killed
when onett stops reading.

### Production watch

Each observer is polled every `prod_watch.every`. New signals (by fingerprint) above the
project's threshold become `incident` tasks for the `sre` role, deduped by fingerprint; a
signal that keeps firing after its fix merged reopens.

### Friction and self-improvement

Every agent prompt ends with the outbox contract, including a `friction` list: anything that
slowed the agent down (missing docs, flaky test, bad tooling, onett itself). Onett dedupes
friction by fingerprint and, when `friction.auto_file` is on, files a GitHub issue in the right
repo (the project, or onett's own repo for factory friction) labelled `friction`. Issue intake
picks up issues labelled `onett` (configurable), so turning a friction issue into work is one
label — or automatic with `friction.auto_queue`. Comments from OWNER/MEMBER/COLLABORATOR are
included in the task body; others are ignored.

Before it opens an issue, the friction loop looks for an open one about the same problem, in three
steps, and stops at the first that finds one. First, an issue with the same title is linked.
Second, the reporting agent may have named one: each task prompt lists the project's open friction
issues (number and title, the 20 most recently reported, redacted and labelled as data), and a
friction entry can carry `"issue": <number>`. Go code accepts that claim only when the issue is
open and a friction report of the same project is already linked to it; a rejected claim is logged
and ignored. Third, the report's words are compared with each open friction issue's: titles that
share half their words, or titles plus bodies that share a quarter (Jaccard similarity of the
stemmed word sets), count as the same problem. A report matched in the second or third step is
added to that issue as a "Reported again" comment with its full title and body, and its friction
row is linked to the issue; once an issue has ten such comments, later reports are linked without
a comment. The agent's claim can only attach a report to an issue: it cannot close or relabel one,
and the report is always stored.

The word comparison is strict on purpose. On the 84 friction issues filed up to 2026-11-06 it
merged no two different problems and caught 18 of the 29 duplicates; the paraphrases it misses are
what the agent's claim is for.

Friction text, and the summary or blocked reason onett posts on an intake issue, are written by
agents and go to a tracker that may be public. Both are redacted with `redactSecrets` before they
are posted, and mention-quoted as before; the stored friction row keeps the original text. The
friction loop opens at most 20 new issues per project per day. Once the day's 20 are used it stops
filing until the next day: the remaining reports stay stored and unfiled, and a log line says how
many.

The self_update loop (`loops.self_update`, on by default for `self_project` once a command is set or inferred, `enabled: false` turns it off) keeps the
running daemon current. The target is the newest release of the self project that is `healthy` and has been for
`min_healthy`, even when newer releases exist (merges landing every few minutes would otherwise
keep the newest one too young). The scan runs through the last 50 releases and stops at the commit
the daemon was built from, since anything older is not an update. When there is a target, code starts the configured
`command` with `ONETT_UPDATE_REF=<sha>`, detached in its own session (a launchd stop kills the
daemon's process group, and the updater must survive it). Agents never trigger it: the decision is
deterministic code over deployment state. It does not wait for a quiet factory and does not
pause anything: agent runs and orchestrator turns survive a daemon restart. A run lives behind a
detached `onett run-shim` process that the new daemon reattaches to at startup; if the shim is gone,
the run resumes the agent's session. The one thing a restart would break is a release in flight,
because a deploy command is a child of the daemon, so the loop does not start the updater while any
project has a release `pending`, `deploying` or `verifying`. So that releases cannot starve it,
an eligible target takes priority: the deploy loop starts no new release while one exists (a
release already in flight finishes), and a release settling kicks the self_update loop at once
instead of waiting for its next tick. A tick that finds a target but cannot start logs one INFO
line with the ref and the reason. The updater swaps the binary,
restarts the daemon and rolls back on a failed health check; it leaves a result file that the next
daemon reports once. (The retired `max_defer` key is still accepted, ignored, with a warning in the
log.) A PID lock file keeps one updater at a time, and a commit that fails three
times (counted in memory) is skipped in favour of an older eligible release.
The daemon writes its own timestamped lines (`<RFC 3339 UTC time> onett: ...`) into `self-update.log`
around each attempt: the start with the target commit, a failure when the updater cannot start or
exits non-zero, and the outcome (ok or rolled back) that the next daemon reports at startup. The
updater's own output lines in between are not stamped. The updater must outlive the daemon it
restarts, so its output goes straight to the file; stamping each line would put a pipe under it,
and a pipe whose reader dies kills the writer (SIGPIPE) in the middle of an update.

The retro loop (`loops.retro`, default every 24h) is the factory's retrospective. Each day, code
gathers the evidence of the last `lookback` (blocked and failed tasks with their reasons,
re-queued attempts, friction, incidents, review findings sent back) and files one improve task
for the improver agent with that evidence. The agent picks the most valuable root cause to fix,
files the rest as followups, and names duplicate friction issues in its summary, since onett
never closes issues itself. Nothing is filed when there is no evidence. It is on only for
`self_project` unless `enabled` says otherwise.

### The orchestrator (main agent)

Chat messages go to a persistent `orchestrator` harness session (resumed by session ID per
conversation). It runs with the `onett` CLI on its PATH and `ONETT_API`/`ONETT_TOKEN` in
its environment, so it delegates by running `onett task create …`, inspects with `onett
task list`, `onett run logs`, and so on. Its answer streams back to the UI over its WebSocket.

Each conversation may carry settings: a model, a reasoning effort and a permission mode, all
optional (empty means the role's default from the config). The owner changes them in the composer
footer; they apply from the next turn, and each run records the model, effort and permission mode
it used. For Claude Code a new model or permission mode reaches the long-lived process in place:
onett writes a `set_model` or `set_permission_mode` control request to it ahead of the next
message, so the process and its prompt cache stay (see "One agent process per conversation").
A new effort has no such request, and neither has clearing a model back to a default the role
does not name: those cannot be done in place, so the next turn replaces the process and resumes
the session with the new flags. The choices per harness are
listed in one place each (`choices.go` in the adapter). Only a signed-in person may change the
settings: the API token, which the orchestrator holds, cannot widen its own permissions.

The owner can keep typing while the orchestrator works. If its harness takes input mid-run, the
message goes straight to the live agent. Otherwise it is queued (`Queued` on the message) and
sent as the next turn's prompt, joined with any other queued messages, but only after a turn that
succeeded. A turn that was cancelled, denied, stalled, timed out or failed leaves the queue
alone, as does any other terminal outcome (invariant 4); only an explicit owner send or flush
starts the next turn. A queued message can be edited or cancelled until a turn takes it.

The orchestrator also follows up on its own. `onett task create` run inside a chat turn (it sees
`ONETT_CONVERSATION_ID`) gives the task the source `chat:<conversation id>`; `--source` overrides
that. A loop in the engine (`chat_followup.go`) watches those tasks. When one reaches done,
blocked, awaiting approval, cancelled or failed, it is held for two minutes so that tasks settling
together share a turn; then, if the conversation is open and idle and neither the factory nor the
conversation's project is paused, one orchestrator turn runs with a `system` message that lists
each task's status, PR, blocked reason and summary (redacted, marked as data) and asks for one to
three sentences, plus action only on what the owner already asked for. A conversation gets at most
one such turn per two minutes. The pending list is in memory: a restart forgets it and starts from
the tasks' current statuses, so nothing is reported twice. A follow-up that an agent files for such
a task keeps the source `followup` (policy rules match it) and carries the link in its source ref,
`chat:<conversation id>`, so the loop reports it too.

### Chat attachments

The owner can attach photos and files to a chat message. `internal/attach` stores each file under
`<data_dir>/attachments/<conversation id>/<attachment id>-<name>` (mode 0600, directories 0700)
and its record (id, name, sniffed type, size, path) in the store; a message links to its
attachments by id. The API and, later, the chat channels both save through `attach.Manager.Save`.

- The client's file name is only a label: just its base name is kept, stripped of path separators,
  `..`, control characters and leading dots, and capped at about 100 bytes. The final path is
  checked to stay inside the attachments directory.
- The type is sniffed from the first 512 bytes (plus HEIC); the extension and the client's
  Content-Type are ignored.
- The size limit (`chat.attachments.max_size`) is enforced while streaming; an upload over it
  leaves no file and gets 413.
- Upload and download need a signed-in person, not the API token. Raster images and PDFs are served
  inline; everything else, SVG and HTML included, is a forced download, always with `nosniff` and a
  sandboxing CSP. File contents never go into logs or events.
- Housekeeping deletes attachments older than `chat.attachments.retention` (default 30 days): the
  file, the record, and the conversation directory once empty. A message that referenced a deleted
  file keeps rendering and simply lists one attachment fewer (**decision:** the reference is dropped
  rather than shown as "expired", so clients need no extra state).

The orchestrator's prompt for a message ends with a list of its attachments (name, type, size, ID
and absolute path), labelled as data. The stored message keeps only the owner's text. Task agents
run in other worktrees and cannot see those paths, so the note tells the orchestrator to pass an
ID with `onett task create --attach <id>` (repeatable; `POST /tasks` `attachments`).

A task keeps its attachment references in `task_attachments`, with the name, type and size copied
in. At the start of every run (so retries and follow-ups get them too) the workspace copies each
file into the fresh worktree at `.onett-run/attachments/<name>` (mode 0600; a repeated name gets the
attachment ID as a prefix). That directory is inside `.onett-run/`, which git already ignores, so
the files never reach a commit. The task agent's prompt lists them by worktree-relative and
absolute path, and says to open them with Read. **Decision:** unlike a chat message, a task keeps
the reference when retention deletes the file: the agent is told the file has expired and the run
goes ahead, because a task's body may depend on it and silently dropping it would hide that.

### Goals

A goal (`domain.Goal`) is an objective the owner states once for one project; it gets its own
orchestrator conversation. The `goals` loop (default every 1m, per-project `enabled`) looks at each
active goal and starts an orchestrator turn in that conversation when the previous step has settled:
every task linked to the goal (`Task.GoalID`) is done, blocked, failed or cancelled, the
conversation is not busy, and something changed since the last turn (a goal task changed, or the
owner wrote in the conversation; the first turn needs nothing). A task still queued, running, in
review, awaiting approval, merged or deploying makes the goal wait, so approvals stay with the
owner. The turn is a `system` message holding a check-in (the goal, each task's status, blocked
reason and summary, and what to decide), run through the normal chat path with `ONETT_GOAL` in the
orchestrator's environment, which the CLI uses to link every `onett task create` to the goal; no
fake owner message is written. Two guards keep an unattended loop from spinning: a turn that leaves
the goal active but queued no task, retried none and did not finish or block the goal gets one
second chance (the next check-in opens by saying so and the loop wakes at once), and a second such
turn in a row blocks it; so does reaching `limits.max_goal_turns` (default 40). Blocking, and `onett goal done`, notify the
owner. A task that blocks inside an active goal does not notify: the goal's orchestrator handles it
first, and the owner hears only if the goal blocks. The daily budget, `onett pause` and project pauses stop turns the same way they stop runs.
Goals add no new way to act: the orchestrator still only queues tasks, and merges still wait for
the project's policy (the owner only under `supervised`). `loops.goals.enabled: false` turns the
loop off.

### Heartbeat

The `heartbeat` loop (`loops.heartbeat`, default every 15m, per-project `enabled`) is the sweep for
work nobody is looking at. Each tick, per project, code builds an attention digest from the store
(`internal/engine/heartbeat.go`): blocked and failed tasks, blocked goals, runs that stalled, were
denied, failed or timed out since the last digest, queued tasks waiting on a task that is blocked,
failed, cancelled or already landed, tasks queued longer than `stale_queued` (default 4h), loops
whose last run failed, failed or rolled-back deploys, self-update failures, a failing preflight,
pending approvals, and agent results that say they need a human (matched conservatively by
`resultNeedsHuman`). Task-level items (blocked, failed, dependency, stale queued, agent question)
are skipped for tasks in an active goal, because that goal's orchestrator owns them; tasks with no
goal, or whose goal is blocked, paused or done, stay in, and so do blocked goals. Each item has a stable key (kind, entity id, a fingerprint of the reason).
The loop keeps the keys it has reported, with their first-seen times, in its `LoopState.Cursor`, so a
restart does not repeat them. When the digest holds a key that is not stored, the loop calls
`HeartbeatWaker.WakeHeartbeat`; otherwise nothing runs and no model is called. If the call fails, the
tick still counts and escalates (see below) and then reports the error as the loop's failure; the
new keys stay new, so the next tick wakes again, and the window for one-off items (runs, deploys,
results) stays open so they are still in that digest. `quiet_hours` (`"22:00-07:00"`, daemon local time,
may wrap midnight) skips the tick; what happened meanwhile is in the first digest after it. The
loop records `next_run_at` and `item_count` in its `LoopState`. The waker (`heartbeat_waker.go`)
posts the digest as a hidden internal message in one "Operations" conversation, created on first
use and found afterwards by its `kind` (`operations`), not its title, so the owner can rename it. It
then runs an orchestrator turn there like a chat follow-up; if a turn is already running, the digest
is queued behind it, never run beside it. The prompt (`heartbeatPrompt`) lists each item, marks new
ones, labels the item text as data, not instructions, and limits the orchestrator to what the owner
already asked for (retry, cancel duplicates, re-queue a dependency, file one small fix, resume a
goal, answer an agent), ending with one `<entity id>: <action | left alone | escalate>` line per
item.

The loop is the only writer of the per-item memory, kept in the same cursor: how many ticks in a
row (quiet hours not counted) the key has been in the digest, and the last five actions. At the
start of each tick it reads the orchestrator's replies in the Operations conversation posted since
the last tick, parses the `<entity id>: <text>` lines whose id is a remembered item
(`parseHeartbeatReplies`) and appends each to that item's actions; the prompt shows them as
"previously:" and tells the orchestrator not to repeat one. An item that leaves the digest is
forgotten, and a changed reason is a new key with fresh memory. Escalation is plain code, no model
call, and happens on unchanged digests too: once per key, the loop sends one high-priority
notification (item, time open, actions tried, link to the Operations conversation) when the item
reaches `escalate_after` heartbeats (default 3), or at once when a reply line says `escalate:`. The
old cursor format (first-seen times only) is migrated on read.
A failed self-update (the updater could not start, exited non-zero, or was rolled back) is kept by
the self_update loop in its own `LoopState` row (`self_update_failure`) and not found from its
system event, which ages out. It stays in the digest until the daemon runs a version other than
the one that failed to update, however it got there.

Escalation depends on the heartbeat running at all, so a loop that keeps failing also tells the
owner directly, in plain code with no model call (`internal/engine/loop_alert.go`). This covers
every loop, the heartbeat included: an expired `gh` login shows up as `pr_shepherd` failing, a
broken model call as `heartbeat` failing. After each pass `runLoopOnce` counts the loop's failed
passes in a row for that project, whatever the error text (a text that changes every pass must not
hide a loop that never succeeds). The owner gets one high-priority notification when both hold:
at least 3 failed passes in a row, and a streak at least 10 minutes old (three failed passes of a
30-second loop are a blip). It names the loop and project, the latest error (redacted like any
loop error) and when the streak started. While the loop keeps failing it is repeated every 24h;
the first successful pass ends the streak. The streak is kept in a `LoopState` row
(`loop_alert:<loop>`), so a restart neither forgets it nor repeats the notification. The
thresholds are constants, not config.

### Triggers

A trigger (`domain.Trigger`) is a rule a signed-in owner defines, on the Triggers page of the UI or
through the API. When it fires, it starts one orchestrator turn with a prompt the owner wrote.
There are two kinds: `schedule` (at times the owner sets) and `github_issue` (when a new issue is
opened in the project's repository). Triggers are stored in the database (tables `triggers` and
`trigger_firings`), not in `onett.yaml`. Only a signed-in owner can create, change, delete, enable,
disable or fire one.

Each trigger has its own orchestrator conversation, titled `Trigger: <name>`, created at its first
firing. A firing posts one internal message there and starts a turn, or queues behind the turn
already running, through `wakeConversation`, the same function the heartbeat uses. The owner's
prompt comes first, between two marker lines that carry the firing's ID. For an issue, its title
and body follow as redacted evidence inside a code fence, so issue text cannot pass as the owner's
words. A firing that reads `started` can still be waiting: if its turn cannot start, or it sat
behind a turn that then fails, is denied, stalls or times out, the prompt stays queued until the
trigger's next firing or a person's message in that conversation. Onett does not retry after a
denial or a stall.

The conversation has the kind `trigger`. Every turn in it, whether a firing or a person typing
there, runs with the orchestrator role's default model, reasoning effort and permission mode.
Settings saved on it are stored but not applied, so an unattended run never inherits a setting
someone chose later.

A schedule is a five-field cron expression (minute, hour, day of month, month, day of week) plus an
IANA time zone (a named zone such as `America/Chicago`, with its daylight-saving rules). The parser
is in `internal/domain/trigger_cron.go` and needs no new dependency. A wall-clock time that the
clocks skip fires once, at the jump. A time that happens twice fires the first time only. After
downtime, a trigger makes one catch-up run and never a backlog.

`github_issue` polls, because onett is local by default and cannot receive webhooks. About every 30
seconds the `triggers` loop lists the project's open issues through the SCM plugin (the piece that
talks to GitHub). An issue fires a trigger at most once. The firing row is the record, so a restart
does not repeat it. Only issues created after the trigger was created or last enabled count. The
author must be `OWNER`, `MEMBER` or `COLLABORATOR`. That rule is a filter against strangers, not
proof that a person wrote the issue, so issue text is always evidence, whoever wrote it. Issues with
the friction label never fire: those
are onett's own reports, and letting them start turns could file more reports. While issue intake
is on for the project, an issue carrying the intake label goes to intake instead, because intake has
already made it a task.

A trigger only starts a turn, and only when work may start. Before each firing onett checks the
global pause, the project's pause, the daily budget, any rate-limit hold on the harness (the wait
after an agent CLI reports a rate limit) and, last, the cap of 20 firings per trigger in any rolling
hour. If one applies, the firing is recorded as `skipped` with a reason. A missed schedule time is
gone. A skipped issue stays pending and fires when the condition clears. The `triggers` loop keeps
running while paused, only to record what it skipped.

The API token can read triggers and their firings, but it cannot change or fire them (invariant 12
in `AGENTS.md`). The CLI is read-only: `onett trigger list` and `onett trigger history ID`.

This design depends on two things fixed in a separate token-boundary change, not here: the API
token can approve merges and post into any conversation, and before any account exists a local
process can become owner (see "Who can do what" in the proposal).

Details and the API: `docs/proposals/triggers.md`.

### Messaging a task's agent

The owner can also message the agent working on a task (`POST /tasks/{id}/messages`). The
messages live on a conversation attached to the task that started it (the root); follow-up tasks
share it. A live agent that takes input gets the message at once. Otherwise the message is queued,
and the next run carries it: it resumes the agent's harness session with just the owner's text, or,
for a task that has never run, joins the first prompt after the task text. The queue is claimed
when a run starts and handed back if the harness fails to start.

What happens when nothing is live depends on the newest task in the conversation. A blocked,
failed, in-review, awaiting-approval or done-without-a-PR task is re-queued (compare-and-set on its
status, attempts reset, stale reviews cancelled), so the agent continues on the same branch and
pull request through the normal publish path. A merged, deploying or done-with-a-PR task gets a
child task (source `conversation`, parent the newest task) that resumes the session on its own
fresh branch, because the old branch has shipped. A queued or running task does nothing here: for
a task re-queued by the message itself, the scheduler starts the follow-up run that claims them. An
automatic retry (a re-queue after a failure) does not deliver queued messages; they wait for a run
that succeeds or for the owner's Send now.

Queued messages move on only after a run that succeeded, once it is no longer registered as live.
A run that was cancelled, denied, stalled, timed out or failed leaves them (invariant 4); an
explicit send or `POST /tasks/{id}/conversation/flush` starts the follow-up. A follow-up run that
itself fails (the agent reports failure, a check fails, or the commit or push fails) is not
retried: it puts the messages it took back in the queue, where they show as not delivered, and
blocks the task with "follow-up failed: <reason>", so the owner decides (Send now, or Retry).
Runs that took no owner messages keep the normal re-queue-then-block rule. Nothing here adds a
path from agent output to a commit, push or merge: follow-ups run through the same publish code.

### When a conversation closes

A task agent's conversation stays open until something closes it, and closing only marks it
(`closed_at`, `closed_by`, `closed_reason`): it never changes the task, runs or git. Three things
close it. The agent, by setting `"finished": true` in a completed or no-changes report, closed by
`agent` with its summary as the reason (a blocked or failed report, and reviews, are ignored). The orchestrator, with `onett task close-conversation
ID --reason R` (`POST /tasks/{id}/conversation/close`), closed by `orchestrator`. The owner, by
cancelling the task (closed by `owner:<username>`, or `orchestrator` when the API token cancels)
or through the same route. A closed conversation refuses new messages; messages already queued
stay queued and visible but are never delivered, because `continueConversation` does nothing for
a closed one. Closing twice is a conflict. Retrying a cancelled task leaves the conversation closed, and the
retry run neither resumes the session nor delivers the queue; the owner reopens it. Only a
signed-in person can reopen (`POST /tasks/{id}/conversation/reopen`), so an agent cannot undo an
owner's decision. Sending, flushing, editing and cancelling messages are person-only too. Both
routes emit `conversation.updated` with the root `task_id`.

A task re-queued or created by an owner message carries `follow_up_pending`; only its run claims
the queue, resuming the session with the owner's text plus any merge-conflict section. An
automatic retry after a failure never has the flag, so it keeps its attempt notes and leaves the
queue alone. Messages to a review task's agent are not continued after the review finishes (the
review outcome is applied first and returns).

## Code layout

```text
cmd/onett/          one binary: `onett serve` (daemon) and CLI subcommands (API client)
internal/domain/      shared types: Task, Run, Event, Approval, Deployment, Signal, Friction, outbox
internal/config/      onett.yaml schema, loading, defaults, validation
internal/store/       Store interface; store/sqlite implementation (modernc.org/sqlite, no cgo)
internal/events/      event bus: persist + fan-out to SSE and WebSocket subscribers
internal/harness/     Harness interface + registry; claude/, codex/, exec/, fake/
internal/runshim/     per-run shim process (`onett run-shim`), its socket protocol and client
internal/workspace/   git worktrees, diff stats, commit, push
internal/scm/         SCM interface; scm/github (gh CLI)
internal/policy/      risk evaluation
internal/deploy/      Deployer interface; command deployer; HTTP health checks
internal/observe/     Observer interface; exec observer; sentry observer
internal/notify/      Notifier interface; ntfy, discord, telegram, exec, log
internal/channel/     two-way chat bots (telegram, discord) the daemon starts from the channels config; each type registers itself (`channel.Register`) and `channel/all` links them in
internal/engine/      scheduler, supervisor, shepherd, loops, prompt assembly
internal/attach/      chat attachments: sanitised names, type sniffing, size limit, retention
internal/api/         chi HTTP API + SSE + WebSocket + embedded web UI
web/                  React + Vite + TypeScript UI (built into internal/api/webdist)
```

## Security defaults

- Binds `127.0.0.1:7777`. Every API call needs `Authorization: Bearer <token>` or a signed-in
  session; the token is generated on first start at `<data_dir>/token` (mode 600). Remote access
  is opt-in: reach it over a VPN such as Tailscale or through a reverse proxy with TLS, and name
  the address in `server.public_url` or `server.allowed_hosts` (see the Host check below).
- `onett backup FILE` writes a consistent copy of the database (`<data_dir>/onett.db`) that is
  safe to take while the daemon runs: SQLite's `VACUUM INTO` reads one snapshot. The copy holds
  password hashes and sign-in sessions, so it is not an API route: a route would need a signed-in
  owner, and the API token, which the orchestrator agent holds, must never be able to fetch it.
  Instead the command reads the database file itself on the daemon's machine, as
  `onett user reset-password` does, and writes the copy with mode 600, never over an existing
  file. It refuses inside an agent run (`ONETT_RUN_ID` or `ONETT_ROLE` is set) and refuses a
  destination inside a git repository or worktree, where the copy could be committed or read by
  an agent. Restore is in the README, "Backup and restore".
- Agents never get push credentials from onett; onett pushes. Harness permissions are
  configured per agent profile (`claude-code` defaults to `--permission-mode auto` plus the
  role's pre-approved `tools` rules; `codex` defaults to `--approve-for-me`, its workspace-write sandbox with automatic review
  of escalations; resumed Codex sessions get the same workspace-write sandbox through a config
  override, since `codex exec resume` takes no sandbox flag).
- Secrets for providers come from the environment or `~/.config/onett/secrets.env`.
- Agents get an allowlist of onett's environment (PATH, HOME, locale, their own provider
  keys), plus what a harness's `inherit_env` and the project's `env` add. Onett's GitHub and
  observer tokens never reach them. A project's `setup` and `checks` commands run with the same
  environment, because they execute code the agent may have written.
- The server answers only requests whose Host header is localhost, a loopback address, the bind
  host, the `public_url` host, or `server.allowed_hosts`. This blocks DNS rebinding, where a web
  page points its own name at 127.0.0.1. Responses carry a strict CSP and refuse framing.
  Behind a reverse proxy, forward the `Upgrade` and `Connection` headers, or the UI's WebSocket
  cannot connect, and keep the `Host` header the browser sent, since the host check and the
  WebSocket's Origin check both compare against it.
- Agent text that onett posts to GitHub (PR bodies, issues, comments) has an invisible
  zero-width space inserted after the @ of every mention outside fenced code blocks, so an agent
  cannot notify people. Release evidence is redacted
  (see Deploy watch).

### Known limits of the harnesses

- Claude Code reports refused tool calls as `permission_denials`; onett treats any as a
  denial. Codex does not report sandbox refusals in machine-readable form (only in prose), so
  for Codex the "denial is terminal" rule relies on the prompt ("if denied, report blocked")
  and the stall timeout.
- No harness reports a dollar cost while a run is going: Claude Code reports it, and Codex the
  tokens to estimate it from, when the run ends. The daily budget is checked before each run
  starts and sums today's runs, those in flight included, but a running run counts only what
  onett can estimate. With a `price` on its harness that has a `cached_input_per_mtok` rate,
  that is the prompt tokens of its requests so far (Claude Code streams each request's prompt
  size) at that rate, the cheapest kind of input, with output left out, so the estimate stays
  at or below the cost the run reports at the end and never stops runs on a guess that is too
  high. A run stopped before it reports a cost keeps the estimate. The billing mode is also
  detected only at the end, so the estimate of a run on a subscription counts while it runs
  unless its harness says `billing: subscription`. Without such a `price`, and for Codex, a
  running run counts as $0, so runs started together can still overshoot the budget.

## Not in v0

Native/mobile apps (API is ready for them), multi-machine workers, cost budgets finer than the
one daily limit (`limits.daily_budget_usd`), Vercel/Render native
deployers (use the `command` deployer with their CLIs meanwhile).
