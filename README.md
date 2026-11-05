# onett

Onett is a personal software factory. It is a local daemon (a background program that keeps
running) that hands work to coding agents: Claude Code and Codex today, and any other agent CLI
through a plugin. For each piece of work it runs the agent in its own git checkout, opens a pull
request, waits for CI and an agent review, and applies a risk policy that decides whether the change
may merge. After a merge it can deploy, check the deploy, and roll back if the check fails. It also
watches production for errors and files the problems it finds as issues. You steer it by chat, from
a web UI, or with the `onett` command.

## The CEO model

Onett's default is full autonomy. You act like a CEO and the agents are the employees: you state an
idea, and onett turns it into shipped, deployed software without further intervention. Supervision
(approvals, caps, pauses) is opt-in configuration for people who want to rein it in. It is never the
default.

When you state an idea in chat, the orchestrator (the agent you talk to) decides how big it is. A
single small change becomes a task. Anything with several steps becomes a **goal** (see
[Goals](#goals-unattended-multi-step-work)), which the `goals` loop drives until it is done. Each task is
implemented by an agent in its own checkout, reviewed by a second agent, merged once CI is green, and
deployed if the project has a deployer. A failed health check rolls the release back. The default
policy merges at every risk level; risk is still computed and recorded on each PR.

What stops it:

- **Hard limits that stay on.** A denied tool call is never retried in the same form, and a run with
  no output for `limits.stall_timeout` stops; both block the task with the reason. Secrets never go
  into repositories, logs or chat. The daily budget stops new runs that bill per token. A
  preflight check holds runs that would hang, such as an unreadable directory.
- **A real need for you.** When an agent reports that it needs a human, a task runs out of
  `limits.max_attempts`, or a goal cannot move on, the task or goal is `blocked` and onett tells you
  through the notifier or channel you configured (ntfy, Telegram, Discord and so on).

Reining it in, per setting:

- `policy: supervised` on a project: `low` risk still merges by itself, `medium` and `high` wait for
  your approval (`onett approvals`, `onett approve`).
- `limits.daily_budget_usd`: stop new pay-per-token runs once today's cost reaches the cap.
- `paused: true` on a project, or `onett project pause NAME`; `onett pause` stops the whole
  factory. Running agents finish.
- `loops.goals.enabled: false`: no unattended multi-step work; the orchestrator creates tasks only.
- `loops.self_update.enabled: false`: onett never replaces its own binary.

What keeps it running unattended:

- **Rate limits are waited out, not failed.** When Claude Code or Codex reports a usage limit, onett
  holds new runs on that harness until the reset time (or a growing backoff when none is given),
  requeues the task without using an attempt, and resumes by itself. `onett status` shows the hold.
- **Goals get a second chance.** A goal turn that makes no progress gets one explicit "change
  approach" check-in before the goal blocks. A task that blocks inside an active goal goes to the
  goal's orchestrator first; you hear about it only if the goal itself blocks.
- **Self-update is on by default** for the `self_project` once its update command is known, so onett
  keeps running its own latest release. `loops.self_update.enabled: false` turns it off.

Each is described under [Safety model](#safety-model) and annotated in
[onett.example.yaml](onett.example.yaml).

## Terms used below

- **Task**: one unit of work for one project, such as a feature, a bug fix or an incident.
- **Role** (agent profile): a named recipe for who does a task: which agent CLI runs it, which
  model, and which instructions. Built-in roles are `orchestrator` (the agent you chat with),
  `implementer`, `reviewer`, `triager`, `sre` and `improver`. You can edit a built-in role, turn it
  off with `disabled: true` (it cannot be turned off while a route still uses it, and the
  orchestrator cannot be turned off), or add a custom role. A role's instructions are a Markdown
  file (`prompt`) or written inline in the config (`prompt_text`). Inline instructions cannot use
  `${NAME}` references, because they are given to the agent, which never gets credentials.
- **Harness**: the adapter that runs one agent CLI non-interactively and streams its output back.
- **Run**: one attempt at a task by one agent session, in its own git worktree (a second checkout
  of the repository on its own branch).
- **Policy and risk**: rules that look at a diff and rate it `low`, `medium` or `high`. The policy
  decides which levels may merge on their own: by default all of them, after review and green CI;
the built-in `supervised` policy holds `medium` and `high` for you.
- **Loop**: a recurring background job, such as checking open PRs or polling production.
- **Outbox**: a folder in each worktree where the agent writes its report, `result.json`.
- **Approval**: a request for your decision. Under the opt-in `supervised` policy, a `medium` or `high` risk PR waits for one.

## How a task flows

1. A task is created from chat, the CLI, the web UI, a GitHub issue with the `onett` label, a
   production error, or a friction report. It waits as `queued`.
2. The scheduler starts a run. The agent works in the worktree and writes its report. Agents never
   commit; onett does.
3. If the run changed code, onett commits it, pushes a `onett/...` branch and opens a PR. The
   task moves to `review`.
4. The PR shepherd (the loop that moves PRs along) waits for CI, starts a `reviewer` run when the
   policy requires one, and then asks the policy. the PR merges automatically once the reviewer
   approves and CI is green, at any risk level. Under the opt-in `supervised` policy, `medium` and
   `high` risk move the task to `awaiting_approval` and wait for you.
5. After a merge, a project with a deployer goes through `deploying`. A health window (default 10
   minutes) checks the release. A failed check rolls back, opens an `incident` task and notifies you.
6. The task becomes `done`.

What waits for you: `blocked` tasks, and approvals (`onett approvals`, then `onett approve`) when
you opted in to `supervised`. A task is blocked when a tool call is denied, when a run stalls, when the agent reports that it needs a
human, or when it has used up `max_attempts`. Retry it with a note, or cancel it.

## Safety model

- **Agents judge, code gates.** Agents write code, review diffs and triage issues. Deterministic Go
  code does everything that leaves the machine: commits, pushes, PRs, merges, deploys and rollbacks.
- **Agents cannot approve their own work.** Worker agents get no API token. Only the orchestrator
  does, and it uses the CLI.
- **People sign in; config and users change only through them.** The web UI signs people in with
  a username and password (roles: viewer reads, operator also acts on work, owner also edits
  `onett.yaml` and manages users). The API token, which the CLI and the orchestrator hold, can
  read and act but can never edit the config or users, so an agent cannot rewrite its own policy
  or environment. The first visit to a new instance creates the owner account, so create it
  before you make onett reachable from other machines. Behind an HTTPS proxy, set
  `server.public_url` to the `https://` address so the session cookie is marked Secure. This
  boundary is the API's: agents still run as your user, so one that can write `onett.yaml` or
  the database file directly is limited only by its harness until agents are isolated (see
  [docs/proposals/agent-isolation.md](docs/proposals/agent-isolation.md)).
- **Agents get a small environment.** An agent sees an allowlist (`PATH`, `HOME`, `USER`, `LANG`,
  ...), not onett's own environment, so `GH_TOKEN` and provider tokens do not reach it. To pass
  one through, name it in the harness's `inherit_env` list. Agents still run as your user and can
  read what your user can read; isolating them is an open decision, see
  [docs/proposals/agent-isolation.md](docs/proposals/agent-isolation.md).
- **Self-assigned work is rated higher.** Follow-up and retro tasks (task sources `followup` and
  `retro`) are rated at least `medium` (under `supervised`, they wait for you) by a built-in policy rule, and so is any change to onett's
  own gates and agent instructions (`high`). Add rules with a policy's `extra_rules`. Setting
  `rules` instead **replaces** all the built-in rules, so copy back any you want to keep.
- **Daily budget.** `limits.daily_budget_usd` stops new task runs once today's reported cost
  of pay-per-token runs reaches it. Runs on a flat-rate subscription (Claude Code signed in with
  claude.ai, Codex signed in with ChatGPT) report only an API-price estimate that nobody is billed
  for, so they are left out and `onett status` shows them separately. Onett detects the mode per
  run; a harness's `billing` (`auto`, `metered` or `subscription`) overrides detection, and a run
  whose mode is unknown counts. A run's cost is recorded when it ends, so runs in flight count only after they finish
  and the total can overshoot. Codex reports no cost, so it counts only if its
  harness has a `price` (per-million-token rates), which gives an estimate shown as `~$0.42`.
- **Risk only goes up.** Final risk is the highest of the rules the diff matches and the agent's own
  rating. An agent cannot lower it.
- **Merges are pinned to a commit.** A merge uses `gh pr merge --match-head-commit`, so it merges the
  exact commit that CI and review judged. An approval covers one commit. If the PR changes, the
  approval is withdrawn and the PR is checked again.
- **Denials and stalls stop, they do not retry.** A denied tool call, or no output for
  `limits.stall_timeout` (default 15 minutes), ends the run and blocks the task with the reason.
- **Pause switch.** `onett pause` stops new runs and loop actions. Running agents finish. A
  single project pauses with `onett project pause NAME` (`resume` undoes it) or on the web UI's
  Projects page; this survives restarts, but a `paused: true` in `onett.yaml` can only be cleared
  in the file. While paused, the `deploy_watch` and `prod_watch` loops keep running, so a release
  already in flight is still health-checked and rolled back.
- **Project setup and checks.** A project's `setup` commands run in each fresh worktree before the
  agent starts (install dependencies). Its `checks` (name, `run` shell command, optional `timeout`,
  default 10m) run after the agent finishes and before onett commits and pushes. A failing check
  re-queues the task with its output, like a CI failure, without spending a CI run. Both run with
  the agent's environment, not onett's, because they execute code the agent may have written.
  Setup is skipped on a tree with merge conflicts, and a setup failure that is not a network or
  registry error goes to the agent as its first job; setup runs again after the agent, and the
  task blocks with the real error if it still fails. Network-like failures wait a minute, then
  re-queue.
  A policy's `require_checks: true` makes a PR with no CI checks wait (then block after 30 minutes)
  instead of counting as "no CI"; use it for repositories that always run CI.
- **Evidence is redacted, agent text cannot ping.** Deploy output, service logs, loop errors and
  production signals have secret values (env vars named like `*_TOKEN`, `*_SECRET`, `*_KEY`, and
  URLs with credentials) replaced with `***` before they are stored or shown to an agent. Agent text
  posted to GitHub has its @mentions neutralised, so an agent cannot notify people.
- **Local API.** It listens on `127.0.0.1:7777`. Every call needs the bearer token from `onett token`.
  Requests whose Host header is not local or configured are refused (DNS-rebinding guard).
- **Known limit with Codex.** Claude Code reports refused tool calls in machine-readable form.
  Codex reports sandbox refusals only in prose. For Codex, "a denial stops the task" therefore relies
  on the agent prompt ("if denied, report blocked") and on the stall timeout.

## Quick start

Prerequisites: `git`; `mise` (it installs the Go version in `mise.toml`); Node.js and npm (only for
the web UI build); `gh` logged in (`gh auth status`); and the `claude` and/or `codex` CLI installed
and logged in.

Install and start (build the binary with the web UI embedded; `OUT=path` relocates it):

```sh
mise exec -- scripts/build.sh
# Manual fallback: (cd web && npm ci && npm run build) && go build -o onett ./cmd/onett
./onett serve      # leave it running in its own terminal
```

Then finish setup in the web UI:

1. **Open the link `serve` prints.** With no config file, `serve` writes a starter one and, until
   an owner account exists, prints `No owner account yet: open http://127.0.0.1:7777/ ...`. Create
   the owner account on that page. (`./onett open` opens the UI later.)
2. **Follow the "Finish setting up onett" card** on the **Overview** page. It has one live check per
   step and links to where each is done:
   - **Add a project**: the **Projects** page, **Add project** (a repository, plus an optional
     checkout path and branch). **Remove** is there too.
   - **Agents ready**: the **Config** page, Agents group. Check that at least one agent is enabled.
   - **Choose a policy**: the Config page, Policies group. With none chosen, the default applies.
   - **Reach me**: the Config page's **Setup** tab walks through Telegram, Discord or ntfy and has
     a **Send test** button. See [docs/channels.md](docs/channels.md).
   - **Secrets set**: the Config page's **Secrets** tab stores tokens in `secrets.env`. It only
     writes: a saved value is never shown again.
3. **Change anything else on the Config page.** Its Visual tab has every setting, grouped, with
   defaults shown; the YAML tab edits the raw file. Changes apply live. Settings that need a
   restart (`server.addr`, `server.data_dir`) offer a **Restart now** button.

Before you point onett at a real repository, follow [docs/getting-started.md](docs/getting-started.md):
it starts with a throwaway repository and a paused project.

**Prefer a terminal or config-as-code?** Everything above can also be done with the CLI and files:

```sh
./onett init --project myproject --repo you/myproject   # writes ~/.config/onett/onett.yaml
./onett project add other --repo you/other               # add a project to the file
./onett doctor     # checks your setup: config, git, gh, harness CLIs, daemon
./onett serve      # run the daemon; leave it running in its own terminal
./onett open       # open the web UI in your browser (second terminal)
./onett chat "What is running right now?"
```

Edit `onett.yaml` by hand (every option is annotated in [onett.example.yaml](onett.example.yaml))
and keep tokens in `~/.config/onett/secrets.env`, then run `./onett config --reload`. Put `onett`
on your PATH to drop the `./`.

The web UI's **Chat** page works like a coding agent's terminal for every agent: talk to the
orchestrator, open any task's agent from the sidebar or from a task, watch its tool calls as they
happen (Conversation or Terminal view), type while it works, queue a message, stop a run with Stop
or Esc, and close or reopen a task agent's conversation. See [docs/ui.md](docs/ui.md).

To use the web UI on a phone, add it to the home screen; see [docs/ui.md](docs/ui.md#install-on-a-phone).

### Commands

Run `onett <command> -h` for a command's flags. For `task`, `goal`, `run` and `project`, put the verb first,
as in `onett task create -h`.

| Command | What it does |
|---------|--------------|
| `serve [--addr HOST:PORT]` | run the daemon: API, web UI and engine; `--addr` sets the listen address in the starter config written on first run |
| `init [--notify ntfy --ntfy-topic T] [--channel telegram\|discord --user-ids IDS]` | write a starter onett.yaml; the flags set up notifications without prompts |
| `token` | print the API token |
| `open` | open the web UI (sign in there) |
| `status` | what is running, and what needs you |
| `projects` | list managed projects |
| `project add\|pause\|resume` | add a project to the config, or pause or resume one |
| `agents [--all]` | list agent roles; `--all` also shows removed (disabled) ones |
| `config` | print the redacted config, or `--reload` it |
| `task` | create, list, show, cancel and retry tasks (`task create --attach ID`, repeatable, hands a chat attachment to the agent as a file under `.onett-run/attachments/`); `task message ID --text T` sends a message to a task agent; `task close-conversation ID --reason R` closes a task agent's conversation |
| `goal` | create, list, show, pause, resume and finish goals the orchestrator works on unattended |
| `run` | list, show, cancel and print logs of agent runs |
| `deploys` | list releases, or show one with its log |
| `approvals` | list approvals waiting for a decision |
| `approve` | approve or reject an approval |
| `events` | show history, or follow the event stream |
| `loops` | list recurring loops |
| `loop run NAME` | run a loop now |
| `user reset-password NAME` | give a user a new random password and sign them out everywhere |
| `pause` | stop starting runs and loops (`status` shows who paused it and since when) |
| `resume` | resume a paused factory |
| `chat` | message the orchestrator and print its reply |
| `service` | install, uninstall or check the launchd service that keeps serve running |
| `doctor` | check the setup: config, git, gh, harness CLIs, projects and the daemon |
| `version` | print the onett version |

### Run it as a service

On macOS, `onett service install` keeps the daemon running in the background and restarts it if it
crashes. It uses launchd, the macOS service manager, which reads a plist file (an XML settings file)
from `~/Library/LaunchAgents`. The daemon's log is `onett.log` in the config's data directory
(`server.data_dir`, `~/.local/share/onett` by default).

The Config page's **Restart now** button (owner only, `POST /restart`) restarts the daemon after a
change to `server.addr` or `server.data_dir`. It shuts down the way SIGTERM does, then: under this
service the process exits and launchd starts it again; run any other way (a terminal, the dogfood
script) it re-executes its own binary with the same arguments and environment, so it keeps the same
process ID.

```sh
./onett service install     # write the plist, start the daemon, keep it alive
./onett service status      # running (with pid) or installed/not installed, and the log path
./onett service uninstall   # stop the daemon and remove the plist
```

The plist sets `AbandonProcessGroup`, so stopping or restarting the service never kills the
per-run shims (and the agents under them) that outlive the daemon. Running `service install` again
rewrites a plist that differs from the current one (for example one written by an older onett,
which lacks that key) and reloads the service. The reload stops the daemon under the old plist, so
do it while no agent is running.

Run `service install` once, and do not also keep `onett serve` running in a terminal: two daemons
cannot both listen on the same address.

A launchd service on macOS also needs explicit access to external volumes and other
privacy-protected folders: a service that was not granted it hangs on any read under such a path
(for example a toolchain symlinked to an external drive), even though the same path works in your
terminal. The daemon checks the directories its agents read before it starts a run, holds a
project's tasks in the queue while one is unreadable, and notifies you with the path and the fix.
`onett doctor` shows the daemon's last result per project.

macOS ties that grant to the binary's code signature. `scripts/build.sh` signs ad hoc by default,
and an ad-hoc signature is the binary's hash, so every rebuild (including each self-update) looks
like a new app and loses Full Disk Access. To keep the grant, sign every build with a stable
certificate:

1. In Keychain Access, choose Certificate Assistant > Create a Certificate, give it a name
   (for example `onett-codesign`), set Identity Type to Self Signed Root and Certificate Type to
   Code Signing.
2. Build with `ONETT_CODESIGN_IDENTITY="onett-codesign" scripts/build.sh`. The binary is signed
   with that certificate and the identifier `io.onett.onett`.
3. Grant Full Disk Access to the signed binary once more (System Settings > Privacy & Security).
   Later builds signed with the same certificate keep the grant.

Without the variable, `build.sh` prints a warning on macOS, and `onett doctor` warns when an
ad-hoc signed binary meets an unreadable external-volume path.

launchd starts the daemon without your shell's environment. `service install` therefore records,
in the plist, the `PATH` of the shell you run it from and every variable your config references
as `${NAME}`. The plist is readable only by you, because those values can be secrets. Install
from a shell where `onett serve` works. To change a recorded value, run `service uninstall`
and then `service install` again.

### Goals: unattended multi-step work

The orchestrator creates a goal itself when you state a multi-step idea; you can also create one by
hand. A **goal** is an objective you state once, such as "build parts 1 and 2 of X, part 2 after part 1
merges". While it is active, the `goals` loop wakes the orchestrator, in the goal's own
conversation, each time the previous step has settled: it queues the next tasks (linked to the goal
automatically), retries a blocked one, or declares the goal done or blocked. It waits while any goal
task is still running, in review or waiting for an approval, and does nothing when nothing changed.
A turn that makes no progress, or the `limits.max_goal_turns` limit (default 40), blocks the goal
and notifies you. The daily budget and `onett pause` stop turns too.

```sh
onett goal create --project P --title "Ship X" --body-file goal.md   # prints the goal ID
onett goal list [--status active,blocked]
onett goal show ID                 # the goal and its tasks
onett goal pause ID                # and: onett goal resume ID
onett goal done ID --summary "what was achieved"
onett goal block ID --reason "what only you can unblock"
```

`onett task create --goal ID` links a task to a goal; it defaults to `$ONETT_GOAL`, which onett
sets for the orchestrator in a goal conversation. You can chat in that conversation to steer the
goal. The web UI shows the conversation as a normal chat; it has no goals page yet.

### Retro loop and friction

Agents list anything that slowed them down as `friction`. Onett files it as a GitHub issue and
dedupes by title: a near-duplicate of an open issue becomes a comment on that issue. The retro loop
(`loops.retro`, every 24h) hands the last day's blocked and failed tasks, friction, incidents and
review rejections to the `improver` role as one task. It is on by default only for the project named
by `self_project`; set `enabled: true` or `false` to override.

## Repository layout

```text
cmd/onett/      one binary: `onett serve` (daemon) and the CLI commands (API client)
internal/domain/  shared types: Task, Run, Event, Approval, Deployment, Signal, Friction, outbox
internal/config/  onett.yaml schema, loading, defaults, validation
internal/store/   Store interface; store/sqlite implementation (SQLite, no cgo)
internal/events/  event bus: stores events and fans them out to SSE and WebSocket subscribers
internal/harness/ Harness interface and registry; claude/, codex/, exec/, fake/
internal/workspace/ git worktrees, diff stats, commit, push
internal/scm/     SCM interface; scm/github uses the gh CLI
internal/policy/  risk evaluation
internal/deploy/  Deployer interface; command and noop deployers; HTTP health checks
internal/observe/ Observer interface; exec and sentry observers
internal/notify/  Notifier interface; log, ntfy, discord, telegram and exec notifiers
internal/channel/ two-way chat channels: Telegram and Discord bots, wired in by `onett serve` from `channels:`
internal/engine/  scheduler, supervisor, PR shepherd, loops, prompt assembly
internal/chat/    orchestrator conversations
internal/api/     HTTP API, SSE stream (CLI), WebSocket (web UI) and the embedded web UI
web/              React + Vite + TypeScript UI (built into internal/api/webdist)
docs/             design, API, UI and plugin documentation
```

## Documentation

- [docs/getting-started.md](docs/getting-started.md): from nothing to a first merged PR.
- [docs/design.md](docs/design.md): the architecture, task lifecycle, policy and known limits.
- [docs/api.md](docs/api.md): the HTTP API and event stream.
- [docs/ui.md](docs/ui.md): the web UI pages and design.
- [docs/channels.md](docs/channels.md): connect Telegram or Discord.
- [docs/config-ui-coverage.md](docs/config-ui-coverage.md): which settings the Config page edits; a test keeps every setting editable in the UI.
- [docs/plugins.md](docs/plugins.md): writing harnesses, deployers, observers and notifiers.
- [onett.example.yaml](onett.example.yaml): every config option, annotated.
- [examples/dogfood/README.md](examples/dogfood/README.md): run onett on its own repo, with a local staging release.
