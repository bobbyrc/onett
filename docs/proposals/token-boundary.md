# Proposal: what the API tokens can do, and who can get them

Status: implemented, 2026-11-12, awaiting the owner's approval of the pull request. Two of the
decisions below (**D2** and **D3**) change something the owner could do from chat; each can be
restored by deleting one line, see "Owner decisions" at the end.

## The problem in one paragraph

Onett had one **API token**: a random secret in `<data_dir>/token` that a caller sends as a
bearer header. Two very different callers held it. One is the owner at a terminal, through the
`onett` CLI. The other is the **orchestrator**, the agent the owner chats with (also called chief
of staff), which needs to run the same CLI. The server could not tell them apart, so a decision
meant for a person (approve a merge, resume a pause, give a blocked goal a fresh budget, speak as
the owner) was open to an agent, and held back only by the agent's instructions. Instructions are
not a boundary: an agent that reads text from an issue, a log or a task summary can be steered by
it.

## Two kinds of agent misbehaviour, and which one this closes

This distinction decides every choice below, so it comes first.

- **An agent that uses what onett hands it.** It runs a command that is on its PATH, uses a
  variable in its environment, or runs something onett pre-approved. No file hunting. This is
  what happens when untrusted text steers an agent, or when an agent is "helpfully" unblocking
  itself. **This change closes this kind.**
- **An agent that goes looking.** Agents run as the owner's OS user. One that decides to read
  `<data_dir>/token`, write `onett.db` or edit `onett.yaml` directly can do so, whatever the API
  refuses. **Nothing here closes that, and nothing short of agent isolation can** (a separate OS
  user or a container, `docs/proposals/agent-isolation.md`). Any credential the owner's CLI can
  read from disk, a same-user agent can read too. A password would not help either: the same
  agent could write a session or a password hash into the database.

So the promise is: onett gives no agent the means to take a person's decision, and an agent has
to deliberately break out of its lane to take one. The README already states the same-user limit;
this change makes the rest of the Safety model true up to that limit.

## D1. Two tokens

| Option | What it means | Cost to the owner in daily use |
|---|---|---|
| **A. Two tokens (chosen)** | `<data_dir>/token` is the **owner token**: the owner's CLI uses it and keeps every power it had. The orchestrator is given a different, lesser **agent token**, which the server refuses for a person's decisions (D2 to D5). | None. `onett approve`, `onett resume` and the rest keep working at the owner's terminal. |
| B. One token, refused for a person's decisions | Approve, resume and the rest need a signed-in person: the web UI or a chat-channel button. | Those commands stop working at the owner's terminal, and a CLI-only instance must create an account first. |
| C. B, plus `onett login` so the CLI holds a session | As B, but the CLI signs in and stores a session. | A sign-in step; and the stored session is one more file a same-user agent can read, so it buys nothing over A. |

Why A: B and C charge the owner every day for a boundary that still does not hold against an
agent that goes looking. A closes the "uses what onett hands it" cases at no daily cost.

How it works:

- The agent token is **derived** from the owner token (a keyed hash, HMAC-SHA256, with a fixed
  label). The server computes it at start. Nothing new is stored, existing installs need no
  migration, and it is the same after a restart, which matters because an orchestrator turn can
  outlive a daemon restart. Knowing the agent token does not reveal the owner token.
- The orchestrator's `ONETT_TOKEN` is the agent token. No agent is handed the owner token.
- Everything a token was refused before stays refused for **both** tokens: config, users and
  secrets (invariant 12), reopening a conversation, conversation settings, editing or cancelling
  a queued message. Nothing is widened.
- The actions the agent token is refused are one table, `agentRefused` in `internal/auth`, one
  line per action, each with the reason the agent is given so that it tells the owner where to
  act instead of trying another way.

### D7. Inside an agent run the CLI is an API client and nothing else

The split only means something if an agent cannot get the owner's authority from the CLI itself.
Onett pre-approves every `onett ...` command for the orchestrator, so the CLI has to hold the
line on its own. Every agent session carries `ONETT_RUN_ID` and `ONETT_ROLE`. When either is set:

