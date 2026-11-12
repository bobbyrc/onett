# Proposal: what the API token can do, and who can get it

Status: proposal for the owner, 2026-11-12. Nothing here is implemented. It answers findings 1,
2, 5, 9 and 10 of the security review of `main` at `8eb4637`, and points 2, 3 and 4 of that
review's feedback on triggers. The choices that need the owner are numbered **D1** to **D7** and
collected at the end.

## The problem in one paragraph

Onett has one **API token**: a random secret in `<data_dir>/token` that a caller sends as a
bearer header. Two very different callers hold it. One is the owner at a terminal, through the
`onett` CLI. The other is the **orchestrator**, the agent the owner chats with (also called chief
of staff), which gets the token so it can run the same CLI. The server cannot tell them apart, so
every action the owner's CLI may take, an agent may take too: approve a merge, resume a pause,
give a blocked goal a fresh budget, speak to a task agent as "the owner". Worker agents (engineer,
reviewer and the rest) are meant to hold no token at all, but the CLI is on their PATH and finds
the token file by itself. And while no account exists, any local process may create the first
owner account.

## Two kinds of agent misbehaviour, and which one this closes

This distinction decides every option below, so it comes first.

- **An agent that uses what onett hands it.** It runs a command that is on its PATH, uses a
  variable in its environment, or runs something onett pre-approved. No file hunting. This is
  what happens when text in an issue, a log or a task summary steers an agent, or when an agent
  is "helpfully" unblocking itself. **This proposal closes this kind completely** for the five
  findings.
- **An agent that goes looking.** Agents run as the owner's OS user. One that decides to read
  `<data_dir>/token`, write `onett.db` or edit `onett.yaml` directly can do so, whatever the API
  refuses. **Nothing in this proposal closes that, and nothing short of agent isolation can**
  (a separate OS user or a container, `docs/proposals/agent-isolation.md`). Any credential the
  owner's CLI can read from disk, a same-user agent can read too. A password does not help
  either: the same agent can write a session row or a password hash into the database.

So the honest promise after this change is: onett no longer gives any agent the means to take a
person's decision, and an agent has to deliberately break out of its lane to take one. The
README already states the same-user limit; this change makes the rest of the Safety model true
up to that limit.

## D1. Split the token in two (recommended)

| Option | What changes | Cost to the owner in daily use |
|---|---|---|
| **A. Two tokens (recommended)** | `<data_dir>/token` stays and becomes the **owner token**: the owner's CLI uses it and keeps every power it has today. The orchestrator is given a different, lesser **agent token**. The server refuses the agent token for a person's decisions (D2 to D5). | None. `onett approve`, `onett resume` and the rest keep working at the owner's terminal. |
| B. One token, refused for a person's decisions | Approve, resume and the rest need a signed-in person: the web UI or a chat-channel button. | `onett approve`, `onett resume`, `onett goal resume`, `onett project resume` and `onett loop run` stop working at the owner's terminal. An instance used only from the CLI must create an account first. |
| C. B, plus `onett login` so the CLI holds a session | As B, but the CLI signs in and stores a session. | A sign-in step; and the stored session is one more file a same-user agent can read, so it buys nothing over A. |

Why A: B and C charge the owner every day for a boundary that still does not hold against an
agent that goes looking (it can write a session into the database). A closes the same
"uses what onett hands it" cases at no daily cost.

How A works, in plain terms:

- The agent token is **derived** from the owner token (a keyed hash, HMAC-SHA256, with a fixed
  label). The server computes it at start; nothing new is stored, existing installs need no
  migration, and it stays the same across restarts, which matters because an orchestrator turn
  can outlive a daemon restart. Knowing the agent token does not reveal the owner token.
- The orchestrator's `ONETT_TOKEN` becomes the agent token. No agent is handed the owner token
  any more.
- Everything the single token is refused today stays refused for **both** tokens: config,
  users and secrets (invariant 12), reopening a conversation, conversation settings, editing or
  cancelling a queued message. Nothing is widened.
- The agent token is the owner token minus the decisions in D2 to D5.

### The CLI inside an agent run (finding 1, and one thing the review did not reach)

The split only means something if an agent cannot get the owner token from the CLI itself.
Today it can, three ways, all with what onett hands it:

1. Any agent runs `onett approve`; the CLI reads the token file by itself (finding 1).
2. The orchestrator runs `onett token`, which prints the owner token. Onett pre-approves every
   `onett ...` command for the orchestrator, so no permission check sees it. **New, found while
   writing this.**