- the CLI never reads the token file. It uses only `ONETT_TOKEN` from its environment: the agent
  token for the orchestrator, nothing for a worker, which gets a plain "worker agents have no API
  access" error;
- only the commands that call the API run, plus `doctor`, `version` and `help`. The commands
  that act with the owner's authority on this machine (`token`, `user`, `service`, `init`,
  `open`, `backup`, `project add`, and `run-shim`, the helper the daemon starts agents through) refuse
  with a one-line reason. The list is an allowlist, so a command added later is refused inside
  an agent run until someone decides it is safe;
- `serve` is refused for the orchestrator only. A second daemon on the owner's data directory
  would run the scheduler twice, and the orchestrator has no use for one. A worker that develops
  onett may still start a throwaway daemon to test its change, as `AGENTS.md` asks.

`onett serve` also stops adding the onett directory to a worker's PATH. That is tidiness more
than a boundary (a worker's inherited PATH may hold `onett` anyway); the rules above do the work.

Cost: an agent that develops onett itself and tests against a throwaway daemon passes that
daemon's token explicitly (`ONETT_TOKEN=$(cat <tmp>/token) onett status`) instead of relying on
the file lookup.

Not closed (only isolation closes it): an agent that unsets the two variables, or reads the token
file and calls the API directly.

## D2. Approving a merge is a person's decision

`POST /approvals/{id}/decide` refuses the agent token. A signed-in owner or operator, a
chat-channel button (already checked against the allowlist) and the owner's own CLI keep working.

This **reverses a recorded decision**: `docs/proposals/accounts-and-config-editor.md`
(2026-10-28) kept "approving merges from chat or the CLI". The CLI half stays. The chat half
goes, because code cannot tell "the owner asked" from "text the orchestrator read said the owner
asked".

Cost: under the default policy, none, because merges need no approval. Under `supervised`, one
click on the approval card (web UI, Discord or Telegram) or `onett approve` in a terminal, instead
of typing "approve it" in chat. The orchestrator still lists what is waiting and says where to
decide it.

## D3. Agents may stop things; only a person starts them again

One rule instead of tracking who set each pause:

- The agent token may still **pause** the factory or a project, cancel a task or a run, and block
  or finish an active goal. Stopping is the safe direction.
- The agent token is refused: **resuming** the factory, resuming a project, **any change to a
  goal someone paused** (resume, block or finish: blocking it and then resuming the blocked goal
  would otherwise start it again), and **running a loop by hand**. No built-in prompt uses
  `onett loop run`, and a manual run is how a loop the owner disabled would be started.

Cost: "resume the factory" typed in chat no longer works; the owner uses the UI, a channel or
`onett resume`. The alternative, letting an agent resume a pause the agent itself set, keeps chat
symmetrical but needs a "who paused" check on every resume path, and a pause the owner asked for
in chat becomes one an agent may lift.

## D4. A blocked goal: an agent may resume it, but only a person gives it a fresh budget

Resuming a blocked goal resets its turn budget. The heartbeat (the on-duty sweep that runs the
orchestrator over stuck work) is told to "resume or unblock a goal when its blocker is gone",
which is useful autonomy, so the agent token may still resume a blocked goal, but with the turn
and stalled-turn counts it already has. A goal blocked for reaching `limits.max_goal_turns`
blocks again at once; one blocked for stalling gets one more turn. A person's resume resets the
budget as before. The turn limit becomes a cap only a person lifts, at no cost to the owner.

## D5. Messages and retries: say who is speaking, and keep a denial terminal

1. **The heading names the real author.** A message the orchestrator sends to a task agent
   arrives under "Message from the orchestrator", not "Message from the owner". The same goes for
   the note `onett task retry --note` appends and the recorded reason for a re-queue.
2. **The agent token cannot write into an orchestrator conversation.** Text there is stored as
   the owner's own words and starts a turn, so only the owner puts it there.
3. **Only a person retries a task that a denial stopped.** A *denial* is the agent CLI reporting
   that a tool call was refused; the run ends with status `denied` and the task blocks. With the
   agent token, `onett task retry` and a message to that task are refused with the reason, also
   after the task is cancelled, and also when it was the task's review that was denied (retrying
   the task runs the review again). No schema change: the rule reads the latest run of the task
   and of its newest review.