3. The same pre-approval covers the commands that skip the API and write files directly:
   `onett user reset-password` (prints a new password for any account, the owner's included)
   and `onett project add` (edits `onett.yaml`). Both go around invariant 12. **Also new.**

Proposed, as one rule: **inside an agent run the CLI is an API client and nothing else.** Every
agent session already carries `ONETT_RUN_ID` and `ONETT_ROLE`. When either is set:

- the CLI never reads the token file; it uses only `ONETT_TOKEN` from its environment (the agent
  token, for the orchestrator; nothing, for a worker, which then gets a plain "worker agents
  have no API access" error);
- only the commands that call the API run (`status`, `task`, `goal`, `run`, `approvals`,
  `events` and so on, plus `serve`, `doctor`, `version` and `help`). `token`, `user`, `service`,
  `init`, `open` and `project add` refuse with a one-line reason. The list is an allowlist, so a
  command added later is refused inside an agent run until someone decides it is safe.

And in `onett serve`: the onett directory goes on PATH only for the orchestrator, not for
workers. This is tidiness more than a boundary (a worker's inherited PATH may hold `onett`
anyway); the rule above is what does the work.

Cost: an agent that develops onett itself and tests against a throwaway daemon must pass that
daemon's token explicitly (`ONETT_TOKEN=$(cat <tmp>/token) onett status`) instead of relying on
the file lookup. `serve` still runs, so the throwaway-daemon check in `AGENTS.md` keeps working.

Not closed (only isolation closes it): an agent that unsets the two variables, or reads the
token file with `cat` and calls the API with `curl`.

## D2. Approving a merge is a person's decision (finding 2)

`POST /approvals/{id}/decide` refuses the agent token. A signed-in owner or operator, a
chat-channel button (already checked against the allowlist) and the owner's own CLI keep working.

This **reverses a recorded decision**: `docs/proposals/accounts-and-config-editor.md`
(2026-10-28) kept "approving merges from chat or the CLI". The CLI half stays. The chat half
goes: telling the orchestrator "approve it" no longer works, because code cannot tell "the owner
asked" from "text the orchestrator read said the owner asked".

| Option | Cost to the owner |
|---|---|
| **Refuse the agent token (recommended)** | Under the default policy, none: merges need no approval. Under `supervised`, one click on the approval card (web UI, Discord or Telegram) or `onett approve` in a terminal, instead of typing "approve it" in chat. The orchestrator still lists what is waiting. |
| Keep "tell the chat to approve" | None, but finding 2 stays open, the `supervised` gate stays held by prompt text, and the Safety model must say so. |

## D3. Agents may stop things; only a person starts them again (finding 9, pauses and loops)

One rule instead of tracking who set each pause:

- The agent token may still **pause** the factory, a project or a goal, cancel a task or a run,
  and block or finish a goal. Stopping is the safe direction.
- The agent token is refused for: **resuming** the factory, resuming a project, resuming a
  *paused* goal, and **running a loop by hand** (`POST /loops/{name}/run`). No built-in prompt
  uses `onett loop run`, and a manual run is how a disabled `deploy_watch` or `self_update`
  would be started.

| Option | Cost to the owner |
|---|---|
| **Flat rule: agents never resume (recommended)** | "Resume the factory" typed in chat no longer works; use the UI, a channel or `onett resume`. |
| The agent may resume only a pause the agent itself set | Keeps "pause ... resume" in chat symmetrical. Costs a recorded "who paused" check on every resume path, and a pause the owner asked for in chat becomes one an agent may lift. |

## D4. A blocked goal: an agent may resume it, but only a person gives it a fresh budget

Today resuming a blocked goal resets its turn budget, and the code comment calls that "the
owner's call", yet the token may do it. The heartbeat (the on-duty sweep that runs the
orchestrator over stuck work) is told to "resume or unblock a goal when its blocker is gone",
which is useful autonomy.

| Option | What it does | Cost to the owner |
|---|---|---|
| **Resume without the reset (recommended)** | With the agent token, resuming a blocked goal keeps its turn and stalled-turn counts. A goal blocked for reaching `limits.max_goal_turns` blocks again at once; one blocked for stalling gets one more turn. A person's resume resets the budget as today. | None. The heartbeat keeps unblocking goals; the turn limit becomes a hard cap only a person lifts. |
| Person only | The agent token cannot resume a goal at all. | Every blocked goal waits for the owner, and the heartbeat instruction is removed. |

## D5. Messages and retries: say who is speaking, and keep a denial terminal (finding 10)

Three changes, the first two with no real alternative:

1. **The heading names the real author.** A message the orchestrator sends to a task agent
   arrives under "Message from the orchestrator", not "Message from the owner". The same goes for
   the note `onett task retry --note` appends ("Note from the orchestrator") and the recorded
   reason ("re-queued by the orchestrator").
2. **The agent token cannot write into an orchestrator conversation** (`POST
   /conversations/{id}/messages` and `/flush`). Text there is stored as the owner's own words and
   starts a turn, so only the owner should be able to put it there. No prompt uses `onett chat`.
   This also closes triggers feedback point 3: an agent cannot write into a trigger's
   conversation.
3. **A task blocked by a denial is retried only by a person.** A *denial* is the agent CLI
   reporting that a tool call was refused; the run ends with status `denied` and the task blocks.
   With the agent token, `onett task retry` and a message to that task are refused with the
   reason. No schema change is needed: "blocked by a denial" is "the task's latest run has
   status `denied`".

That third change is the answer to the **goal loop's recorded exception** (`docs/design.md`,
goals section: a task that blocks inside an active goal is the goal orchestrator's to handle
first, and the owner is not notified). Options:

| Option | Invariant 4 ("a denial or a stall is terminal") | Cost to the owner |
|---|---|---|
| **Keep the exception, except for denials (recommended)** | Holds as written, and gains a sentence: only a person retries a denial. A denial-blocked task inside a goal notifies the owner at once, as it would outside a goal. | A goal with a denied task waits for the owner on that task (other tasks in the goal carry on). Ordinary failures (failed checks, attempts used up, merge conflicts) are still retried unattended. |
| Also exclude stalls | As above, and a stalled task needs a person too. | More waiting: stalls are often a hung network call, which a retry fixes. |
| Keep the exception as it is | Must be reworded to admit that a goal's orchestrator may retry a denial. This weakens an invariant, which is the owner's call. | None. |

Known limit, already in the README: Codex reports a refusal only in prose, so its run is not
marked `denied` and this rule does not see it.

## D6. The first account needs proof of being the operator (finding 5)

Today `POST /auth/setup` creates the owner account for any local process while no account
exists.

| Option | How it works | Cost to the owner |
|---|---|---|
| **A setup link from `onett open` (recommended)** | Setup needs a **setup code**, derived from the owner token the same way the agent token is. `onett open` (which the owner already runs to open the UI) adds the code to the address after `#`, a part of the address browsers do not send to servers or write to logs. The page sends it with the new username and password. Without it the page says "run `onett open` on the machine running onett". | None: one command the owner runs anyway, once. No new API route. |
| Create the first account from the CLI only | Remove `POST /auth/setup` and the setup form. `onett user create NAME` writes the account and prints a generated password, as `user reset-password` does today. | First run needs a terminal step and a password to copy before the browser is any use. Less code, worse first impression. |

The code works only while no account exists, as setup itself does. Inside an agent run the CLI
cannot produce it (D1: `open` is refused and the token file is not read). Not closed: an agent
that reads the token file or writes the database directly.

## What this does for triggers

- Point 2 (a trigger's turn holds a token that can approve merges): closed by D1 and D2.
- Point 3 (the token can write into a trigger's conversation): closed by D5.2.
- Point 4 ("only a signed-in owner" depends on first-account setup): closed by D6.
- For the triggers stream: a new CLI command the orchestrator should run must be added to the
  agent-run allowlist (D1), and a new route that takes a person's decision must refuse the agent
  token.

## How it is built (for the reviewer, not a decision)

- The server's notion of a caller gains one fact: "this request used the agent token". The
  sign-in middleware sets it in one place, so no handler can forget it. Handlers that take a
  person's decision refuse it with the same per-handler check the code already uses for
  person-only routes (`userPrincipal`), so every route keeps its declared action and
  `routes.go` does not change. The engine reads the same fact for the three state-dependent
  rules (D4, D5.1, D5.3).
- Refusals answer 403 with a reason the orchestrator can repeat to the owner ("only a person can
  approve a merge"), so it reports instead of trying another way.
- Tests first, one new file per finding: the route or command is refused for the agent token or
  inside an agent run, and still works for a signed-in owner and for the owner token. Then the
  review's proofs for findings 1 and 5 are repeated against a throwaway daemon and shown refused.
- Docs in the same change: README **Safety model**, invariants 4 and 12 in `AGENTS.md`,
  `docs/api.md`, `docs/openapi.yaml`, the goals section of `docs/design.md`, and the orchestrator
  prompt (it no longer offers `onett approve` or `onett resume`).

Files beyond the stream's brief that this needs: `internal/engine/helpers.go` (one condition in
`goalHandlesBlock`, so a denial inside a goal notifies), a new `cmd/onett` file for the agent-run
rule, and the setup call in `web/src/api.ts` (it gains the code).

## Left out on purpose

- Agent isolation. It is the only thing that closes "an agent that goes looking"; it has its own
  proposal and is a larger decision.
- `POST /config/reload` stays available to the agent token: it re-reads a file an API caller
  cannot write.
- Tracking who set each pause, a second stored token, and a CLI sign-in: each adds state or a
  daily cost without closing more.

## What I need from you

| | Decision | Recommended |
|---|---|---|
| D1 | Split the token; inside an agent run the CLI is an API client only | Yes |
| D2 | The orchestrator can no longer approve merges, reversing the 2026-10-28 "approve from chat" decision | Yes |
| D3 | Agents may pause but never resume, and never run a loop by hand | Flat rule |
| D4 | An agent may resume a blocked goal without a fresh budget; a paused goal needs a person | Resume without the reset |
| D5 | Only a person retries a task blocked by a denial (stalls stay retryable); the goal exception is kept for everything else | Denials only |
| D6 | The first account needs the setup link from `onett open` | Setup link |
| D7 | Treat the two new CLI paths (`onett token`, and `user reset-password` / `project add`, all pre-approved for the orchestrator) as part of this change | Yes |