4. **Only onett creates a conversation follow-up.** A follow-up task joins its parent's
   conversation and resumes that agent's session, so `POST /tasks` refuses `source:
   "conversation"` from any client. Otherwise a new task could be attached to a stopped one to
   carry it on.

The third rule settles the **goal loop's recorded exception** (`docs/design.md`, goals: a task
that blocks inside an active goal is the goal orchestrator's to handle first, and the owner is not
notified). The exception stays for ordinary failures (failed checks, attempts used up, merge
conflicts), which are still retried unattended. A task that a denial stopped is excluded: it
notifies the owner at once, as it would outside a goal, and the goal's other tasks carry on.
Invariant 4 ("a denial or a stall is terminal") therefore holds as written and gains a sentence.
A stalled task stays retryable by the orchestrator: a stall is often a hung network call that a
retry fixes.

Known limits. Codex reports a refusal only in prose, so its run is not marked `denied` and this
rule does not see it (already in the README). The orchestrator can still create a new task, or a
new goal, with the same instructions: that is new work with its own run and its own turn budget,
not a retry, and the daily budget is what bounds it. And after a person retries a denied task,
the agent stays refused until the task has a new run or a new review, which errs on the safe side.

A message the owner sends with their own CLI (`onett task message`) is the owner's, like one
typed in the web UI; before, every message sent with the token was stored as the orchestrator's.
The same goes for the record of who closed or cancelled a conversation.

## D6. The first account needs proof of being the operator

`POST /auth/setup` needs a **setup code**, derived from the owner token the same way the agent
token is, under a different label. `onett open`, which the owner already runs to open the UI,
adds the code to the address after `#`, a part of the address browsers do not send to servers or
write to logs. The page sends it with the new username and password. Without it the page says to
run `onett open` on the machine that runs onett. The code is only useful while no account exists,
as setup itself is, and there is no new API route. Cost: none; one command the owner runs anyway.

The alternative was to remove the web form and create the first account from the CLI only: less
code, but the first run would need a terminal step and a password to copy before the browser is
any use.

Not closed: an agent that reads the token file or writes the database directly. Also, the code
is the same until the account exists, and while `onett open` starts the browser it is briefly in
that command's arguments, which other OS users on a shared machine can list. Create the account
right after the first start.

## What this does for triggers

- A trigger's orchestrator turn no longer holds a token that can approve merges (D1, D2).
- An agent cannot write into a trigger's conversation (D5.2).
- "Only a signed-in owner" no longer depends on who reaches first-account setup first (D6).
- For new code: a CLI command the orchestrator should run must be added to the agent-run
  allowlist, and a route that takes a person's decision must refuse the agent token.

## How it is built

- The API's sign-in step marks a request made with the agent token, in one place, so no handler
  can forget it. Handlers that take a person's decision refuse it with a per-handler check, the
  pattern the code already used for person-only routes, so every route keeps its declared action.
  The engine reads the same mark for the rules that depend on state (a paused goal, a blocked
  goal's budget, a task a denial stopped).
- Refusals answer 403 with a reason the orchestrator can repeat to the owner.

## Left out on purpose

- Agent isolation. It is the only thing that closes "an agent that goes looking"; it has its own
  proposal and is a larger decision.
- `POST /config/reload` stays available to the agent token: it re-reads a file an API caller
  cannot write.
- Tracking who set each pause, a second stored token, and a CLI sign-in: each adds state or a
  daily cost without closing more.

## Owner decisions

| | Decision | Cost | The way back |
|---|---|---|---|
| D2 | The orchestrator can no longer approve merges (reverses the 2026-10-28 "approve from chat" decision) | Under `supervised`, one click or one terminal command instead of a chat message | Delete the `ApproveMerge` line in `agentRefused` (`internal/auth/agent.go`) and the test that names it |
| D3 | Agents may pause but never resume, and never run a loop by hand | "Resume" typed in chat no longer works | Delete the `ResumeFactory`, `ResumeProject`, `ChangePausedGoal` or `RunLoop` line there, each with its test |
